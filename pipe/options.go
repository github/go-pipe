package pipe

import (
	"context"
	"io"
)

// Option is a functional option that can be passed to the `Pipeline`
// constructor.
type Option func(*Pipeline)

// WithDir sets the default directory for running external commands.
func WithDir(dir string) Option {
	return func(r *Pipeline) {
		r.env.Dir = dir
	}
}

// WithStdin assigns stdin to the first command in the pipeline. The
// caller retains ownership of stdin; the pipeline will not close it,
// even if `Start()` returns an error.
func WithStdin(stdin io.Reader) Option {
	return func(r *Pipeline) {
		r.stdin = Input(stdin)
	}
}

// WithStdout assigns stdout to the last command in the pipeline. The
// caller retains ownership of stdout; the pipeline will not close it,
// even if `Start()` returns an error.
func WithStdout(stdout io.Writer) Option {
	return func(r *Pipeline) {
		r.stdout = Output(stdout)
	}
}

// WithStdoutCloser assigns stdout to the last command in the
// pipeline, and closes stdout when the pipeline is done with it. The
// pipeline is responsible for closing stdout even if `Start()` returns
// an error.
func WithStdoutCloser(stdout io.WriteCloser) Option {
	return func(r *Pipeline) {
		r.stdout = ClosingOutput(stdout)
	}
}

// WithEnvVar appends an environment variable for the pipeline.
func WithEnvVar(key, value string) Option {
	return func(r *Pipeline) {
		r.env.Vars = append(r.env.Vars, func(_ context.Context, vars []EnvVar) []EnvVar {
			return append(vars, EnvVar{Key: key, Value: value})
		})
	}
}

// WithEnvVars appends several environment variable for the pipeline.
func WithEnvVars(b []EnvVar) Option {
	return func(r *Pipeline) {
		r.env.Vars = append(r.env.Vars, func(_ context.Context, a []EnvVar) []EnvVar {
			return append(a, b...)
		})
	}
}

type ContextValueFunc func(context.Context) (string, bool)

// WithEnvVarFunc appends a context-based environment variable for a
// runner.
func WithEnvVarFunc(key string, valueFunc ContextValueFunc) Option {
	return func(r *Pipeline) {
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
	return func(r *Pipeline) {
		r.env.Vars = append(
			r.env.Vars,
			func(ctx context.Context, vars []EnvVar) []EnvVar {
				return append(vars, valuesFunc(ctx)...)
			},
		)
	}
}

// WithEventHandler sets a handler for the pipeline. Setting one will emit
// and event for each process.
func WithEventHandler(handler EventHandler) Option {
	return func(r *Pipeline) {
		r.eventHandler = handler
	}
}

// WithStagePanicHandler sets a panic handler for the stages within a pipeline.
// When a pipeline stage panics, the provided handler will be invoked, allowing
// the client to handle the panic in whatever way they see fit.
//
// Note:
//   - The client is responsible for deciding whether to recover from the panic or panicking again.
//   - If a panic handler is not set, the panic will be propagated normally.
func WithStagePanicHandler(ph StagePanicHandler) Option {
	return func(r *Pipeline) {
		r.panicHandler = ph
	}
}
