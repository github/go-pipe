package pipe_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/github/go-pipe/pipe"
	"github.com/stretchr/testify/assert"
)

// Tests that `Pipeline.Start()` uses the correct types of pipes in
// various situations.
//
// The type of pipe to use depends on both the source and the consumer
// of the data, including the overall pipeline's stdin and stdout. So
// there are a lot of possibilities to consider.

// Additional values used for the expected types of stdin/stdout:
const (
	IOPreferenceUndefinedNopCloser pipe.IOPreference = iota + 100
	IOPreferenceFileNopCloser
)

func file(t *testing.T) *os.File {
	f, err := os.Open(os.DevNull)
	assert.NoError(t, err)
	return f
}

func readCloser() io.ReadCloser {
	r, w := io.Pipe()
	w.Close()
	return r
}

func writeCloser() io.WriteCloser {
	r, w := io.Pipe()
	r.Close()
	return w
}

func newPipeSniffingStage1(
	retval io.ReadCloser, stdinExpectation pipe.IOPreference,
) *pipeSniffingStage1 {
	return &pipeSniffingStage1{
		StdinExpectation: stdinExpectation,
		retval:           retval,
	}
}

type pipeSniffingStage1 struct {
	StdinExpectation pipe.IOPreference
	retval           io.ReadCloser
	stdin            io.ReadCloser
}

func newPipeSniffingFunc1(stdinExpectation pipe.IOPreference) *pipeSniffingStage1 {
	return newPipeSniffingStage1(readCloser(), stdinExpectation)
}

func newPipeSniffingCmd1(t *testing.T, stdinExpectation pipe.IOPreference) *pipeSniffingStage1 {
	return newPipeSniffingStage1(file(t), stdinExpectation)
}

func (*pipeSniffingStage1) Name() string {
	return "pipe-sniffer"
}

func (s *pipeSniffingStage1) Start(
	_ context.Context, _ pipe.Env, stdin io.ReadCloser,
) (io.ReadCloser, error) {
	s.stdin = stdin
	if stdin != nil {
		_ = stdin.Close()
	}

	return s.retval, nil
}

func (s *pipeSniffingStage1) Wait() error {
	return nil
}

func (s *pipeSniffingStage1) check(t *testing.T, i int) {
	t.Helper()

	checkStdinExpectation(t, i, s.StdinExpectation, s.stdin)
}

func newPipeSniffingStage2(
	stdinPreference, stdinExpectation pipe.IOPreference,
	stdoutPreference, stdoutExpectation pipe.IOPreference,
) *pipeSniffingStage2 {
	return &pipeSniffingStage2{
		prefs: pipe.StagePreferences{
			StdinPreference:  stdinPreference,
			StdoutPreference: stdoutPreference,
		},
		expect: pipe.StagePreferences{
			StdinPreference:  stdinExpectation,
			StdoutPreference: stdoutExpectation,
		},
	}
}

func newPipeSniffingFunc2(
	stdinExpectation, stdoutExpectation pipe.IOPreference,
) *pipeSniffingStage2 {
	return newPipeSniffingStage2(
		pipe.IOPreferenceUndefined, stdinExpectation,
		pipe.IOPreferenceUndefined, stdoutExpectation,
	)
}

func newPipeSniffingCmd2(
	stdinExpectation, stdoutExpectation pipe.IOPreference,
) *pipeSniffingStage2 {
	return newPipeSniffingStage2(
		pipe.IOPreferenceFile, stdinExpectation,
		pipe.IOPreferenceFile, stdoutExpectation,
	)
}

type pipeSniffingStage2 struct {
	prefs  pipe.StagePreferences
	expect pipe.StagePreferences
	stdin  io.ReadCloser
	stdout io.WriteCloser
}

func (*pipeSniffingStage2) Name() string {
	return "pipe-sniffer"
}

func (s *pipeSniffingStage2) Start(
	_ context.Context, _ pipe.Env, _ io.ReadCloser,
) (io.ReadCloser, error) {
	panic("Start() called for a Stage2")
}

func (s *pipeSniffingStage2) Preferences() pipe.StagePreferences {
	return s.prefs
}

func (s *pipeSniffingStage2) Start2(
	_ context.Context, _ pipe.Env, stdin io.ReadCloser, stdout io.WriteCloser,
) error {
	s.stdin = stdin
	if stdin != nil {
		_ = stdin.Close()
	}
	s.stdout = stdout
	if stdout != nil {
		_ = stdout.Close()
	}
	return nil
}

func (s *pipeSniffingStage2) check(t *testing.T, i int) {
	t.Helper()

	checkStdinExpectation(t, i, s.expect.StdinPreference, s.stdin)
	checkStdoutExpectation(t, i, s.expect.StdoutPreference, s.stdout)
}

func (s *pipeSniffingStage2) Wait() error {
	return nil
}

var _ pipe.Stage2 = (*pipeSniffingStage2)(nil)

func ioTypeString(f any) string {
	if f == nil {
		return "nil"
	}
	if f, ok := pipe.UnwrapNopCloser(f); ok {
		return fmt.Sprintf("nopCloser(%s)", ioTypeString(f))
	}
	switch f := f.(type) {
	case *os.File:
		return "*os.File"
	case io.Reader:
		return "other"
	case io.Writer:
		return "other"
	default:
		return fmt.Sprintf("%T", f)
	}
}

func prefString(pref pipe.IOPreference) string {
	switch pref {
	case pipe.IOPreferenceUndefined:
		return "other"
	case pipe.IOPreferenceFile:
		return "*os.File"
	case pipe.IOPreferenceNil:
		return "nil"
	case IOPreferenceUndefinedNopCloser:
		return "nopCloser(other)"
	case IOPreferenceFileNopCloser:
		return "nopCloser(*os.File)"
	default:
		panic(fmt.Sprintf("invalid IOPreference: %d", pref))
	}
}

type ReaderNopCloser interface {
	NopCloserReader() io.Reader
}

func checkStdinExpectation(t *testing.T, i int, pref pipe.IOPreference, stdin io.ReadCloser) {
	t.Helper()

	ioType := ioTypeString(stdin)
	expType := prefString(pref)
	assert.Equalf(
		t, expType, ioType,
		"stage %d stdin: expected %s, got %s (%T)", i, expType, ioType, stdin,
	)
}

type WriterNopCloser interface {
	NopCloserWriter() io.Writer
}

func checkStdoutExpectation(t *testing.T, i int, pref pipe.IOPreference, stdout io.WriteCloser) {
	t.Helper()

	ioType := ioTypeString(stdout)
	expType := prefString(pref)
	assert.Equalf(
		t, expType, ioType,
		"stage %d stdout: expected %s, got %s (%T)", i, expType, ioType, stdout,
	)
}

type checker interface {
	check(t *testing.T, i int)
}

func TestPipeTypes(t *testing.T) {
	ctx := context.Background()

	t.Parallel()

	for _, tc := range []struct {
		name   string
		opts   []pipe.Option
		stages []pipe.Stage
		stdin  io.Reader
		stdout io.Writer
	}{
		{
			name: "func2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(pipe.IOPreferenceNil, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func2-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(IOPreferenceFileNopCloser, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func2-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(pipe.IOPreferenceNil, IOPreferenceFileNopCloser),
			},
		},
		{
			name: "func2-file-stdout-closer",
			opts: []pipe.Option{
				pipe.WithStdoutCloser(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
			},
		},
		{
			name: "func2-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "cmd2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(pipe.IOPreferenceNil, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd2-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(IOPreferenceFileNopCloser, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd2-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(pipe.IOPreferenceNil, IOPreferenceFileNopCloser),
			},
		},
		{
			name: "cmd2-file-stdout-closer",
			opts: []pipe.Option{
				pipe.WithStdoutCloser(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
			},
		},
		{
			name: "cmd2-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "func1",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(pipe.IOPreferenceNil),
			},
		},
		{
			name: "func1-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(IOPreferenceFileNopCloser),
			},
		},
		{
			name: "func1-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(pipe.IOPreferenceNil),
			},
		},
		{
			name: "func1-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(IOPreferenceUndefinedNopCloser),
			},
		},
		{
			name: "func2-func2",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(IOPreferenceFileNopCloser, pipe.IOPreferenceUndefined),
				newPipeSniffingFunc2(pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "func2-cmd2",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
				newPipeSniffingCmd2(pipe.IOPreferenceFile, IOPreferenceFileNopCloser),
			},
		},
		{
			name: "cmd2-func2",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceFile),
				newPipeSniffingFunc2(pipe.IOPreferenceFile, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd2-cmd2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
				newPipeSniffingCmd2(pipe.IOPreferenceFile, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func1-func2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(pipe.IOPreferenceNil),
				newPipeSniffingFunc2(pipe.IOPreferenceUndefined, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd1-func2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd1(t, pipe.IOPreferenceNil),
				newPipeSniffingFunc2(pipe.IOPreferenceFile, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func1-cmd2",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(IOPreferenceUndefinedNopCloser),
				newPipeSniffingCmd2(pipe.IOPreferenceUndefined, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd1-cmd2",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd1(t, IOPreferenceUndefinedNopCloser),
				newPipeSniffingCmd2(pipe.IOPreferenceFile, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func1-func1",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(IOPreferenceFileNopCloser),
				newPipeSniffingFunc1(pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "cmd1-func1",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd1(t, IOPreferenceFileNopCloser),
				newPipeSniffingFunc1(pipe.IOPreferenceFile),
			},
		},
		{
			name: "func1-cmd1",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc1(IOPreferenceFileNopCloser),
				newPipeSniffingCmd1(t, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "func2-func1",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingFunc2(pipe.IOPreferenceNil, pipe.IOPreferenceUndefined),
				newPipeSniffingFunc1(pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "cmd2-func1",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd2(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceFile),
				newPipeSniffingFunc1(pipe.IOPreferenceFile),
			},
		},
		{
			name: "hybrid1",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingStage2(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
				),
				newPipeSniffingStage2(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
					pipe.IOPreferenceFile, pipe.IOPreferenceFile,
				),
				newPipeSniffingStage2(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceFile,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
				),
			},
		},
		{
			name: "hybrid2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingStage2(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceFile,
				),
				newPipeSniffingStage2(
					pipe.IOPreferenceFile, pipe.IOPreferenceFile,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
				),
				newPipeSniffingStage2(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
				),
			},
		},
	} {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := pipe.New(tc.opts...)
			p.Add(tc.stages...)
			assert.NoError(t, p.Run(ctx))
			for i, s := range tc.stages {
				s.(checker).check(t, i)
			}
		})
	}
}
