package pipe

import (
	"bytes"
	"os"
	"runtime"
	"testing"
)

// TestIOCopierPoolBufferUsed verifies that ioCopier uses the sync.Pool
// buffer rather than allocating a fresh one. On Go 1.26+, *os.File
// implements WriterTo, which causes io.CopyBuffer to bypass the
// provided pool buffer entirely. Instead, File.WriteTo →
// genericWriteTo → io.Copy allocates a fresh 32KB buffer on every call.
func TestIOCopierPoolBufferUsed(t *testing.T) {
	const payload = "hello from pipe\n"

	// Pre-warm the pool so Get doesn't allocate.
	copyBufPool.Put(copyBufPool.New())

	// Warm up: run once to stabilize lazy init.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		pw.Write([]byte(payload))
		pw.Close()
	}()
	var warmBuf bytes.Buffer
	c := newIOCopier(nopWriteCloser{&warmBuf})
	c.Start(nil, Env{}, pr)
	c.Wait()

	// Now measure: run the copy and check how many bytes were allocated.
	// If the pool buffer is bypassed, a fresh 32KB buffer is allocated.
	pr, pw, err = os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		pw.Write([]byte(payload))
		pw.Close()
	}()
	var buf bytes.Buffer
	c = newIOCopier(nopWriteCloser{&buf})

	// GC clears sync.Pool, so re-warm it afterward to isolate the
	// measurement from pool repopulation overhead.
	runtime.GC()
	copyBufPool.Put(copyBufPool.New())

	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	c.Start(nil, Env{}, pr)
	c.Wait()

	runtime.GC()
	runtime.ReadMemStats(&m2)

	if buf.String() != payload {
		t.Fatalf("unexpected output: %q", buf.String())
	}

	allocBytes := m2.TotalAlloc - m1.TotalAlloc
	// A bypassed pool buffer causes ~32KB of allocation.
	// With the pool buffer working, we expect well under 32KB.
	const maxBytes = 16 * 1024
	if allocBytes > maxBytes {
		t.Errorf("ioCopier allocated %d bytes during copy (max %d); "+
			"pool buffer may be bypassed by *os.File WriterTo",
			allocBytes, maxBytes)
	}
}
