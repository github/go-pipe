package pipe

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandStageStdoutFastPath asserts that when a commandStage's stdout is
// an `*os.File`, the file is set as `cmd.Stdout` so that `exec.Cmd` dup's the
// fd into the child process directly. This is one of the optimizations enabled
// by the Stage interface redesign in #21: the subprocess writes straight to
// the caller's destination fd with no Go-side copy stage in between, and the
// subprocess can detect when that fd is closed.
func TestCommandStageStdoutFastPath(t *testing.T) {
	cases := []struct {
		name          string
		closingStdout bool
	}{
		{
			name:          "raw *os.File with closing stdout",
			closingStdout: true,
		},
		{
			name: "raw *os.File with non-closing stdout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			f, err := os.CreateTemp(t.TempDir(), "stdout")
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })

			cmd := exec.Command("true")
			s := CommandStage("true", cmd).(*commandStage)

			stdout := OutputStream{writer: f}
			if tc.closingStdout {
				stdout = ClosingOutput(f)
			}

			require.NoError(t, s.Start(ctx, StageOptions{}, Input(nil), stdout))
			t.Cleanup(func() { _ = s.Wait() })

			gotFile, ok := s.cmd.Stdout.(*os.File)
			require.Truef(t, ok, "expected cmd.Stdout to be *os.File, got %T", s.cmd.Stdout)
			assert.Samef(
				t, f, gotFile,
				"expected cmd.Stdout to be the user-provided *os.File (fd %d), "+
					"got a different *os.File (fd %d). The fd-pass fast path is broken; "+
					"sendfile/zero-copy will not apply.",
				f.Fd(), gotFile.Fd(),
			)
		})
	}
}

// TestCommandStageStdoutFastPathThroughPipeline is the same assertion
// but driven end-to-end through `Pipeline.Start()`, so it also
// exercises the `Pipeline.stdout` plumbing that hands the writer to
// the last stage.
func TestCommandStageStdoutFastPathThroughPipeline(t *testing.T) {
	cases := []struct {
		name   string
		option func(*os.File) Option
	}{
		{
			name:   "WithStdoutCloser(*os.File)",
			option: func(f *os.File) Option { return WithStdoutCloser(f) },
		},
		{
			name:   "WithStdout(*os.File)",
			option: func(f *os.File) Option { return WithStdout(f) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			f, err := os.CreateTemp(t.TempDir(), "stdout")
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })

			cmd := exec.Command("true")
			s := CommandStage("true", cmd).(*commandStage)

			p := New(tc.option(f))
			p.Add(s)
			require.NoError(t, p.Start(ctx))
			stdoutAfterStart := s.cmd.Stdout
			t.Cleanup(func() { _ = p.Wait() })

			gotFile, ok := stdoutAfterStart.(*os.File)
			require.Truef(t, ok, "expected cmd.Stdout to be *os.File, got %T", stdoutAfterStart)
			assert.Samef(
				t, f, gotFile,
				"expected cmd.Stdout to be the user-provided *os.File (fd %d), "+
					"got a different *os.File (fd %d). The fd-pass fast path is broken; "+
					"sendfile/zero-copy will not apply.",
				f.Fd(), gotFile.Fd(),
			)
		})
	}
}
