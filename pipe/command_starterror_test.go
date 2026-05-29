package pipe_test

import (
	"bytes"
	"context"
	"os/exec"
	"sync/atomic"
	"testing"

	"github.com/github/go-pipe/pipe"
)

// TestCommandStageStartFailureNoRace verifies that when `cmd.Start()`
// fails (e.g. command not found), the goroutine that
// `setupPooledStdout` spawned does not leak past `Pipeline.Run()`.
// `bytes.Buffer.ReadFrom` writes to the buffer's slice header via
// `grow()` before its first `Read()`, so a leaked goroutine races
// with the caller's access to the destination buffer once Run
// returns the error. Run a tight loop so `-race` is likely to catch
// any regression.
func TestCommandStageStartFailureNoRace(t *testing.T) {
	for i := 0; i < 50; i++ {
		var buf bytes.Buffer
		p := pipe.New(pipe.WithStdout(&buf))
		p.Add(pipe.CommandStage("nope", exec.Command("this-binary-does-not-exist-xyz123")))
		if err := p.Run(context.Background()); err == nil {
			t.Fatalf("expected error from non-existent command, got nil")
		}
		_ = buf.String()
	}
}

// trackingWriteCloser is a non-`*os.File` `io.WriteCloser` that records
// whether it has been closed. Because it isn't an `*os.File`, a command
// stage routes it through `setupPooledStdout` and closes it as a "late
// closer" (i.e. only after the command finishes / cleanup runs).
type trackingWriteCloser struct {
	closed atomic.Bool
}

func (w *trackingWriteCloser) Write(p []byte) (int, error) { return len(p), nil }

func (w *trackingWriteCloser) Close() error {
	w.closed.Store(true)
	return nil
}

// TestCommandStageStartFailureClosesLateClosers verifies that a
// `WithStdoutCloser` on the last stage is closed even when `cmd.Start()`
// fails. The closer is registered as a "late closer," which is normally
// drained by `Wait()`; since `Wait()` never runs when `Start()` fails,
// the start-failure cleanup path must close it instead.
func TestCommandStageStartFailureClosesLateClosers(t *testing.T) {
	w := &trackingWriteCloser{}
	p := pipe.New(pipe.WithStdoutCloser(w))
	p.Add(pipe.CommandStage("nope", exec.Command("this-binary-does-not-exist-xyz123")))
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error from non-existent command, got nil")
	}
	if !w.closed.Load() {
		t.Fatalf("expected late closer to be closed after Start() failure")
	}
}
