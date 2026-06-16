package pipe

import (
	"bytes"
	"context"
	"errors"
)

// Env represents the environment that a pipeline stage should run in.
// It is passed to `Stage.Start()`.
type Env struct {
	// The directory in which external commands should be executed by
	// default.
	Dir string

	// Vars are extra environment variables. These will override any
	// environment variables that would be inherited from the current
	// process.
	Vars []AppendVars
}

// FinishEarly is an error that can be returned by a `Stage` to
// request that the iteration be ended early (possibly without reading
// all of its input). This "error" is considered a successful return,
// and is not reported to the caller.
//
//revive:disable:error-naming
//nolint:staticcheck // ST1012: FinishEarly is the intentional name for this sentinel error
var FinishEarly = errors.New("finish stage early")

//revive:enable:error-naming

type AppendVars func(context.Context, []EnvVar) []EnvVar

// EnvVar represents an environment variable that will be provided to
// any child process.
type EnvVar struct {
	// The name of the environment variable.
	Key string
	// The value.
	Value string
}

// runner runs a `Stage`. A `runner` can only be used once.
type runner struct {
	env Env

	stdin  *InputStream
	stdout *OutputStream

	stage Stage

	eventHandler EventHandler
	panicHandler StagePanicHandler

	oneUse oneUse
}

var emptyEventHandler = func(_ *EventError) {}

// newRunner returns a Runner with all of the `options` applied.
func newRunner(stage Stage, options ...Option) *runner {
	r := &runner{
		stage:        stage,
		eventHandler: emptyEventHandler,
		oneUse:       oneUse{thing: "runner for " + stage.Name()},
	}

	r.applyOptions(options...)

	return r
}

// applyOptions applies `options` to `r` in place (in addition to any
// options that have already been applied).
func (r *runner) applyOptions(options ...Option) {
	r.oneUse.assertNotStarted("apply options")

	for _, option := range options {
		option.applyAtStart(r)
	}
}

func (r *runner) stageOptions() StageOptions {
	return StageOptions{Env: r.env, PanicHandler: r.panicHandler}
}

type WaitFunc func() error

func nopWait(err error) WaitFunc {
	return func() error { return err }
}

// start starts the stage. If `start()` exits without an error, the
// returned `Waiter` must also be called exactly once, to learn about
// any errors and to allow all resources to be freed.
//
// If `start()` returns an error, the returned `Waiter` is a NOP that
// returns the same error. Before returning an error, `start()`
// cancels and waits for any stages that were started, closes any
// inter-stage pipes that the pipeline owns, and closes stdin/stdout
// if required. Streams that were supplied with `WithStdin()` or
// `WithStdout()` remain owned by the caller and are never closed by
// `runner`.
func (r *runner) start(ctx context.Context) (WaitFunc, error) {
	r.oneUse.assertStarting("start")

	if err := r.stage.Start(ctx, r.stageOptions(), r.stdin, r.stdout); err != nil {
		return nopWait(err), err
	}

	return r.wait, nil
}

// wait is the `WaitFunc` that is normally returned by `start()`.
func (r *runner) wait() error {
	r.oneUse.assertStarted("wait")

	err := r.stage.Wait()

	// Handle errors:
	switch {
	case err == nil:
		// No error to handle.

	case errors.Is(err, FinishEarly):
		// We ignore `FinishEarly` errors because that is how a
		// stage informs us that it intentionally finished early.

	default:
		var eventErr *EventError
		if errors.As(err, &eventErr) {
			r.eventHandler(eventErr)
		}

		return err
	}

	return nil
}

// run starts `stage` and waits for it to finish.
func (r *runner) run(ctx context.Context) error {
	// If start returns an error, the same error is returned by
	// `wait`.
	wait, _ := r.start(ctx)
	return wait()
}

// output starts `stage`, waits for it to finish, and collects and
// returns its stdout.
func (r *runner) output(ctx context.Context) ([]byte, error) {
	r.oneUse.assertNotStarted("get output")

	var buf bytes.Buffer
	r.applyOptions(WithStdout(&buf))
	err := r.run(ctx)
	return buf.Bytes(), err
}
