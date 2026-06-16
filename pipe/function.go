package pipe

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// StageFunc is a function that can be used to power a `goStage`. It
// should read its input from `stdin` and write its output to
// `stdout`. The Function stage closes `stdin` and `stdout` after the
// function returns only when the pipeline gave the stage ownership of
// those streams; StageFunc implementations should not close them
// directly.
//
// Neither `stdin` nor `stdout` are necessarily buffered. If the
// `StageFunc` requires buffering, it needs to arrange that itself.
//
// A later stage can stop reading before this function has written all
// of its output. In that case, writes to `stdout` can fail with an
// error matched by `IsPipeError`. If the function only writes output
// and is otherwise stateless, callers can usually wrap the stage with
// `IgnoreError(stage, IsPipeError)`. If the function also updates
// producer-owned state, metrics, cursors, or other side effects that
// depend on how much output was produced, it should bring those side
// effects to a consistent point before returning the write error.
//
// A `StageFunc` is run in a separate goroutine, so it must be careful
// to synchronize any data access aside from reading and writing.
type StageFunc func(ctx context.Context, env Env, stdin io.Reader, stdout io.Writer) error

// FunctionOption configures a Function stage.
type FunctionOption func(*goStage)

// WithStdinRequirement returns a FunctionOption declaring the stage's stdin
// requirement.
func WithStdinRequirement(requirement StreamRequirement) FunctionOption {
	return func(s *goStage) {
		s.requirements.Stdin = requirement
	}
}

// WithStdoutRequirement returns a FunctionOption declaring the stage's stdout
// requirement.
func WithStdoutRequirement(requirement StreamRequirement) FunctionOption {
	return func(s *goStage) {
		s.requirements.Stdout = requirement
	}
}

// ForbidStdin returns a FunctionOption declaring that the stage must not be
// connected to stdin.
func ForbidStdin() FunctionOption {
	return WithStdinRequirement(StreamForbidden)
}

// ForbidStdout returns a FunctionOption declaring that the stage must not be
// connected to stdout.
func ForbidStdout() FunctionOption {
	return WithStdoutRequirement(StreamForbidden)
}

// Function returns a pipeline `Stage` that will run a `StageFunc` in
// a separate goroutine to process the data. See `StageFunc` for more
// information.
func Function(name string, f StageFunc, opts ...FunctionOption) Stage {
	stage := &goStage{
		name:         name,
		f:            f,
		done:         make(chan struct{}),
		requirements: StageRequirements{},
	}
	for _, opt := range opts {
		opt(stage)
	}
	return stage
}

// goStage is a `Stage` that does its work by running an arbitrary
// `stageFunc` in a goroutine.
type goStage struct {
	name         string
	f            StageFunc
	done         chan struct{}
	requirements StageRequirements
	err          error
}

var _ Stage = (*goStage)(nil)

func (s *goStage) Name() string {
	return s.name
}

func (s *goStage) Requirements() StageRequirements {
	return s.requirements
}

func (s *goStage) Start(
	ctx context.Context, opts StageOptions,
	stdin *InputStream, stdout *OutputStream,
) error {
	r := stdin.Reader()
	if r == nil {
		// treat nil as empty input.
		r = strings.NewReader("")
	}

	w := stdout.Writer()
	if w == nil {
		// treat nil output as /dev/null
		w = io.Discard
	}

	go func() {
		defer func() {
			if opts.PanicHandler != nil {
				if p := recover(); p != nil {
					s.err = opts.PanicHandler(p)
				}
			}
			if err := stdout.Close(); err != nil && s.err == nil {
				s.err = fmt.Errorf("error closing stdout for stage %q: %w", s.Name(), err)
			}
			if err := stdin.Close(); err != nil && s.err == nil {
				s.err = fmt.Errorf("error closing stdin for stage %q: %w", s.Name(), err)
			}
			close(s.done)
		}()
		s.err = s.f(ctx, opts.Env, r, w)
	}()

	return nil
}

func (s *goStage) Wait() error {
	<-s.done
	return s.err
}
