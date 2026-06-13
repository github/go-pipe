package pipe

import (
	"context"
)

// Stage is an element of a `Pipeline`. It reads from standard input
// and writes to standard output.
//
// # Who closes stdin and stdout?
//
// A `Stage` as a whole is responsible for closing its end of stdin
// and stdout if the corresponding stream is closing. That
// responsibility transfers to the stage as soon as the stage's
// `Start()` method is called and applies even if `Start()` returns an
// error. Before returning an error from `Start()`, the stage must
// close any closing stream that it has not already handed off to
// something else that will close it promptly. The caller must not
// close a closing stream after passing it to `Start()`.
//
// If the caller wants to retain ownership of stdin/stdout, it passes
// a non-closing stream. Calling `Close()` on a non-closing stream is
// a NOP, so it isn't harmful but isn't required.
//
// Closing stdin/stdout tells the previous/next stage that this stage
// is done reading/writing data, which can affect their behavior.
// Therefore, after a successful start, a stage should close each one
// as soon as it is done with it. Assume that this is done via the
// following local function
//
//	closeStreams := func() {
//		// Error handling omitted.
//		_ = stdin.Close()
//		_ = stdout.Close()
//	}
//
// From the point of view of the pipeline as a whole, if stdin is
// provided by the user (`WithStdin()`), then we don't want the first
// stage to close it at all, whether it's an `*os.File` or not. The
// pipeline communicates this by passing a non-closing `InputStream`
// when it starts that stage. For stdout, it depends on whether the
// user supplied it using `WithStdout()` or `WithStdoutCloser()`. In
// any case, this function can close the streams anyway, because
// `InputStream.Close()` and `OutputStream.Close()` do nothing in
// those cases.
//
// When these closes should happen depends on what kind of stage it is
// and whether stdin/stdout are of type `*os.File`.
//
// ## A command stage
//
// If a stage is an external command, then the subprocess ultimately
// needs its own copies of `*os.File` file descriptors for its stdin
// and stdout. The external command will "always" [1] close those when
// it exits.
//
// (It's theoretically possible for a command to pass the open file
// descriptor to another, longer-lived process, in which case the file
// descriptor wouldn't necessarily get closed when the command
// finishes. But that's ill-behaved in a command that is being used in
// a pipeline, so we'll ignore that possibility.)
//
// If the stage is an external command and one of the arguments is an
// `*os.File`, then it can set the corresponding field of `exec.Cmd`
// to that argument directly. This has the result that `exec.Cmd`
// duplicates that file descriptor and passes the dup to the
// subprocess. Therefore, the stage must close its copy of that
// argument as soon as the external command has started, because the
// external command will keep its own copy open as long as necessary
// (and no longer!). Therefore, it should use roughly the following
// sequence:
//
//	cmd.Stdin = stdin.Reader()
//	cmd.Stdout = stdout.Writer()
//	err := cmd.Start(…)
//	// Close our copies as soon as the command has started:
//	closeStreams()
//	if err != nil {
//		return err
//	}
//	return cmd.Wait()
//
// If the stage is an external command and its stdin is not an
// `*os.File`, then `exec.Cmd` will take care of creating an
// `os.Pipe()`, copying from the provided reader into the pipe, and
// eventually closing both ends of the pipe. The stage must close the
// provided stdin itself, but only _after_ the external command and
// the copy have finished, like so:
//
//	defer closeStreams()
//	cmd.Stdin = stdin.Reader()
//	cmd.Stdout = stdout.Writer()
//	err := cmd.Start(…)
//	if err != nil {
//		return err
//	}
//	return cmd.Wait()
//
// ## A function stage
//
// If the stage is a Go function, then it holds the only copy of
// stdin/stdout, so it must wait until the function is done before
// closing them (regardless of their underlying type, like so:
//
//	go func() {
//		defer closeStreams()
//		f(…, stdin.Reader(), stdout.Writer())
//	}()
type Stage interface {
	// Name returns the name of the stage.
	Name() string

	// Requirements returns this stage's requirements regarding how its
	// stdin and stdout pipes should be created.
	Requirements() StageRequirements

	// Start starts the stage in the background, in the environment
	// described by `opts.Env`, using `stdin` to provide its input and
	// `stdout` to collect its output. (`stdin.Reader()` or
	// `stdout.Writer()` might be `nil` if the stage is to receive no
	// input or produce no output, which might be the case for the
	// first/last stage in a pipeline.) The stage is responsible for
	// calling `stdin.Close()` and `stdout.Close()`, even if `Start()`
	// returns an error. See the `Stage` type comment for more
	// information about responsibility for closing stdin and stdout.
	//
	// If `Start()` returns without an error, `Wait()` must also be
	// called, to allow all resources to be freed. If `Start()` returns
	// an error, `Wait()` must not be called.
	Start(
		ctx context.Context, opts StageOptions,
		stdin *InputStream, stdout *OutputStream,
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
	// user code that a stage runs in a library-spawned goroutine,
	// converting it into an error. Stage types that don't run user code in
	// a library-spawned goroutine ignore it.
	PanicHandler StagePanicHandler
}

// StagePanicHandler is a function that handles panics in a pipeline's stages.
type StagePanicHandler func(p any) error

// StageRequirements describes what a Stage needs from the streams connected to
// its stdin and stdout. The zero value is correct for stages that are happy
// with arbitrary io.Reader/io.Writer streams, such as Function stages.
type StageRequirements struct {
	Stdin  StreamRequirement
	Stdout StreamRequirement

	// {Stdin,Stdout}NeedsFile indicate that, if stdio is connected, the
	// stage requires it to be backed by an *os.File (a real file descriptor)
	StdinNeedsFile  bool
	StdoutNeedsFile bool
}
