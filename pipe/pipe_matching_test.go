package pipe_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/github/go-pipe/v2/pipe"
	"github.com/stretchr/testify/assert"
)

// Tests that `Pipeline.Start()` uses the correct types of pipes in
// various situations.
//
// The type of pipe to use depends on both the source and the consumer
// of the data, including the overall pipeline's stdin and stdout. So
// there are a lot of possibilities to consider.

type ioExpectation int

const (
	expectOther ioExpectation = iota
	expectFile

	// expectNil means that the stage should be passed a nil stdin / stdout,
	// which happens at the beginning / end of a pipeline when no overall
	// stdin / stdout is configured.
	expectNil
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
	stdinNeedsFile bool, stdinExpectation ioExpectation,
	stdoutNeedsFile bool, stdoutExpectation ioExpectation,
) *pipeSniffingStage {
	return &pipeSniffingStage{
		requirements: pipe.StageRequirements{
			StdinNeedsFile:  stdinNeedsFile,
			StdoutNeedsFile: stdoutNeedsFile,
		},
		expect: pipeExpectations{
			stdin:  stdinExpectation,
			stdout: stdoutExpectation,
		},
	}
}

func newPipeSniffingFunc(
	stdinExpectation, stdoutExpectation ioExpectation,
) *pipeSniffingStage {
	return newPipeSniffingStage(
		false, stdinExpectation,
		false, stdoutExpectation,
	)
}

func newPipeSniffingCmd(
	stdinExpectation, stdoutExpectation ioExpectation,
) *pipeSniffingStage {
	return newPipeSniffingStage(
		true, stdinExpectation,
		true, stdoutExpectation,
	)
}

type pipeExpectations struct {
	stdin  ioExpectation
	stdout ioExpectation
}

type pipeSniffingStage struct {
	requirements pipe.StageRequirements
	expect       pipeExpectations
	stdin        io.Reader
	stdout       io.Writer
}

func (*pipeSniffingStage) Name() string {
	return "pipe-sniffer"
}

func (s *pipeSniffingStage) Requirements() pipe.StageRequirements {
	return s.requirements
}

func (s *pipeSniffingStage) Start(
	_ context.Context, _ pipe.StageOptions,
	stdin io.Reader, stdinCloser io.Closer,
	stdout io.Writer, stdoutCloser io.Closer,
) error {
	s.stdin = stdin
	if stdinCloser != nil {
		_ = stdinCloser.Close()
	}
	s.stdout = stdout
	if stdoutCloser != nil {
		_ = stdoutCloser.Close()
	}
	return nil
}

func (s *pipeSniffingStage) check(t *testing.T, i int) {
	t.Helper()

	checkStdinExpectation(t, i, s.expect.stdin, s.stdin)
	checkStdoutExpectation(t, i, s.expect.stdout, s.stdout)
}

func (s *pipeSniffingStage) Wait() error {
	return nil
}

var _ pipe.Stage = (*pipeSniffingStage)(nil)

func ioTypeString(f any) string {
	if f == nil {
		return "nil"
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

func expectationString(expect ioExpectation) string {
	switch expect {
	case expectOther:
		return "other"
	case expectFile:
		return "*os.File"
	case expectNil:
		return "nil"
	default:
		panic(fmt.Sprintf("invalid ioExpectation: %d", expect))
	}
}

func checkStdinExpectation(t *testing.T, i int, expect ioExpectation, stdin io.Reader) {
	t.Helper()

	ioType := ioTypeString(stdin)
	expType := expectationString(expect)
	assert.Equalf(
		t, expType, ioType,
		"stage %d stdin: expected %s, got %s (%T)", i, expType, ioType, stdin,
	)
}

func checkStdoutExpectation(t *testing.T, i int, expect ioExpectation, stdout io.Writer) {
	t.Helper()

	ioType := ioTypeString(stdout)
	expType := expectationString(expect)
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
				newPipeSniffingFunc(expectNil, expectNil),
			},
		},
		{
			name: "func-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(expectFile, expectNil),
			},
		},
		{
			name: "func-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(expectNil, expectFile),
			},
		},
		{
			name: "func-file-stdout-closer",
			opts: []pipe.Option{
				pipe.WithStdoutCloser(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(expectNil, expectFile),
			},
		},
		{
			name: "func-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(expectOther, expectOther),
			},
		},
		{
			name: "cmd",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectNil, expectNil),
			},
		},
		{
			name: "cmd-file-stdin",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectFile, expectNil),
			},
		},
		{
			name: "cmd-file-stdout",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectNil, expectFile),
			},
		},
		{
			name: "cmd-file-stdout-closer",
			opts: []pipe.Option{
				pipe.WithStdoutCloser(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectNil, expectFile),
			},
		},
		{
			name: "cmd-file-stdin-other-stdout-closer-other",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectOther, expectOther),
			},
		},
		{
			name: "func-func",
			opts: []pipe.Option{
				pipe.WithStdin(file(t)),
				pipe.WithStdoutCloser(writeCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(expectFile, expectOther),
				newPipeSniffingFunc(expectOther, expectOther),
			},
		},
		{
			name: "func-cmd",
			opts: []pipe.Option{
				pipe.WithStdout(file(t)),
			},
			stages: []pipe.Stage{
				newPipeSniffingFunc(expectNil, expectFile),
				newPipeSniffingCmd(expectFile, expectFile),
			},
		},
		{
			name: "cmd-func",
			opts: []pipe.Option{
				pipe.WithStdin(readCloser()),
			},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectOther, expectFile),
				newPipeSniffingFunc(expectFile, expectNil),
			},
		},
		{
			name: "cmd-cmd",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingCmd(expectNil, expectFile),
				newPipeSniffingCmd(expectFile, expectNil),
			},
		},
		{
			name: "hybrid1",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingStage(
					false, expectNil,
					false, expectOther,
				),
				newPipeSniffingStage(
					false, expectOther,
					true, expectFile,
				),
				newPipeSniffingStage(
					false, expectFile,
					false, expectNil,
				),
			},
		},
		{
			name: "hybrid2",
			opts: []pipe.Option{},
			stages: []pipe.Stage{
				newPipeSniffingStage(
					false, expectNil,
					false, expectFile,
				),
				newPipeSniffingStage(
					true, expectFile,
					false, expectOther,
				),
				newPipeSniffingStage(
					false, expectOther,
					false, expectNil,
				),
			},
		},
	} {
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
