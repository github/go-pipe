package pipe

import (
	"context"
	"io"
)

//
// From the point of view of the pipeline as a whole, if stdin is
// provided by the user (`WithStdin()`), then we don't want to close
// it at all, whether it's an `*os.File` or not. For this reason,
// stdin has to be wrapped using a `readerNopCloser` before being
// passed into the first stage. For efficiency reasons, it's
// advantageous for the first stage should ideally unwrap its stdin
// argument before actually using it. If the wrapped value is an
// `*os.File` and the stage is a command stage, then unwrapping is
// also important to get the right semantics.
//
// For stdout, it depends on whether the user supplied it using
// `WithStdout()` or `WithStdoutCloser()`. If the former, then the
// considerations are the same as for stdin.
//
// [1] It's theoretically possible for a command to pass the open file
//     descriptor to another, longer-lived process, in which case the
//     file descriptor wouldn't necessarily get closed when the
//     command finishes. But that's ill-behaved in a command that is
//     being used in a pipeline, so we'll ignore that possibility.

// Stage is an element of a `Pipeline`. It reads from standard input
// and writes to standard output.
//
// Who closes stdin and stdout?
//
// A `Stage` as a whole needs to be responsible for closing its end of
// stdin and stdout (assuming that `Start()` / `Start2()` returns
// successfully). Its doing so tells the previous/next stage that it
// is done reading/writing data, which can affect their behavior.
// Therefore, it should close each one as soon as it is done with it.
// (If the caller wants to suppress the closing of stdin/stdout, it
// can always wrap the corresponding argument in a `nopCloser`.)
//
// Specifically, if a stage is started using `Start()`, then it is
// responsible for closing the stdin that is passed to it, and also
// for closing its end of the `io.Reader` that the method returns. If
// a stage implements `Stage2` and is started using `Start2()`, then
// it is responsible for closing both the stdin and stdout that are
// passed in as arguments. How this should be done depends on the kind
// of stage and whether stdin/stdout are of type `*os.File`.
//
// If a stage is an external command, it the subprocess ultimately
// needs its own copies of `*os.File` file descriptors for its stdin
// and stdout. The external command will "always" [1] close those when
// it exits.
//
// If the stage is an external command and one of the arguments is an
// `*os.File`, then it can set the corresponding field of `exec.Cmd`
// to that argument directly. This has the result that `exec.Cmd`
// duplicates that file descriptor and passes the dup to the
// subprocess. Therefore, the stage can close its copy of that
// argument as soon as the external command has started, because the
// external command will keep its own copy open as long as necessary
// (and no longer!), in roughly the following sequence:
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
// finished:
//
//	cmd.Stdin = r // Similarly for stdout
//	cmd.Start(…)
//	cmd.Wait()
//	r.Close()
//
// If the stage is a Go function, then it holds the only copy of
// stdin/stdout, so it must wait until the function is done before
// closing them (regardless of their underlying type:
//
//	f(…, stdin, stdout)
//	stdin.Close()
//	stdout.Close()
type Stage interface {
	// Name returns the name of the stage.
	Name() string

	// Start starts the stage in the background, in the environment
	// described by `env`, and using `stdin` as input. (`stdin` should
	// be set to `nil` if the stage is to receive no input, which
	// might be the case for the first stage in a pipeline.) It
	// returns an `io.ReadCloser` from which the stage's output can be
	// read (or `nil` if it generates no output, which should only be
	// the case for the last stage in a pipeline). See the `Stage`
	// type comment for more information about responsibility for
	// closing stdin and stdout.
	//
	// If `Start()` returns without an error, `Wait()` must also be
	// called, to allow all resources to be freed.
	Start(ctx context.Context, env Env, stdin io.ReadCloser) (io.ReadCloser, error)

	// Wait waits for the stage to be done, either because it has
	// finished or because it has been killed due to the expiration of
	// the context passed to `Start()`.
	Wait() error
}

// StagePreferences is the way that a Stage2 indicates its preferences
// about how it is run. This is used within `pipe.Pipeline` to decide
// when to use `os.Pipe()` vs. `io.Pipe()` for creating the pipes
// between stages.
type StagePreferences struct {
	StdinPreference  IOPreference
	StdoutPreference IOPreference
}

// Stage2 is a `Stage` that can accept both stdin and stdout arguments
// when it is started.
type Stage2 interface {
	Stage

	// Preferences() returns this stage's preferences regarding how it
	// should be run.
	Preferences() StagePreferences

	// Start2 starts the stage (like `Stage.Start()`), except that it
	// allows the caller to pass in both stdin and stdout.
	Start2(ctx context.Context, env Env, stdin io.ReadCloser, stdout io.WriteCloser) error
}

// IOPreference describes what type of stdin / stdout a stage would
// prefer.
//
// External commands prefer `*os.File`s (such as those produced by
// `os.Pipe()`) as their stdin and stdout, because those can be passed
// directly by the external process without any extra copying and also
// simplify the semantics around process termination. Go function
// stages are typically happy with any `io.ReadCloser` (such as one
// produced by `io.Pipe()`), which can be more efficient because
// traffic through an `io.Pipe()` happens entirely in userspace.
type IOPreference int

const (
	// IOPreferenceUndefined indicates that the stage doesn't care
	// what form the specified stdin / stdout takes (i.e., any old
	// `io.ReadCloser` / `io.WriteCloser` is just fine).
	IOPreferenceUndefined IOPreference = iota

	// IOPreferenceFile indicates that the stage would prefer for the
	// specified stdin / stdout to be an `*os.File`, to avoid copying.
	IOPreferenceFile

	// IOPreferenceNil indicates that the stage does not use the
	// specified stdin / stdout, so `nil` should be passed in. This
	// should only happen at the beginning / end of a pipeline.
	IOPreferenceNil
)
