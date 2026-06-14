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

// EnvVar represents an environment variable that will be provided to any child
// process spawned in this pipeline.
type EnvVar struct {
	// The name of the environment variable.
	Key string
	// The value.
	Value string
}

// Pipeline represents a Unix-like pipe that can include multiple
// stages, including external processes but also and stages written in
// Go.
type Pipeline struct {
	env Env

	stdin  *InputStream
	stdout *OutputStream

	p *Pipe

	eventHandler EventHandler
	panicHandler StagePanicHandler
}

var emptyEventHandler = func(_ *EventError) {}

type NewPipeFn func(opts ...Option) *Pipeline

// NewPipeline returns a Pipeline struct with all of the `options`
// applied.
func New(options ...Option) *Pipeline {
	p := &Pipeline{
		p:            NewPipe(""),
		eventHandler: emptyEventHandler,
	}

	for _, option := range options {
		option(p)
	}

	return p
}

// Add appends one or more stages to the pipeline.
func (p *Pipeline) Add(stages ...Stage) {
	p.p.Add(stages...)
}

// AddWithIgnoredError appends one or more stages that are ignoring
// the passed in error to the pipeline.
func (p *Pipeline) AddWithIgnoredError(em ErrorMatcher, stages ...Stage) {
	p.p.AddWithIgnoredError(em, stages...)
}

func (p *Pipeline) stageOptions() StageOptions {
	return StageOptions{Env: p.env, PanicHandler: p.panicHandler}
}

// Start starts the commands in the pipeline. If `Start()` exits
// without an error, `Wait()` must also be called, to allow all
// resources to be freed.
//
// If `Start()` returns an error, `Wait()` must not be called. Before
// returning an error, `Start()` cancels and waits for any stages that
// were started, closes any inter-stage pipes that the pipeline owns,
// and closes stdout if it was supplied with `WithStdoutCloser()`.
// Streams supplied with `WithStdin()` or `WithStdout()` remain owned by
// the caller and are not closed by the pipeline.
func (p *Pipeline) Start(ctx context.Context) error {
	return p.p.Start(ctx, p.stageOptions(), p.stdin, p.stdout)
}

func (p *Pipeline) Output(ctx context.Context) ([]byte, error) {
	var buf bytes.Buffer
	p.stdout = Output(&buf)
	err := p.Run(ctx)
	return buf.Bytes(), err
}

// Wait waits for each stage in the pipeline to exit.
func (p *Pipeline) Wait() error {
	err := p.p.Wait()

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
			p.eventHandler(eventErr)
		}

		return err
	}

	return nil
}

// Run starts and waits for the commands in the pipeline. If startup
// fails, it returns the `Start()` error after `Start()` has performed
// its failure cleanup.
func (p *Pipeline) Run(ctx context.Context) error {
	if err := p.Start(ctx); err != nil {
		return err
	}

	return p.Wait()
}
