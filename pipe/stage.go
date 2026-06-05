package pipe

import (
	"context"
	"io"
)

// Stage is an element of a `Pipeline`. It reads from standard input
// and writes to standard output.
//
// Who closes stdin and stdout?
//
// A `Stage` as a whole is responsible for closing its end of stdin
// and stdout (assuming that `Start()` returns successfully) if the
// corresponding closer passed to `Start()` is non-nil. Its doing so
// tells the previous/next stage that it is done reading/writing data,
// which can affect their behavior. Therefore, it should close each
// one as soon as it is done with it. If the caller wants to suppress
// the closing of stdin/stdout, it passes a nil closer.
//
// How this should be done depends on whether stdin/stdout are of type
// `*os.File`.
//
// If a stage is an external command, then the subprocess ultimately
// needs its own copies of `*os.File` file descriptors for its stdin
// and stdout. The external command will "always" [1] close those when
// it exits.
//
// If the stage is an external command and one of the arguments is an
// `*os.File`, then it can set the corresponding field of `exec.Cmd`
// to that argument directly. This has the result that `exec.Cmd`
// duplicates that file descriptor and passes the dup to the
// subprocess. Therefore, the stage must close its copy of that
// argument as soon as the external command has started, because the
// external command will keep its own copy open as long as necessary
// (and no longer!). It should use roughly the following sequence:
//
//	cmd.Stdin = f // Similarly for stdout
//	cmd.Start(…)
//	f.Close() // close our copy
//	cmd.Wait()
//
// If the stage is an external command and one of its arguments is not
// an `*os.File`, then `exec.Cmd` will take care of creating an
// `os.Pipe()`, copying from the provided argument in/out of the pipe,
// and eventually closing both ends of the pipe. The stage must close
// the argument itself, but only _after_ the external command has
// finished, like so:
//
//	cmd.Stdin = r // Similarly for stdout
//	cmd.Start(…)
//	cmd.Wait()
//	r.Close()
//
// If the stage is a Go function, then it holds the only copy of
// stdin/stdout, so it must wait until the function is done before
// closing them (regardless of their underlying type, like so:
//
//	go func() {
//		f(…, stdin, stdout)
//		stdin.Close()
//		stdout.Close()
//	}()
//
// From the point of view of the pipeline as a whole, if stdin is
// provided by the user (`WithStdin()`), then we don't want the first
// stage to close it at all, whether it's an `*os.File` or not. The
// pipeline communicates this by passing a nil stdin closer when it
// starts that stage. For stdout, it depends on whether the user
// supplied it using `WithStdout()` or `WithStdoutCloser()`.
//
// [1] It's theoretically possible for a command to pass the open file
//     descriptor to another, longer-lived process, in which case the
//     file descriptor wouldn't necessarily get closed when the
//     command finishes. But that's ill-behaved in a command that is
//     being used in a pipeline, so we'll ignore that possibility.

type Stage interface {
	// Name returns the name of the stage.
	Name() string

	// Requirements returns this stage's requirements regarding how its
	// stdin and stdout pipes should be created.
	Requirements() StageRequirements

	// Start starts the stage in the background, in the environment
	// described by `opts.Env`, using `stdin` to provide its input and
	// `stdout` to collect its output. (`stdin`/`stdout` might be set
	// to `nil` if the stage is to receive no input, which might be the
	// case for the first/last stage in a pipeline.) If `stdinCloser` or
	// `stdoutCloser` is non-nil, the stage is responsible for closing
	// it. See the `Stage` type comment for more information about
	// responsibility for closing stdin and stdout.
	//
	// If `Start()` returns without an error, `Wait()` must also be
	// called, to allow all resources to be freed.
	Start(
		ctx context.Context, opts StageOptions,
		stdin io.Reader, stdinCloser io.Closer,
		stdout io.Writer, stdoutCloser io.Closer,
	) error

	// Wait waits for the stage to be done, either because it has
	// finished or because it has been killed due to the expiration of
	// the context passed to `Start()`.
	Wait() error
}

// StageOptions carries everything (other than `ctx`, `stdin`, and
// `stdout`) that a pipeline passes to `Stage.Start`.
type StageOptions struct {
	// Env is the environment (working directory and extra environment
	// variables) that the stage should run in.
	Env

	// PanicHandler, if non-nil, is invoked to recover a panic that escapes
	// user code that a stage runs in a library-spawned goroutine (a
	// Function stage's StageFunc, or a memory-limit stage's event
	// handler), converting it into an error. Stage types that don't run
	// user code in a library-spawned goroutine ignore it.
	PanicHandler StagePanicHandler
}

// StagePanicHandler is a function that handles panics in a pipeline's stages.
type StagePanicHandler func(p any) error

// StageRequirements describes what a Stage needs from the pipes connected to
// its stdin and stdout. The zero value is correct for stages that are happy
// with arbitrary io.Reader/io.Writer streams, such as Function stages.
type StageRequirements struct {
	// StdinNeedsFile indicates that the stage requires stdin to be backed by an
	// *os.File (a real file descriptor), for example so an external command can
	// read from the descriptor directly.
	StdinNeedsFile bool

	// StdoutNeedsFile indicates that the stage requires stdout to be backed by
	// an *os.File (a real file descriptor), for example so an external command
	// can write to the descriptor directly.
	StdoutNeedsFile bool
}
