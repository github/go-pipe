package pipe

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// StageFunc is a function that can be used to power a `goStage`. It
// should read its input from `stdin` and write its output to
// `stdout`. `stdin` and `stdout` will be closed automatically (if
// non-nil) once the function returns.
//
// Neither `stdin` nor `stdout` are necessarily buffered. If the
// `StageFunc` requires buffering, it needs to arrange that itself.
//
// A `StageFunc` is run in a separate goroutine, so it must be careful
// to synchronize any data access aside from reading and writing.
type StageFunc func(ctx context.Context, env Env, stdin io.Reader, stdout io.Writer) error

// FunctionOption configures a Function stage.
type FunctionOption func(*goStage)

// ForbidStdin returns a FunctionOption declaring that the stage must not be
// connected to stdin.
func ForbidStdin() FunctionOption {
	return func(s *goStage) {
		s.requirements.Stdin = StreamForbidden
	}
}

// ForbidStdout returns a FunctionOption declaring that the stage must not be
// connected to stdout.
func ForbidStdout() FunctionOption {
	return func(s *goStage) {
		s.requirements.Stdout = StreamForbidden
	}
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
	stdin io.Reader, stdinCloser io.Closer,
	stdout io.Writer, stdoutCloser io.Closer,
) error {
	r := stdin
	if r == nil {
		// treat nil as empty input.
		r = strings.NewReader("")
	}

	w := stdout
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
			if stdoutCloser != nil {
				if err := stdoutCloser.Close(); err != nil && s.err == nil {
					s.err = fmt.Errorf("error closing stdout for stage %q: %w", s.Name(), err)
				}
			}
			if stdinCloser != nil {
				if err := stdinCloser.Close(); err != nil && s.err == nil {
					s.err = fmt.Errorf("error closing stdin for stage %q: %w", s.Name(), err)
				}
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
