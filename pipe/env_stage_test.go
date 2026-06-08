package pipe

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
	"time"
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

func TestWithExtraEnvDoesNotShareVarsBackingArray(t *testing.T) {
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
		env := env
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

	if err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}

	wantBase := []EnvVar{
		{Key: "PIPELINE1", Value: "present"},
		{Key: "PIPELINE2", Value: "present"},
		{Key: "PIPELINE3", Value: "present"},
	}
	if want := append(append([]EnvVar(nil), wantBase...), EnvVar{Key: "STAGE", Value: "first"}); !reflect.DeepEqual(firstStageVars, want) {
		t.Fatalf("first stage vars = %#v, want %#v", firstStageVars, want)
	}
	if want := append(append([]EnvVar(nil), wantBase...), EnvVar{Key: "STAGE", Value: "second"}); !reflect.DeepEqual(secondStageVars, want) {
		t.Fatalf("second stage vars = %#v, want %#v", secondStageVars, want)
	}
}

func TestWithExtraEnvAddsCommandEnv(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}

	p := New(WithStdout(stdout))
	p.Add(WithExtraEnv(
		Command("sh", "-c", "printf %s \"$STAGE\""),
		[]EnvVar{{Key: "STAGE", Value: "command"}},
	))

	if err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if got, want := stdout.String(), "command"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestWithExtraEnvPreservesProcessHooks(t *testing.T) {
	stage := WithExtraEnv(Command("true"), nil)

	if _, ok := stage.(processKiller); !ok {
		t.Fatal("WithExtraEnv(Command(...)) does not implement processKiller")
	}
}

func TestWithExtraEnvDoesNotAddProcessHooks(t *testing.T) {
	inner := Function("inner", func(context.Context, Env, io.Reader, io.Writer) error {
		return nil
	})

	stage := WithExtraEnv(inner, nil)

	if _, ok := stage.(processKiller); ok {
		t.Fatal("WithExtraEnv(Function(...)) unexpectedly implements processKiller")
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
