package pipe

import (
	"context"
	"io"
)

// WithExtraEnv returns a Stage that adds env to the environment seen by inner.
func WithExtraEnv(inner Stage, env []EnvVar) Stage {
	stage := &stageWithExtraEnv{
		inner: inner,
		env:   env,
	}
	if processProvider, ok := inner.(processProvider); ok {
		return &processStageWithExtraEnv{
			stageWithExtraEnv: stage,
			processProvider:   processProvider,
		}
	}
	return stage
}

type processStageWithExtraEnv struct {
	processProvider
	*stageWithExtraEnv
}

type stageWithExtraEnv struct {
	inner Stage
	env   []EnvVar
}

func (s *stageWithExtraEnv) Name() string {
	return s.inner.Name() + " (with extra env vars)"
}

func (s *stageWithExtraEnv) Requirements() StageRequirements {
	return s.inner.Requirements()
}

func (s *stageWithExtraEnv) Start(
	ctx context.Context, opts StageOptions,
	stdin io.Reader, closeStdin bool,
	stdout io.Writer, closeStdout bool,
) error {
	opts.Vars = append(opts.Vars[:len(opts.Vars):len(opts.Vars)], func(_ context.Context, vars []EnvVar) []EnvVar {
		return append(vars, s.env...)
	})
	return s.inner.Start(ctx, opts, stdin, closeStdin, stdout, closeStdout)
}

func (s *stageWithExtraEnv) Wait() error {
	return s.inner.Wait()
}
