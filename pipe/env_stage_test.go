package pipe

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectEnvVars(ctx context.Context, env Env) []EnvVar {
	var vars []EnvVar
	for _, fn := range env.Vars {
		vars = fn(ctx, vars)
	}
	return vars
}

func lastEnvValue(ctx context.Context, env Env, key string) (string, bool) {
	var (
		value string
		ok    bool
	)
	for _, envVar := range collectEnvVars(ctx, env) {
		if envVar.Key == key {
			value = envVar.Value
			ok = true
		}
	}
	return value, ok
}

func TestWithExtraEnvAddsStageLocalVars(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var firstStageVars []EnvVar
	var secondStageVars []EnvVar

	p := New(WithEnvVar("PIPELINE", "present"))
	p.Add(
		WithExtraEnv(
			Function("first", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
				firstStageVars = collectEnvVars(ctx, env)
				return nil
			}),
			[]EnvVar{{Key: "STAGE", Value: "first"}},
		),
		Function("second", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
			secondStageVars = collectEnvVars(ctx, env)
			return nil
		}),
	)

	require.NoError(t, p.Run(ctx))
	assert.Equal(t, []EnvVar{{Key: "PIPELINE", Value: "present"}, {Key: "STAGE", Value: "first"}}, firstStageVars)
	assert.Equal(t, []EnvVar{{Key: "PIPELINE", Value: "present"}}, secondStageVars)
}

func TestWithExtraEnvStageLocalVarsOverridePipelineVars(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var firstStageValue string
	var secondStageValue string

	p := New(WithEnvVar("STAGE", "pipeline"))
	p.Add(
		WithExtraEnv(
			Function("first", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
				var ok bool
				firstStageValue, ok = lastEnvValue(ctx, env, "STAGE")
				require.True(t, ok)
				return nil
			}),
			[]EnvVar{{Key: "STAGE", Value: "stage-local"}},
		),
		Function("second", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
			var ok bool
			secondStageValue, ok = lastEnvValue(ctx, env, "STAGE")
			require.True(t, ok)
			return nil
		}),
	)

	require.NoError(t, p.Run(ctx))
	assert.Equal(t, "stage-local", firstStageValue)
	assert.Equal(t, "pipeline", secondStageValue)
}

func TestWithExtraEnvDoesNotShareVarsBackingArray(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	allowFirstStage := make(chan struct{})
	var firstStageVars []EnvVar
	var secondStageVars []EnvVar

	baseVars := make([]AppendVars, 0, 4)
	for _, env := range []EnvVar{
		{Key: "PIPELINE1", Value: "present"},
		{Key: "PIPELINE2", Value: "present"},
		{Key: "PIPELINE3", Value: "present"},
	} {
		baseVars = append(baseVars, func(_ context.Context, vars []EnvVar) []EnvVar {
			return append(vars, env)
		})
	}

	p := New(func(p *Pipeline) {
		p.env.Vars = baseVars
	})
	p.Add(
		WithExtraEnv(
			Function("first", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
				select {
				case <-allowFirstStage:
				case <-ctx.Done():
					return ctx.Err()
				}
				firstStageVars = collectEnvVars(ctx, env)
				return nil
			}),
			[]EnvVar{{Key: "STAGE", Value: "first"}},
		),
		WithExtraEnv(
			Function("second", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
				secondStageVars = collectEnvVars(ctx, env)
				close(allowFirstStage)
				return nil
			}),
			[]EnvVar{{Key: "STAGE", Value: "second"}},
		),
	)

	require.NoError(t, p.Run(ctx))

	wantBase := []EnvVar{
		{Key: "PIPELINE1", Value: "present"},
		{Key: "PIPELINE2", Value: "present"},
		{Key: "PIPELINE3", Value: "present"},
	}
	assert.Equal(t, append(append([]EnvVar(nil), wantBase...), EnvVar{Key: "STAGE", Value: "first"}), firstStageVars)
	assert.Equal(t, append(append([]EnvVar(nil), wantBase...), EnvVar{Key: "STAGE", Value: "second"}), secondStageVars)
}

func TestWithExtraEnvAddsCommandEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stdout := &bytes.Buffer{}

	p := New(WithStdout(stdout))
	p.Add(WithExtraEnv(
		Command("sh", "-c", "printf %s \"$STAGE\""),
		[]EnvVar{{Key: "STAGE", Value: "command"}},
	))

	require.NoError(t, p.Run(ctx))
	assert.Equal(t, "command", stdout.String())
}

func TestWithExtraEnvPreservesProcessHooks(t *testing.T) {
	t.Parallel()

	stage := WithExtraEnv(Command("true"), nil)
	assert.Implements(t, (*processKiller)(nil), stage)
}

func TestWithExtraEnvDoesNotAddProcessHooks(t *testing.T) {
	t.Parallel()

	inner := Function("inner", func(context.Context, Env, io.Reader, io.Writer) error {
		return nil
	})

	stage := WithExtraEnv(inner, nil)
	assert.NotImplements(t, (*processKiller)(nil), stage)
}

func TestWithExtraEnvPreservesStageMetadata(t *testing.T) {
	t.Parallel()

	inner := Function("inner", func(context.Context, Env, io.Reader, io.Writer) error {
		return nil
	}, ForbidStdin(), ForbidStdout())

	stage := WithExtraEnv(inner, nil)
	assert.Equal(t, "inner (with extra env vars)", stage.Name())
	assert.Equal(t, inner.Requirements(), stage.Requirements())
}
