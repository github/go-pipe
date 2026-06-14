package pipe

import (
	"context"
	"io"
)

// Option is a functional option that can be passed to the `Runner`
// constructor.
type Option func(*runner)

// WithDir sets the default directory for running external commands.
func WithDir(dir string) Option {
	return func(r *runner) {
		r.env.Dir = dir
	}
}

// WithStdin assigns stdin for the runner. The caller retains
// ownership of stdin; the runner will not close it, even if `Start()`
// returns an error.
func WithStdin(stdin io.Reader) Option {
	return func(r *runner) {
		r.stdin = Input(stdin)
	}
}

// WithStdout assigns stdout for the runner. The caller retains
// ownership of stdout; the runner will not close it, even if
// `Start()` returns an error.
func WithStdout(stdout io.Writer) Option {
	return func(r *runner) {
		r.stdout = Output(stdout)
	}
}

// WithStdoutCloser assigns stdout for the runner, arranging to close
// stdout when the runner is done with it. The runner is responsible
// for closing stdout even if `Start()` returns an error.
func WithStdoutCloser(stdout io.WriteCloser) Option {
	return func(r *runner) {
		r.stdout = ClosingOutput(stdout)
	}
}

// WithEnvVar appends an environment variable for commands run within
// the runner.
func WithEnvVar(key, value string) Option {
	return func(r *runner) {
		r.env.Vars = append(r.env.Vars, func(_ context.Context, vars []EnvVar) []EnvVar {
			return append(vars, EnvVar{Key: key, Value: value})
		})
	}
}

// WithEnvVars appends several environment variable for commands run
// within the runner.
func WithEnvVars(b []EnvVar) Option {
	return func(r *runner) {
		r.env.Vars = append(r.env.Vars, func(_ context.Context, a []EnvVar) []EnvVar {
			return append(a, b...)
		})
	}
}

type ContextValueFunc func(context.Context) (string, bool)

// WithEnvVarFunc appends a context-based environment variable to be
// passed to commands run within the runner.
func WithEnvVarFunc(key string, valueFunc ContextValueFunc) Option {
	return func(r *runner) {
		r.env.Vars = append(
			r.env.Vars,
			func(ctx context.Context, vars []EnvVar) []EnvVar {
				if val, ok := valueFunc(ctx); ok {
					return append(vars, EnvVar{Key: key, Value: val})
				}
				return vars
			},
		)
	}
}

type ContextValuesFunc func(context.Context) []EnvVar

// WithEnvVarsFunc appends several context-based environment variables
// for a runner.
func WithEnvVarsFunc(valuesFunc ContextValuesFunc) Option {
	return func(r *runner) {
		r.env.Vars = append(
			r.env.Vars,
			func(ctx context.Context, vars []EnvVar) []EnvVar {
				return append(vars, valuesFunc(ctx)...)
			},
		)
	}
}

// WithEventHandler sets an event handler for the runner. Setting one
// will emit an event for each process.
func WithEventHandler(handler EventHandler) Option {
	return func(r *runner) {
		r.eventHandler = handler
	}
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
func WithStagePanicHandler(ph StagePanicHandler) Option {
	return func(r *runner) {
		r.panicHandler = ph
	}
}
