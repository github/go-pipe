package pipe

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// TestGoStageReceivesConcreteWriterToStdin verifies that a Function stage
// receives its stdin as the caller's concrete type,
// so fast-path interfaces such as io.WriterTo survive. This guards
// against regressions where the concrete type is hidden behind a wrapper.
func TestGoStageReceivesConcreteWriterToStdin(t *testing.T) {
	src := bytes.NewReader([]byte("hello"))

	var got io.Reader
	p := New(WithStdin(src), WithStdout(io.Discard))
	p.Add(Function("capture", func(_ context.Context, _ Env, stdin io.Reader, _ io.Writer) error {
		got = stdin
		_, err := io.Copy(io.Discard, stdin)
		return err
	}))

	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got != io.Reader(src) {
		t.Fatalf("StageFunc stdin = %T %p, want *bytes.Reader %p", got, got, src)
	}
	if _, ok := got.(io.WriterTo); !ok {
		t.Fatalf("stdin %T does not expose io.WriterTo fast path", got)
	}
}
