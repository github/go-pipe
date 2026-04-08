package pipe

import (
	"context"
	"fmt"
	"io"
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

// Function returns a pipeline `Stage` that will run a `StageFunc` in
// a separate goroutine to process the data. See `StageFunc` for more
// information.
func Function(name string, f StageFunc) Stage {
	return &goStage{
		name: name,
		f:    f,
		done: make(chan struct{}),
	}
}

// goStage is a `Stage` that does its work by running an arbitrary
// `stageFunc` in a goroutine.
type goStage struct {
	name         string
	f            StageFunc
	done         chan struct{}
	err          error
	panicHandler StagePanicHandler
}

var (
	_ Stage                  = (*goStage)(nil)
	_ StagePanicHandlerAware = (*goStage)(nil)
)

func (s *goStage) Name() string {
	return s.name
}

func (s *goStage) SetPanicHandler(ph StagePanicHandler) {
	s.panicHandler = ph
}

func (s *goStage) Preferences() StagePreferences {
	return StagePreferences{
		StdinPreference:  IOPreferenceUndefined,
		StdoutPreference: IOPreferenceUndefined,
	}
}

func (s *goStage) Start(
	ctx context.Context, env Env, stdin io.ReadCloser, stdout io.WriteCloser,
) error {
	var r io.Reader = stdin
	if stdin, ok := stdin.(readerNopCloser); ok {
		r = stdin.Reader
	}

	var w io.Writer = stdout
	if stdout, ok := stdout.(writerNopCloser); ok {
		w = stdout.Writer
	}

	go func() {
		defer close(s.done)
		defer func() {
			if stdin != nil {
				if err := stdin.Close(); err != nil && s.err == nil {
					s.err = fmt.Errorf("error closing stdin for stage %q: %w", s.Name(), err)
				}
			}
		}()
		defer func() {
			if stdout != nil {
				if err := stdout.Close(); err != nil && s.err == nil {
					s.err = fmt.Errorf("error closing stdout for stage %q: %w", s.Name(), err)
				}
			}
		}()
		defer s.recoverPanic()

		s.err = s.f(ctx, env, r, w)
	}()

	return nil
}

func (s *goStage) Wait() error {
	<-s.done
	return s.err
}

func (s *goStage) recoverPanic() {
	if s.panicHandler == nil {
		return
	}

	if p := recover(); p != nil {
		s.err = s.panicHandler(p)
	}
}
