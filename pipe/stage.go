package pipe

import (
	"context"
)

// Stage is an element of a `Pipeline`. It reads from standard input
// and writes to standard output.
//
// # Who closes stdin and stdout?
//
// A `Stage` is responsible for calling `Close()` on the
// `InputStream`/`OutputStream` that represent its stdin and stdout as
// soon as it doesn't need them anymore. That responsibility begins as
// soon as the stage's `Start()` method is called, and applies
// regardless of whether `Start()` returns an error. It must close the
// streams before its `Wait()` method returns. The caller must not
// close the streams after calling `Start()`.
//
// Closing stdin/stdout tells the previous/next stage that this stage
// is done reading/writing data, which can affect their behavior.
// Therefore, it is important for a stage to close each one as soon as
// it is done with it.
//
// From the point of view of the pipeline as a whole, if stdin is
// provided by the user (`WithStdin()`), then we don't want the first
// stage to close it at all. This is arranged by passing a
// non-closing `InputStream` when it starts that stage. For stdout, it
// depends on whether the user supplied it using `WithStdout()` or
// `WithStdoutCloser()`, and in the former case provides the last
// stage with a non-closing `OutputStream`. Calling `Close()` on a
// non-closing stream (or even on a nil stream) is a NOP, so the
// `Stage` can always call `Close()` and doesn't have to worry about
// whether a stdin/stdout stream is non-closing.
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

// StageRequirements describes what a Stage needs from the streams
// connected to its stdin and stdout. The zero value is correct for
// stages that are happy with arbitrary io.Reader/io.Writer streams,
// such as Function stages.
type StageRequirements struct {
	Stdin  StreamRequirement
	Stdout StreamRequirement
}
