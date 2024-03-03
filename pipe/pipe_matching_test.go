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

func newPipeSniffingStage(
	stdinPreference, stdinExpectation pipe.IOPreference,
	stdoutPreference, stdoutExpectation pipe.IOPreference,
) *pipeSniffingStage {
	return &pipeSniffingStage{
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

func newPipeSniffingFunc(
	stdinExpectation, stdoutExpectation pipe.IOPreference,
) *pipeSniffingStage {
	return newPipeSniffingStage(
		pipe.IOPreferenceUndefined, stdinExpectation,
		pipe.IOPreferenceUndefined, stdoutExpectation,
	)
}

func newPipeSniffingCmd(
	stdinExpectation, stdoutExpectation pipe.IOPreference,
) *pipeSniffingStage {
	return newPipeSniffingStage(
		pipe.IOPreferenceFile, stdinExpectation,
		pipe.IOPreferenceFile, stdoutExpectation,
	)
}

type pipeSniffingStage struct {
	prefs  pipe.StagePreferences
	expect pipe.StagePreferences
	stdin  io.ReadCloser
	stdout io.WriteCloser
}

func (*pipeSniffingStage) Name() string {
	return "pipe-sniffer"
}

func (s *pipeSniffingStage) Preferences() pipe.StagePreferences {
	return s.prefs
}

func (s *pipeSniffingStage) Start(
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

func (s *pipeSniffingStage) check(t *testing.T, i int) {
	t.Helper()

	checkStdinExpectation(t, i, s.expect.StdinPreference, s.stdin)
	checkStdoutExpectation(t, i, s.expect.StdoutPreference, s.stdout)
}

func (s *pipeSniffingStage) Wait() error {
	return nil
}

var _ pipe.Stage = (*pipeSniffingStage)(nil)

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
			name: "func",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingFunc(pipe.IOPreferenceNil, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(IOPreferenceFileNopCloser, pipe.IOPreferenceNil),
			},
		},
		{
			name: "func-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(pipe.IOPreferenceNil, IOPreferenceFileNopCloser),
			},
		},
		{
			name: "func-file-stdout-closer",
			opts: []pipe.Option{
				pipe.WithStdoutCloser(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
			},
		},
		{
			name: "func-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "cmd",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd(pipe.IOPreferenceNil, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(IOPreferenceFileNopCloser, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(pipe.IOPreferenceNil, IOPreferenceFileNopCloser),
			},
		},
		{
			name: "cmd-file-stdout-closer",
			opts: []pipe.Option{
				pipe.WithStdoutCloser(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
			},
		},
		{
			name: "cmd-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "func-func",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(IOPreferenceFileNopCloser, pipe.IOPreferenceUndefined),
				newPipeSniffingFunc(pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined),
			},
		},
		{
			name: "func-cmd",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
				newPipeSniffingCmd(pipe.IOPreferenceFile, IOPreferenceFileNopCloser),
			},
		},
		{
			name: "cmd-func",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(IOPreferenceUndefinedNopCloser, pipe.IOPreferenceFile),
				newPipeSniffingFunc(pipe.IOPreferenceFile, pipe.IOPreferenceNil),
			},
		},
		{
			name: "cmd-cmd",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd(pipe.IOPreferenceNil, pipe.IOPreferenceFile),
				newPipeSniffingCmd(pipe.IOPreferenceFile, pipe.IOPreferenceNil),
			},
		},
		{
			name: "hybrid1",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingStage(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
				),
				newPipeSniffingStage(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
					pipe.IOPreferenceFile, pipe.IOPreferenceFile,
				),
				newPipeSniffingStage(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceFile,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
				),
			},
		},
		{
			name: "hybrid2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingStage(
					pipe.IOPreferenceUndefined, pipe.IOPreferenceNil,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceFile,
				),
				newPipeSniffingStage(
					pipe.IOPreferenceFile, pipe.IOPreferenceFile,
					pipe.IOPreferenceUndefined, pipe.IOPreferenceUndefined,
				),
				newPipeSniffingStage(
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
