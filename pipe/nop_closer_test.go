package pipe

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, p.Run(context.Background()))

	assert.Same(t, src, got)
	assert.Implements(t, (*io.WriterTo)(nil), got)
}
