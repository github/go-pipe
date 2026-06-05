package pipe

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
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

// TestGoStageHonorsCloseFlags verifies that a Function stage closes
// stdin/stdout iff the corresponding close flag is true.
func TestGoStageHonorsCloseFlags(t *testing.T) {
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

			if err := s.Start(
				context.Background(), StageOptions{},
				in, !tc.leaveIn,
				out, !tc.leaveOut,
			); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if err := s.Wait(); err != nil {
				t.Fatalf("Wait: %v", err)
			}

			if got, want := in.closed.Load(), !tc.leaveIn; got != want {
				t.Errorf("stdin closed = %v, want %v (closeStdin=%v)", got, want, !tc.leaveIn)
			}
			if got, want := out.closed.Load(), !tc.leaveOut; got != want {
				t.Errorf("stdout closed = %v, want %v (closeStdout=%v)", got, want, !tc.leaveOut)
			}
		})
	}
}

func TestStagePanicsWhenOwnedStreamIsNotCloseable(t *testing.T) {
	s := Function("f", func(_ context.Context, _ Env, _ io.Reader, _ io.Writer) error {
		return nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected Start to panic")
		}
		if !strings.Contains(r.(string), "does not implement io.Closer") {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()

	_ = s.Start(
		context.Background(), StageOptions{},
		strings.NewReader("not closeable"), true,
		nil, false,
	)
}

// TestCommandStageHonorsCloseStdin verifies that a command stage closes a
// non-file stdin (a "late" closer) iff closeStdin is true. An empty
// reader is used so exec.Cmd's input-copy goroutine sees EOF promptly.
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

			if err := s.Start(
				context.Background(), StageOptions{},
				in, !leave,
				nil, false,
			); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if err := s.Wait(); err != nil {
				t.Fatalf("Wait: %v", err)
			}

			if got, want := in.closed.Load(), !leave; got != want {
				t.Errorf("stdin closed = %v, want %v (closeStdin=%v)", got, want, !leave)
			}
		})
	}
}

// TestCommandStageHonorsCloseStdout verifies the stdout counterpart: a
// non-file stdout (routed through the pooled-copy path) is closed iff
// closeStdout is true.
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

			if err := s.Start(
				context.Background(), StageOptions{},
				nil, false,
				out, !leave,
			); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if err := s.Wait(); err != nil {
				t.Fatalf("Wait: %v", err)
			}

			if got, want := out.closed.Load(), !leave; got != want {
				t.Errorf("stdout closed = %v, want %v (closeStdout=%v)", got, want, !leave)
			}
		})
	}
}
