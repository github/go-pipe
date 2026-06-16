package pipe

import (
	"context"
	"io"
)

// ConfigOption is an option that can be applied to a `Config` or at
// `Start()` time. Options that are not reusable for multiple
// pipelines should implement only `StartOption`, not this interface.
type ConfigOption interface {
	apply(*runner)
	Option
}

// sliceConfigOption is a `ConfigOption` based on a slice of other
// `ConfigOption`s.
type sliceConfigOption []ConfigOption

var _ ConfigOption = (sliceConfigOption)(nil)

func (sco sliceConfigOption) apply(r *runner) {
	for _, opt := range sco {
		opt.apply(r)
	}
}

func (sco sliceConfigOption) applyAtStart(r *runner) {
	for _, opt := range sco {
		opt.applyAtStart(r)
	}
}

// funcConfigOption implements `ConfigOption` by calling a function.
type funcConfigOption struct {
	fn func(r *runner)
}

var _ ConfigOption = funcConfigOption{}

func (opt funcConfigOption) apply(r *runner) {
	opt.fn(r)
}

func (opt funcConfigOption) applyAtStart(r *runner) {
	opt.fn(r)
}

// newConfigOption returns a `configOption` that does its work by
// calling `fn`.
func newConfigOption(fn func(r *runner)) funcConfigOption {
	return funcConfigOption{fn}
}

// Option is an option that can be applied only at `Start()` time
// but is not suitable to be applied to a `Config` (e.g., because the
// option can only be used once, like `WithStdin()`).
type Option interface {
	applyAtStart(*runner)
}

// sliceOption is an `Option` based on a slice of other `Option`s.
type sliceOption []Option

var _ ConfigOption = (sliceConfigOption)(nil)

func (so sliceOption) applyAtStart(r *runner) {
	for _, opt := range so {
		opt.applyAtStart(r)
	}
}

// funcOption implements `StartOption` by calling a function.
type funcOption struct {
	fn func(r *runner)
}

var _ Option = funcOption{}

func (opt funcOption) applyAtStart(r *runner) {
	opt.fn(r)
}

// newFuncOption returns a `startOption` that does its work by
// calling `fn`.
func newFuncOption(fn func(r *runner)) funcOption {
	return funcOption{fn}
}

// WithDir sets the default directory for running external commands.
func WithDir(dir string) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.env.Dir = dir
		},
	)
}

// WithStdin assigns stdin for the runner. The caller retains
// ownership of stdin; the runner will not close it, even if `Start()`
// returns an error.
//
// If this stdin is connected to a `Command` stage and is not an
// `*os.File`, `exec.Cmd` has to copy stdin through an internal
// goroutine, and `Cmd.Wait()` waits for that copy to finish. This is
// fine for bounded readers such as `strings.Reader` and
// `bytes.Reader`, and for `*os.File` values, which are passed to the
// command directly. But a borrowed, non-file reader that can block
// forever can also block the runner forever if the command exits
// without consuming all of its stdin. See
// `TestPipelineIOPipeStdinThatIsNeverClosed` for the known limitation.
func WithStdin(stdin io.Reader) Option {
	return newFuncOption(
		func(r *runner) {
			r.stdin = Input(stdin)
		},
	)
}

// WithStdout assigns stdout for the runner. The caller retains
// ownership of stdout; the runner will not close it, even if
// `Start()` returns an error.
func WithStdout(stdout io.Writer) Option {
	return newFuncOption(
		func(r *runner) {
			r.stdout = Output(stdout)
		},
	)
}

// WithStdoutCloser assigns stdout for the runner, arranging to close
// stdout when the runner is done with it. The runner is responsible
// for closing stdout even if `Start()` returns an error.
func WithStdoutCloser(stdout io.WriteCloser) Option {
	return newFuncOption(
		func(r *runner) {
			r.stdout = ClosingOutput(stdout)
		},
	)
}

// WithEnvVar appends an environment variable for commands run within
// the runner.
func WithEnvVar(key, value string) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.env.Vars = append(
				r.env.Vars,
				func(_ context.Context, vars []EnvVar) []EnvVar {
					return append(vars, EnvVar{Key: key, Value: value})
				},
			)
		},
	)
}

// WithEnvVars appends several environment variable for commands run
// within the runner.
func WithEnvVars(b []EnvVar) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.env.Vars = append(
				r.env.Vars,
				func(_ context.Context, a []EnvVar) []EnvVar {
					return append(a, b...)
				},
			)
		},
	)
}

type ContextValueFunc func(context.Context) (string, bool)

// WithEnvVarFunc appends a context-based environment variable to be
// passed to commands run within the runner.
func WithEnvVarFunc(key string, valueFunc ContextValueFunc) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.env.Vars = append(
				r.env.Vars,
				func(ctx context.Context, vars []EnvVar) []EnvVar {
					if val, ok := valueFunc(ctx); ok {
						return append(vars, EnvVar{Key: key, Value: val})
					}
					return vars
				},
			)
		},
	)
}

type ContextValuesFunc func(context.Context) []EnvVar

// WithEnvVarsFunc appends several context-based environment variables
// for a runner.
func WithEnvVarsFunc(valuesFunc ContextValuesFunc) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.env.Vars = append(
				r.env.Vars,
				func(ctx context.Context, vars []EnvVar) []EnvVar {
					return append(vars, valuesFunc(ctx)...)
				},
			)
		},
	)
}

// WithEventHandler sets an event handler for the runner. Setting one
// will emit an event for each process.
func WithEventHandler(handler EventHandler) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.eventHandler = handler
		},
	)
}

// WithStagePanicHandler sets a panic handler for the runner. When the
// code being run panics, the provided handler will be invoked,
// allowing the client to handle the panic in whatever way they see
// fit.
//
// Note:
//   - The client is responsible for deciding whether to recover from
//     the panic or panicking again.
//   - If a panic handler is not set, the panic will be propagated
//     normally.
func WithStagePanicHandler(ph StagePanicHandler) ConfigOption {
	return newConfigOption(
		func(r *runner) {
			r.panicHandler = ph
		},
	)
}
