package pipe_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/github/go-pipe/v2/pipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const panicChildEnv = "GO_PIPE_FUNCTION_PANIC_CHILD"
const panicSentinel = "function-panic-sentinel"

// TestFunctionPanicWithoutHandlerPropagates verifies that when a
// `Function` stage panics and no panic handler is installed, the panic
// propagates (crashing the process) rather than being silently
// swallowed and reported as a successful run. Because a propagating
// panic would crash the test binary itself, the actual pipeline is run
// in a re-exec'd subprocess and this test asserts on its outcome.
func TestFunctionPanicWithoutHandlerPropagates(t *testing.T) {
	if os.Getenv(panicChildEnv) == "1" {
		runPanicChild()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestFunctionPanicWithoutHandlerPropagates$", "-test.v") //nolint:gosec // re-exec of this test binary with constant arguments.
	cmd.Env = append(os.Environ(), panicChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	output := string(out)

	require.Errorf(t, err, "expected subprocess to crash from a propagated panic, but it exited 0\noutput:\n%s", output)
	assert.NotContains(t, output, "SURVIVED", "panic was swallowed: Run returned instead of propagating")
	assert.Contains(t, output, "panic:")
	assert.Contains(t, output, panicSentinel)
}

// runPanicChild runs a pipeline whose only stage is a `Function` that panics,
// with no panic handler configured. The panic, being unhandled, should crash
// the process before the sleep elapses; if it is swallowed (the regression),
// Run returns and we print SURVIVED so the parent can detect the failure.
func runPanicChild() {
	p := pipe.New(pipe.WithStdout(io.Discard))
	p.Add(pipe.Function("boom", func(_ context.Context, _ pipe.Env, _ io.Reader, _ io.Writer) error {
		panic(panicSentinel)
	}))

	err := p.Run(context.Background())

	// reaching this point at all indicates the panic was swallowed.
	time.Sleep(2 * time.Second)
	os.Stdout.WriteString("SURVIVED: Run returned err=")
	if err != nil {
		os.Stdout.WriteString(err.Error())
	} else {
		os.Stdout.WriteString("<nil>")
	}
	os.Stdout.WriteString("\n")
	os.Exit(0)
}
