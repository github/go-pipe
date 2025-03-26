package pipe_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/github/go-pipe/pipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func LogEventHandler(logger *log.Logger) func(*pipe.Event) {
	return func(e *pipe.Event) {
		ctx := ""
		for k, v := range e.Context {
			ctx = fmt.Sprintf("%s,%s=%v", ctx, k, v)
		}

		logger.Printf("Command %s failed with message %s and error %s. Context: %s", e.Command, e.Msg, e.Command, ctx)
	}
}

func TestMemoryObserverSimple(t *testing.T) {
	t.Parallel()
	rss := testMemoryObserver(t, 400, pipe.Command("less"))
	require.Greater(t, rss, 400_000_000)
}

func TestMemoryObserverTreeMem(t *testing.T) {
	t.Parallel()

	// create a process tree like:
	//   /tmp/go-build3166037414/b001/pipe.test -test.paniconexit0 -test.timeout=10m0s -test.count=1 -test.run=Tree -test.v=true
	//    \_ head -c 3G
	//    \_ sh -c less; :
	//        \_ less
	// so that MemoryObserver is watching the parent `sh` proc and doesn't detect less's mem usage.
	// less should buffer whatever we send it via stdin, giving us some level of control over its
	// memory usage.
	rss := testMemoryObserver(t, 400, pipe.Command("sh", "-c", "less; :"))
	require.Greater(t, rss, 400_000_000)
}

func testMemoryObserver(t *testing.T, mbs int, stage pipe.Stage) int {
	ctx := context.Background()

	stdinReader, stdinWriter := io.Pipe()

	devNull, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	require.NoError(t, err)

	buf := &bytes.Buffer{}
	logger := log.New(buf, "testMemoryObserver", log.Ldate|log.Ltime)

	p := pipe.New(pipe.WithDir("/"), pipe.WithStdin(stdinReader), pipe.WithStdout(devNull))
	p.Add(pipe.MemoryObserver(stage, LogEventHandler(logger)))
	require.NoError(t, p.Start(ctx))

	// Write some nonsense data to less, but don't close stdin until we want it
	// to exit.
	var bytes [1_000_000]byte
	for i := 0; i < mbs; i++ {
		n, err := stdinWriter.Write(bytes[:])
		require.NoError(t, err)
		require.Equal(t, len(bytes), n)
	}

	// MemoryObserver polls every one second, so this should make sure we catch
	// at least one.
	time.Sleep(2 * time.Second)

	// Close stdin and wait for the pipeline to exit.
	require.NoError(t, stdinWriter.Close())
	require.NoError(t, p.Wait())

	return maxBytes(buf.String())
}

func maxBytes(s string) int {
	idx := strings.Index(s, "max_rss_bytes=")
	if idx < 0 {
		return idx
	}
	var maxRSS int
	n, err := fmt.Sscanf(s[idx:], "max_rss_bytes=%d", &maxRSS)
	if n != 1 || err != nil {
		return -1
	}
	return maxRSS
}

func TestMemoryLimitSimple(t *testing.T) {
	t.Parallel()
	msg, err := testMemoryLimit(t, 400, 10_000_000, pipe.Command("less"))
	assert.Contains(t, msg, "exceeded allowed memory")
	assert.Contains(t, msg, "limit=10000000")
	require.ErrorContains(t, err, "memory limit exceeded")
}

func TestMemoryLimitTreeMem(t *testing.T) {
	t.Parallel()
	msg, err := testMemoryLimit(t, 400, 10_000_000, pipe.Command("sh", "-c", "less; :"))
	assert.Contains(t, msg, "exceeded allowed memory")
	assert.Contains(t, msg, "limit=10000000")
	require.ErrorContains(t, err, "memory limit exceeded")
}

func testMemoryLimit(t *testing.T, mbs int, limit uint64, stage pipe.Stage) (string, error) {
	ctx := context.Background()

	devNull, err := os.OpenFile("/dev/null", os.O_WRONLY, 0)
	require.NoError(t, err)

	buf := &bytes.Buffer{}
	logger := log.New(buf, "testMemoryObserver", log.Ldate|log.Ltime)

	p := pipe.New(pipe.WithDir("/"), pipe.WithStdoutCloser(devNull))
	p.Add(
		pipe.Function(
			"write-to-less",
			func(ctx context.Context, _ pipe.Env, _ io.Reader, stdout io.Writer) error {
				// Write some nonsense data to less.
				var bytes [1_000_000]byte
				for i := 0; i < mbs; i++ {
					_, err := stdout.Write(bytes[:])
					if err != nil {
						require.ErrorIs(t, err, syscall.EPIPE)
					}
				}

				return nil
			},
		),
		pipe.MemoryLimit(stage, limit, LogEventHandler(logger)),
	)
	require.NoError(t, p.Start(ctx))

	err = p.Wait()

	return buf.String(), err
}
