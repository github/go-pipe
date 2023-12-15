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

// commandStage is a pipeline `Stage2` based on running an external
// command and piping the data through its stdin and stdout. It also
// implements `Stage2`.
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
	_ Stage2 = (*commandStage)(nil)
)

// Command returns a pipeline `Stage2` based on the specified external
// `command`, run with the given command-line `args`. Its stdin and
// stdout are handled as usual, and its stderr is collected and
// included in any `*exec.ExitError` that the command might emit.
func Command(command string, args ...string) Stage2 {
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
func CommandStage(name string, cmd *exec.Cmd) Stage2 {
	return &commandStage{
		name: name,
		cmd:  cmd,
		done: make(chan struct{}),
	}
}

func (s *commandStage) Name() string {
	return s.name
}

func (s *commandStage) Start(
	ctx context.Context, env Env, stdin io.ReadCloser,
) (io.ReadCloser, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	if err := s.Start2(ctx, env, stdin, pw); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}

	// Now close our copy of the write end of the pipe (the subprocess
	// has its own copy now and will keep it open as long as it is
	// running). There's not much we can do now in the case of an
	// error, so just ignore them.
	_ = pw.Close()

	// The caller is responsible for closing `pr`.
	return pr, nil
}

func (s *commandStage) Preferences() StagePreferences {
	prefs := StagePreferences{
		StdinPreference:  IOPreferenceFile,
		StdoutPreference: IOPreferenceFile,
	}
	if s.cmd.Stdin != nil {
		prefs.StdinPreference = IOPreferenceNil
	}
	if s.cmd.Stdout != nil {
		prefs.StdoutPreference = IOPreferenceNil
	}

	return prefs
}

func (s *commandStage) Start2(
	ctx context.Context, env Env, stdin io.ReadCloser, stdout io.WriteCloser,
) error {
	if s.cmd.Dir == "" {
		s.cmd.Dir = env.Dir
	}

	s.setupEnv(ctx, env)

	// Things that have to be closed as soon as the command has
	// started:
	var earlyClosers []io.Closer

	// See the type command for `Stage` and the long comment in
	// `Pipeline.WithStdin()` for the explanation of this unwrapping
	// and closing behavior.

	if stdin != nil {
		switch stdin := stdin.(type) {
		case readerNopCloser:
			// In this case, we shouldn't close it. But unwrap it for
			// efficiency's sake:
			s.cmd.Stdin = stdin.Reader
		case readerWriterToNopCloser:
			// In this case, we shouldn't close it. But unwrap it for
			// efficiency's sake:
			s.cmd.Stdin = stdin.Reader
		case *os.File:
			// In this case, we can close stdin as soon as the command
			// has started:
			s.cmd.Stdin = stdin
			earlyClosers = append(earlyClosers, stdin)
		default:
			// In this case, we need to close `stdin`, but we should
			// only do so after the command has finished:
			s.cmd.Stdin = stdin
			s.lateClosers = append(s.lateClosers, stdin)
		}
	}

	if stdout != nil {
		// See the long comment in `Pipeline.Start()` for the
		// explanation of this special case.
		switch stdout := stdout.(type) {
		case writerNopCloser:
			// In this case, we shouldn't close it. But unwrap it for
			// efficiency's sake:
			s.cmd.Stdout = stdout.Writer
		case *os.File:
			// In this case, we can close stdout as soon as the command
			// has started:
			s.cmd.Stdout = stdout
			earlyClosers = append(earlyClosers, stdout)
		default:
			// In this case, we need to close `stdout`, but we should
			// only do so after the command has finished:
			s.cmd.Stdout = stdout
			s.lateClosers = append(s.lateClosers, stdout)
		}
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
		return err
	}

	for _, closer := range earlyClosers {
		_ = closer.Close()
	}

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
		ps, ok := eErr.ProcessState.Sys().(syscall.WaitStatus)
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

	for _, closer := range s.lateClosers {
		if closeErr := closer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	return err
}
