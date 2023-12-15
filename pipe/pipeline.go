package pipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
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

type ContextValueFunc func(context.Context) (string, bool)

type ContextValuesFunc func(context.Context) []EnvVar

// Pipeline represents a Unix-like pipe that can include multiple
// stages, including external processes but also and stages written in
// Go.
type Pipeline struct {
	env Env

	stdin  io.ReadCloser
	stdout io.WriteCloser
	stages []Stage
	cancel func()

	// Atomically written and read value, nonzero if the pipeline has
	// been started. This is only used for lifecycle sanity checks but
	// does not guarantee that clients are using the class correctly.
	started uint32

	eventHandler func(e *Event)
}

var emptyEventHandler = func(e *Event) {}

type NewPipeFn func(opts ...Option) *Pipeline

// NewPipeline returns a Pipeline struct with all of the `options`
// applied.
func New(options ...Option) *Pipeline {
	p := &Pipeline{
		eventHandler: emptyEventHandler,
	}

	for _, option := range options {
		option(p)
	}

	return p
}

// Option is a type alias for Pipeline functional options.
type Option func(*Pipeline)

// WithDir sets the default directory for running external commands.
func WithDir(dir string) Option {
	return func(p *Pipeline) {
		p.env.Dir = dir
	}
}

// WithStdin assigns stdin to the first command in the pipeline.
func WithStdin(stdin io.Reader) Option {
	return func(p *Pipeline) {
		// We don't want the first stage to close `stdin`, and it is
		// not even necessarily an `io.ReadCloser`. So wrap it in a
		// fake `io.ReadCloser` whose `Close()` method doesn't do
		// anything.
		//
		// We could use `io.NopCloser()` for this purpose, but that
		// would have a subtle problem. If the first stage is a
		// `Command`, then it wants to set the `exec.Cmd`'s `Stdin` to
		// an `io.Reader` corresponding to `p.stdin`. If `Cmd.Stdin`
		// is an `*os.File`, then `exec.Cmd` will pass the file
		// descriptor to the subcommand directly; there is no need to
		// create a pipe and copy the data into the input side of the
		// pipe. But if `p.stdin` is not an `*os.File`, then this
		// optimization is prevented. And even worse, it also has the
		// side effect that the goroutine that copies from `Cmd.Stdin`
		// into the pipe doesn't terminate until that fd is closed by
		// the writing side.
		//
		// That isn't always what we want. Consider, for example, the
		// following snippet, where the subcommand's stdin is set to
		// the stdin of the enclosing Go program, but wrapped with
		// `io.NopCloser`:
		//
		//     cmd := exec.Command("ls")
		//     cmd.Stdin = io.NopCloser(os.Stdin)
		//     cmd.Stdout = os.Stdout
		//     cmd.Stderr = os.Stderr
		//     cmd.Run()
		//
		// In this case, we don't want the Go program to wait for
		// `os.Stdin` to close (because `ls` isn't even trying to read
		// from its stdin). But it does: `exec.Cmd` doesn't recognize
		// that `Cmd.Stdin` is an `*os.File`, so it sets up a pipe and
		// copies the data itself, and this goroutine doesn't
		// terminate until `cmd.Stdin` (i.e., the Go program's own
		// stdin) is closed. But if, for example, the Go program is
		// run from an interactive shell session, that might never
		// happen, in which case the program will fail to terminate,
		// even after `ls` exits.
		//
		// So instead, in this special case, we wrap `stdin` in our
		// own `nopCloser`, which behaves like `io.NopCloser`, except
		// that `pipe.CommandStage` knows how to unwrap it before
		// passing it to `exec.Cmd`.
		p.stdin = newReaderNopCloser(stdin)
	}
}

// WithStdout assigns stdout to the last command in the pipeline.
func WithStdout(stdout io.Writer) Option {
	return func(p *Pipeline) {
		p.stdout = writerNopCloser{stdout}
	}
}

// WithStdoutCloser assigns stdout to the last command in the
// pipeline, and closes stdout when it's done.
func WithStdoutCloser(stdout io.WriteCloser) Option {
	return func(p *Pipeline) {
		p.stdout = stdout
	}
}

// WithEnvVar appends an environment variable for the pipeline.
func WithEnvVar(key, value string) Option {
	return func(p *Pipeline) {
		p.env.Vars = append(p.env.Vars, func(_ context.Context, vars []EnvVar) []EnvVar {
			return append(vars, EnvVar{Key: key, Value: value})
		})
	}
}

// WithEnvVars appends several environment variable for the pipeline.
func WithEnvVars(b []EnvVar) Option {
	return func(p *Pipeline) {
		p.env.Vars = append(p.env.Vars, func(_ context.Context, a []EnvVar) []EnvVar {
			return append(a, b...)
		})
	}
}

// WithEnvVarFunc appends a context-based environment variable for the pipeline.
func WithEnvVarFunc(key string, valueFunc ContextValueFunc) Option {
	return func(p *Pipeline) {
		p.env.Vars = append(p.env.Vars, func(ctx context.Context, vars []EnvVar) []EnvVar {
			if val, ok := valueFunc(ctx); ok {
				return append(vars, EnvVar{Key: key, Value: val})
			}
			return vars
		})
	}
}

// WithEnvVarsFunc appends several context-based environment variables for the pipeline.
func WithEnvVarsFunc(valuesFunc ContextValuesFunc) Option {
	return func(p *Pipeline) {
		p.env.Vars = append(p.env.Vars, func(ctx context.Context, vars []EnvVar) []EnvVar {
			return append(vars, valuesFunc(ctx)...)
		})
	}
}

// Event represents anything that could happen during the pipeline execution
type Event struct {
	Command string
	Msg     string
	Err     error
	Context map[string]interface{}
}

// WithEventHandler sets a handler for the pipeline. Setting one will emit
// and event for each process.
func WithEventHandler(handler func(e *Event)) Option {
	return func(p *Pipeline) {
		p.eventHandler = handler
	}
}

func (p *Pipeline) hasStarted() bool {
	return atomic.LoadUint32(&p.started) != 0
}

// Add appends one or more stages to the pipeline.
func (p *Pipeline) Add(stages ...Stage) {
	if p.hasStarted() {
		panic("attempt to modify a pipeline that has already started")
	}

	p.stages = append(p.stages, stages...)
}

// AddWithIgnoredError appends one or more stages that are ignoring
// the passed in error to the pipeline.
func (p *Pipeline) AddWithIgnoredError(em ErrorMatcher, stages ...Stage) {
	if p.hasStarted() {
		panic("attempt to modify a pipeline that has already started")
	}

	for _, stage := range stages {
		p.stages = append(p.stages, IgnoreError(stage, em))
	}
}

type stageStarter struct {
	prefs  StagePreferences
	stdin  io.ReadCloser
	stdout io.WriteCloser
}

// Start starts the commands in the pipeline. If `Start()` exits
// without an error, `Wait()` must also be called, to allow all
// resources to be freed.
func (p *Pipeline) Start(ctx context.Context) error {
	if p.hasStarted() {
		panic("attempt to start a pipeline that has already started")
	}

	atomic.StoreUint32(&p.started, 1)
	ctx, p.cancel = context.WithCancel(ctx)

	// We need to decide how to start the stages, especially what
	// pipes to use to connect adjacent stages (`os.Pipe()` vs.
	// `io.Pipe()`) based on the two stages' preferences.
	stageStarters := make([]stageStarter, len(p.stages), len(p.stages)+1)

	// Collect information about each stage's type and preferences:
	for i, s := range p.stages {
		stageStarters[i].prefs = s.Preferences()
	}

	if p.stdin != nil {
		// Arrange for the input of the 0th stage to come from
		// `p.stdin`:
		stageStarters[0].stdin = p.stdin
	}

	if p.stdout != nil {
		i := len(p.stages) - 1
		ss := &stageStarters[i]
		ss.stdout = p.stdout
	}

	// Clean up any processes and pipes that have been created. `i` is
	// the index of the stage that failed to start (whose output pipe
	// has already been cleaned up if necessary).
	abort := func(i int, err error) error {
		// Close the pipe that the previous stage was writing to.
		// That should cause it to exit even if it's not minding
		// its context.
		if stageStarters[i].stdin != nil {
			_ = stageStarters[i].stdin.Close()
		}

		// Kill and wait for any stages that have been started
		// already to finish:
		p.cancel()
		for _, s := range p.stages[:i] {
			_ = s.Wait()
		}
		p.eventHandler(&Event{
			Command: p.stages[i].Name(),
			Msg:     "failed to start pipeline stage",
			Err:     err,
		})
		return fmt.Errorf(
			"starting pipeline stage %q: %w", p.stages[i].Name(), err,
		)
	}

	// Loop over all but the last stage, starting them. By the time we
	// get to a stage, its stdin will have already been determined,
	// but we still need to figure out its stdout and set the stdin
	// that will be used for the subsequent stage.
	for i, s := range p.stages[:len(p.stages)-1] {
		ss := &stageStarters[i]
		nextSS := &stageStarters[i+1]

		// We need to generate a pipe pair for this stage to use
		// to communicate with its successor:
		if ss.prefs.StdoutPreference == IOPreferenceFile ||
			nextSS.prefs.StdinPreference == IOPreferenceFile {
			// Use an OS-level pipe for the communication:
			var err error
			nextSS.stdin, ss.stdout, err = os.Pipe()
			if err != nil {
				return abort(i, err)
			}
		} else {
			nextSS.stdin, ss.stdout = io.Pipe()
		}
		if err := s.Start(ctx, p.env, ss.stdin, ss.stdout); err != nil {
			nextSS.stdin.Close()
			ss.stdout.Close()
			return abort(i, err)
		}
	}

	// The last stage needs special handling, because its stdout
	// doesn't need to flow into another stage (it's already set in
	// `ss.stdout` if it's needed).
	{
		i := len(p.stages) - 1
		s := p.stages[i]
		ss := &stageStarters[i]

		if err := s.Start(ctx, p.env, ss.stdin, ss.stdout); err != nil {
			return abort(i, err)
		}
	}

	return nil
}

func (p *Pipeline) Output(ctx context.Context) ([]byte, error) {
	var buf bytes.Buffer
	p.stdout = writerNopCloser{&buf}
	err := p.Run(ctx)
	return buf.Bytes(), err
}

// Wait waits for each stage in the pipeline to exit.
func (p *Pipeline) Wait() error {
	if !p.hasStarted() {
		panic("unable to wait on a pipeline that has not started")
	}

	// Make sure that all of the cleanup eventually happens:
	defer p.cancel()

	var earliestStageErr error
	var earliestFailedStage Stage

	finishedEarly := false
	for i := len(p.stages) - 1; i >= 0; i-- {
		s := p.stages[i]
		err := s.Wait()

		// Handle errors:
		switch {
		case err == nil:
			// No error to handle. But unset the `finishedEarly` flag,
			// because earlier stages shouldn't be affected by the
			// later stage that finished early.
			finishedEarly = false
			continue

		case errors.Is(err, FinishEarly):
			// We ignore `FinishEarly` errors because that is how a
			// stage informs us that it intentionally finished early.
			// Moreover, if we see a `FinishEarly` error, ignore any
			// pipe error from the immediately preceding stage,
			// because it probably came from trying to write to this
			// stage after this stage closed its stdin.
			finishedEarly = true
			continue

		case IsPipeError(err):
			switch {
			case finishedEarly:
				// A successor stage finished early. It is common for
				// this to cause earlier stages to fail with pipe
				// errors. Such errors are uninteresting, so ignore
				// them. Leave the `finishedEarly` flag set, because
				// the preceding stage might get a pipe error from
				// trying to write to this one.
			case earliestStageErr != nil:
				// A later stage has already reported an error. This
				// means that we don't want to report the error from
				// this stage:
				//
				// * If the later error was also a pipe error: we want
				//   to report the _last_ pipe error seen, which would
				//   be the one already recorded.
				//
				// * If the later error was not a pipe error: non-pipe
				//   errors are always considered more important than
				//   pipe errors, so again we would want to keep the
				//   error that is already recorded.
			default:
				// In this case, the pipe error from this stage is the
				// most important error that we have seen so far, so
				// remember it:
				earliestFailedStage, earliestStageErr = s, err
			}

		default:
			// This stage exited with a non-pipe error. If multiple
			// stages exited with such errors, we want to report the
			// one that is most informative. We take that to be the
			// error from the earliest failing stage. Since we are
			// iterating through stages in reverse order, overwrite
			// any existing remembered errors (which would have come
			// from a later stage):
			earliestFailedStage, earliestStageErr = s, err
			finishedEarly = false
		}
	}

	if earliestStageErr != nil {
		p.eventHandler(&Event{
			Command: earliestFailedStage.Name(),
			Msg:     "command failed",
			Err:     earliestStageErr,
		})
		return fmt.Errorf("%s: %w", earliestFailedStage.Name(), earliestStageErr)
	}

	return nil
}

// Run starts and waits for the commands in the pipeline.
func (p *Pipeline) Run(ctx context.Context) error {
	if err := p.Start(ctx); err != nil {
		return err
	}

	return p.Wait()
}
