package pipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"

	"golang.org/x/sync/errgroup"
)

// commandStage is a pipeline `Stage` based on running an external
// command and piping the data through its stdin and stdout.
type commandStage struct {
	name string
	cmd  *exec.Cmd

	// lateClosers is a list of things that have to be closed once the
	// command has finished.
	lateClosers []io.Closer

	done   chan struct{}
	wg     errgroup.Group
	stderr bytes.Buffer

	// If the context expired, and we attempted to kill the command,
	// `ctx.Err()` is stored here.
	ctxErr atomic.Value
}

var (
	_ Stage           = (*commandStage)(nil)
	_ processProvider = (*commandStage)(nil)
)

// processProvider is the hook external memory-watchers use to find the running
// process so they can sample its RSS and kill it if necessary.
type processProvider interface {
	Process() *os.Process
	Kill(error)
}

// Command returns a pipeline `Stage` based on the specified external
// `command`, run with the given command-line `args`. Its stdin and
// stdout are handled as usual, and its stderr is collected and
// included in any `*exec.ExitError` that the command might emit.
func Command(command string, args ...string) Stage {
	if len(command) == 0 {
		panic("attempt to create command with empty command")
	}

	cmd := exec.Command(command, args...)
	return CommandStage(command, cmd)
}

// CommandStage returns a pipeline `Stage` with the name `name`, based on
// the specified `cmd`. Its stdin and stdout are handled as usual, and
// its stderr is collected and included in any `*exec.ExitError` that
// the command might emit.
func CommandStage(name string, cmd *exec.Cmd) Stage {
	return &commandStage{
		name: name,
		cmd:  cmd,
		done: make(chan struct{}),
	}
}

func (s *commandStage) Name() string {
	return s.name
}

func (s *commandStage) Process() *os.Process {
	return s.cmd.Process
}

func (s *commandStage) Requirements() StageRequirements {
	return StageRequirements{
		StdinNeedsFile:  true,
		StdoutNeedsFile: true,
	}
}

func (s *commandStage) Start(
	ctx context.Context, opts StageOptions,
	stdin io.Reader, closeStdin bool,
	stdout io.Writer, closeStdout bool,
) error {
	stdinCloser := ownedCloser(stdin, closeStdin)
	stdoutCloser := ownedCloser(stdout, closeStdout)

	if s.cmd.Dir == "" {
		s.cmd.Dir = opts.Dir
	}

	s.setupEnv(ctx, opts.Env)

	// Things that have to be closed as soon as the command has started:
	var earlyClosers []io.Closer

	// See the type comment for `Stage` for the explanation of this closing behavior.
	if stdin != nil {
		s.cmd.Stdin = stdin
	}

	if stdinCloser != nil {
		if _, ok := stdin.(*os.File); ok {
			// We can close our copy as soon as the command has started
			earlyClosers = append(earlyClosers, stdinCloser)
		} else {
			// We need to close `stdin`, but only after the command has finished
			s.lateClosers = append(s.lateClosers, stdinCloser)
		}
	}

	closeEarlyClosers := func() {
		for _, closer := range earlyClosers {
			_ = closer.Close()
		}
	}

	// On error, Close any pipes we created and wait for the goroutines to
	// exit before propagating the error.
	cleanupOnStartFailure := func() {
		closeEarlyClosers()
		_ = s.wg.Wait()
		_ = s.closeLateClosers()
	}

	if stdout != nil {
		if f, ok := stdout.(*os.File); ok {
			s.cmd.Stdout = f
			if stdoutCloser != nil {
				earlyClosers = append(earlyClosers, stdoutCloser)
			}
		} else {
			if stdoutCloser != nil {
				s.lateClosers = append(s.lateClosers, stdoutCloser)
			}
			// Route the copy through our own pipe so we can use a
			// pooled buffer rather than letting exec.Cmd allocate a
			// fresh 32KB buffer for its internal io.Copy.
			ec, err := s.setupPooledStdout(stdout)
			if err != nil {
				cleanupOnStartFailure()
				return err
			}
			earlyClosers = append(earlyClosers, ec)
		}
	} else if stdoutCloser != nil {
		s.lateClosers = append(s.lateClosers, stdoutCloser)
	}

	// If the caller hasn't arranged otherwise, read the command's
	// standard error into our `stderr` field:
	if s.cmd.Stderr == nil {
		// We can't just set `s.cmd.Stderr = &s.stderr`, because if we
		// do then `s.cmd.Wait()` doesn't wait to be sure that all
		// error output has been captured. By doing this ourselves, we
		// can be sure.
		p, err := s.cmd.StderrPipe()
		if err != nil {
			cleanupOnStartFailure()
			return err
		}
		s.wg.Go(func() error {
			_, err := io.Copy(&s.stderr, p)
			// We don't consider `ErrClosed` an error (FIXME: is this
			// correct?):
			if err != nil && !errors.Is(err, os.ErrClosed) {
				return err
			}
			return nil
		})
	}

	// Put the command in its own process group, if possible:
	s.runInOwnProcessGroup()

	if err := s.cmd.Start(); err != nil {
		cleanupOnStartFailure()
		return err
	}

	closeEarlyClosers()

	// Arrange for the process to be killed (gently) if the context
	// expires before the command exits normally:
	go func() {
		select {
		case <-ctx.Done():
			s.Kill(ctx.Err())
		case <-s.done:
			// Process already done; no need to kill anything.
		}
	}()

	return nil
}

// setupEnv sets or modifies the environment that will be passed to
// the command.
func (s *commandStage) setupEnv(ctx context.Context, env Env) {
	if len(env.Vars) == 0 {
		return
	}

	if s.cmd.Env == nil {
		// If the caller didn't explicitly set an environment on
		// `cmd`, then start with the current environment, and add a
		// few environment variables that are meaningful to gitmon:
		s.cmd.Env = os.Environ()
	}

	var vars []EnvVar
	for _, fn := range env.Vars {
		vars = fn(ctx, vars)
	}
	varMap := make(map[string]string, len(vars))
	for _, v := range vars {
		varMap[v.Key] = v.Value
	}

	s.cmd.Env = copyEnvWithOverrides(s.cmd.Env, varMap)
}

func copyEnvWithOverrides(myEnv []string, overrides map[string]string) []string {
	vars := make([]string, 0, len(myEnv)+len(overrides))

	for _, v := range myEnv {
		eq := strings.Index(v, "=")
		if eq == -1 {
			vars = append(vars, v)
			continue
		}
		key := v[:eq]
		if _, ok := overrides[key]; ok {
			continue
		}
		vars = append(vars, v)
	}

	for key, value := range overrides {
		vars = append(vars, fmt.Sprintf("%s=%s", key, value))
	}

	return vars
}

// filterCmdError interprets `err`, which was returned by `Cmd.Wait()`
// (possibly `nil`), possibly modifying it or ignoring it. It returns
// the error that should actually be returned to the caller (possibly
// `nil`).
func (s *commandStage) filterCmdError(err error) error {
	if err == nil {
		return err
	}

	eErr, ok := err.(*exec.ExitError)
	if !ok {
		return err
	}

	ctxErr, ok := s.ctxErr.Load().(error)
	if ok {
		// If the process looks like it was killed by us, substitute
		// `ctxErr` for the process's own exit error. Note that this
		// doesn't do anything on Windows, where the `Signaled()`
		// method isn't implemented (it is hardcoded to return
		// `false`).
		ps, ok := eErr.Sys().(syscall.WaitStatus)
		if ok && ps.Signaled() &&
			(ps.Signal() == syscall.SIGTERM || ps.Signal() == syscall.SIGKILL) {
			return ctxErr
		}
	}

	eErr.Stderr = s.stderr.Bytes()
	return eErr
}

func (s *commandStage) Wait() error {
	defer close(s.done)

	// Make sure that any stderr is copied before `s.cmd.Wait()`
	// closes the read end of the pipe:
	wgErr := s.wg.Wait()

	err := s.cmd.Wait()
	err = s.filterCmdError(err)

	if err == nil && wgErr != nil {
		err = wgErr
	}

	if closeErr := s.closeLateClosers(); err == nil {
		err = closeErr
	}

	return err
}

func (s *commandStage) closeLateClosers() error {
	var err error
	for _, closer := range s.lateClosers {
		if closeErr := closer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	s.lateClosers = nil
	return err
}

// setupPooledStdout creates an `os.Pipe()`, sets it as `cmd.Stdout`,
// and starts a goroutine that copies from the read end to `dst` using
// a pooled buffer (or `dst.ReadFrom` when `dst` implements it). The
// returned closer is the write end of the pipe; the caller must add
// it to `earlyClosers` so it is closed once the command has started.
//
// The buffer-pool optimization works for command stages whose stdout is
// not an `*os.File`. Without it, `exec.Cmd` would set up its own pipe
// and run `io.Copy` with a freshly allocated 32KB buffer per invocation.
func (s *commandStage) setupPooledStdout(dst io.Writer) (io.Closer, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	s.cmd.Stdout = pw
	s.wg.Go(func() error {
		defer pr.Close()
		_, err := pooledCopy(dst, pr)
		if err != nil && !errors.Is(err, os.ErrClosed) {
			return err
		}
		return nil
	})
	return pw, nil
}
