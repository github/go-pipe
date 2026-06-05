package pipe

import (
	"context"
	"io"
	"reflect"
	"testing"
)

func collectEnvVars(ctx context.Context, env Env) []EnvVar {
	var vars []EnvVar
	for _, fn := range env.Vars {
		vars = fn(ctx, vars)
	}
	return vars
}

func TestWithExtraEnvAddsStageLocalVars(t *testing.T) {
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
			[]EnvVar{
				{Key: "STAGE", Value: "first"},
			},
		),
		Function("second", func(ctx context.Context, env Env, _ io.Reader, _ io.Writer) error {
			secondStageVars = collectEnvVars(ctx, env)
			return nil
		}),
	)

	if err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if want := []EnvVar{{Key: "PIPELINE", Value: "present"}, {Key: "STAGE", Value: "first"}}; !reflect.DeepEqual(firstStageVars, want) {
		t.Fatalf("first stage vars = %#v, want %#v", firstStageVars, want)
	}
	if want := []EnvVar{{Key: "PIPELINE", Value: "present"}}; !reflect.DeepEqual(secondStageVars, want) {
		t.Fatalf("second stage vars = %#v, want %#v", secondStageVars, want)
	}
}

func TestWithExtraEnvPreservesStageMetadata(t *testing.T) {
	inner := Function("inner", func(context.Context, Env, io.Reader, io.Writer) error {
		return nil
	}, ForbidStdin(), ForbidStdout())

	stage := WithExtraEnv(inner, nil)

	if got, want := stage.Name(), "inner (with extra env vars)"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
	if got, want := stage.Requirements(), inner.Requirements(); got != want {
		t.Fatalf("Requirements() = %#v, want %#v", got, want)
	}
}
