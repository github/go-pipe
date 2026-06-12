package pipe

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readCloseSpy records whether Close was called.
type readCloseSpy struct {
	io.Reader
	closed atomic.Bool
}

func (r *readCloseSpy) Close() error {
	r.closed.Store(true)
	return nil
}

// writeCloseSpy records whether Close was called.
type writeCloseSpy struct {
	io.Writer
	closed atomic.Bool
}

func (w *writeCloseSpy) Close() error {
	w.closed.Store(true)
	return nil
}

// TestGoStageHonorsStreamOwnership verifies that a Function stage closes
// stdin/stdout iff the corresponding stream is closing.
func TestGoStageHonorsStreamOwnership(t *testing.T) {
	cases := []struct {
		name              string
		leaveIn, leaveOut bool
	}{
		{"own both", false, false},
		{"leave stdin open", true, false},
		{"leave stdout open", false, true},
		{"leave both open", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &readCloseSpy{Reader: strings.NewReader("hi")}
			out := &writeCloseSpy{Writer: io.Discard}

			s := Function("f", func(_ context.Context, _ Env, stdin io.Reader, stdout io.Writer) error {
				_, err := io.Copy(stdout, stdin)
				return err
			})

			require.NoError(t, s.Start(
				context.Background(), StageOptions{},
				inputForTest(in, !tc.leaveIn),
				outputForTest(out, !tc.leaveOut),
			))
			require.NoError(t, s.Wait())

			assert.Equal(t, !tc.leaveIn, in.closed.Load(), "closing stdin=%v", !tc.leaveIn)
			assert.Equal(t, !tc.leaveOut, out.closed.Load(), "closing stdout=%v", !tc.leaveOut)
		})
	}
}

func TestStreamConstructorsPreserveOwnershipAndDynamicType(t *testing.T) {
	borrowedInput := strings.NewReader("borrowed")
	assert.Same(t, borrowedInput, Input(borrowedInput).Reader())
	assert.Nil(t, Input(borrowedInput).Closer())

	ownedInput := &readCloseSpy{Reader: strings.NewReader("owned")}
	assert.Same(t, ownedInput, ClosingInput(ownedInput).Reader())
	assert.Same(t, ownedInput, ClosingInput(ownedInput).Closer())

	borrowedOutput := &strings.Builder{}
	assert.Same(t, borrowedOutput, Output(borrowedOutput).Writer())
	assert.Nil(t, Output(borrowedOutput).Closer())

	ownedOutput := &writeCloseSpy{Writer: io.Discard}
	assert.Same(t, ownedOutput, ClosingOutput(ownedOutput).Writer())
	assert.Same(t, ownedOutput, ClosingOutput(ownedOutput).Closer())
}

// TestCommandStageHonorsCloseStdin verifies that a command stage closes a
// non-file stdin (a "late" closer) iff the input stream is closing. An
// empty reader is used so exec.Cmd's input-copy goroutine sees EOF promptly.
func TestCommandStageHonorsCloseStdin(t *testing.T) {
	for _, leave := range []bool{false, true} {
		name := "owns stdin"
		if leave {
			name = "leaves stdin open"
		}
		t.Run(name, func(t *testing.T) {
			in := &readCloseSpy{Reader: strings.NewReader("")}

			cmd := exec.Command("true")
			s := CommandStage("true", cmd).(*commandStage)

			require.NoError(t, s.Start(
				context.Background(), StageOptions{},
				inputForTest(in, !leave),
				Output(nil),
			))
			require.NoError(t, s.Wait())

			assert.Equal(t, !leave, in.closed.Load(), "closing stdin=%v", !leave)
		})
	}
}

// TestCommandStageHonorsCloseStdout verifies the stdout counterpart: a
// non-file stdout (routed through the pooled-copy path) is closed iff
// the output stream is closing.
func TestCommandStageHonorsCloseStdout(t *testing.T) {
	for _, leave := range []bool{false, true} {
		name := "owns stdout"
		if leave {
			name = "leaves stdout open"
		}
		t.Run(name, func(t *testing.T) {
			out := &writeCloseSpy{Writer: io.Discard}

			cmd := exec.Command("true")
			s := CommandStage("true", cmd).(*commandStage)

			require.NoError(t, s.Start(
				context.Background(), StageOptions{},
				Input(nil),
				outputForTest(out, !leave),
			))
			require.NoError(t, s.Wait())

			assert.Equal(t, !leave, out.closed.Load(), "closing stdout=%v", !leave)
		})
	}
}

func inputForTest(r io.ReadCloser, closing bool) InputStream {
	if closing {
		return ClosingInput(r)
	}
	return Input(r)
}

func outputForTest(w io.WriteCloser, closing bool) OutputStream {
	if closing {
		return ClosingOutput(w)
	}
	return Output(w)
}
