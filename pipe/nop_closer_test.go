package pipe

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestUnwrapReader(t *testing.T) {
	src := bytes.NewReader([]byte("payload"))

	if got := UnwrapReader(newReaderNopCloser(src)); got != io.Reader(src) {
		t.Errorf("UnwrapReader(wrapped) = %T %p, want %p", got, got, src)
	}

	// A non-wrapped reader passes through unchanged.
	if got := UnwrapReader(src); got != io.Reader(src) {
		t.Errorf("UnwrapReader(plain) = %T %p, want %p", got, got, src)
	}

	if got := UnwrapReader(nil); got != nil {
		t.Errorf("UnwrapReader(nil) = %v, want nil", got)
	}
}

func TestUnwrapWriter(t *testing.T) {
	dst := &bytes.Buffer{}

	if got := UnwrapWriter(writerNopCloser{dst}); got != io.Writer(dst) {
		t.Errorf("UnwrapWriter(wrapped) = %T %p, want %p", got, got, dst)
	}

	// A non-wrapped writer passes through unchanged.
	if got := UnwrapWriter(dst); got != io.Writer(dst) {
		t.Errorf("UnwrapWriter(plain) = %T %p, want %p", got, got, dst)
	}

	if got := UnwrapWriter(nil); got != nil {
		t.Errorf("UnwrapWriter(nil) = %v, want nil", got)
	}
}

// TestGoStageUnwrapsWriterToStdin verifies that a Function stage
// receives its stdin already unwrapped to the caller's concrete type,
// so fast-path interfaces such as io.WriterTo survive. This guards
// against the regression where goStage only unwrapped one of the
// internal nop-closer wrapper types.
func TestGoStageUnwrapsWriterToStdin(t *testing.T) {
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
		t.Fatalf("StageFunc stdin = %T %p, want unwrapped *bytes.Reader %p", got, got, src)
	}
	if _, ok := got.(io.WriterTo); !ok {
		t.Fatalf("unwrapped stdin %T does not expose io.WriterTo fast path", got)
	}
}
