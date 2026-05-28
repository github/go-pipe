package pipe

import (
	"io"
	"sync"
)

// copyBufPool reuses 32KB buffers across `io.CopyBuffer` calls,
// avoiding a fresh heap allocation per copy. This matters in
// high-throughput pipelines where many command stages run
// concurrently and stdout is not an `*os.File` that can be passed
// directly through `exec.Cmd`.
var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// readerOnly wraps an `io.Reader`, hiding any other interfaces (such
// as `io.WriterTo`) so that `io.CopyBuffer` is forced to use the
// provided buffer. Without this, `*os.File`'s `WriterTo` (added in
// Go 1.26) causes `CopyBuffer` to call `File.WriteTo`, which can
// fall back to `io.Copy` with a fresh allocation, bypassing the pool
// entirely.
type readerOnly struct{ io.Reader }

// pooledCopy copies from `src` to `dst`. If `dst` implements
// `io.ReaderFrom` (e.g. `*net.TCPConn`, `*os.File`), it delegates to
// `ReadFrom` so platform fast paths like splice can be used.
// Otherwise it falls back to `io.CopyBuffer` with a pooled 32KB
// buffer.
func pooledCopy(dst io.Writer, src io.Reader) (int64, error) {
	if rf, ok := dst.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	bp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bp)
	return io.CopyBuffer(dst, readerOnly{src}, *bp)
}
