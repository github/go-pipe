package pipe

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

// ioCopier is a stage that copies its stdin to a specified
// `io.Writer`. It generates no stdout itself.
type ioCopier struct {
	w    io.WriteCloser
	done chan struct{}
	err  error
}

// copyBufPool reuses 32KB buffers across io.CopyBuffer calls, avoiding
// a fresh heap allocation per copy. This matters in high-throughput
// pipelines where many ioCopier stages run concurrently.
var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// readerOnly wraps an io.Reader, hiding any other interfaces (such as
// WriterTo) so that io.CopyBuffer is forced to use the provided buffer.
type readerOnly struct{ io.Reader }

func newIOCopier(w io.WriteCloser) *ioCopier {
	return &ioCopier{
		w:    w,
		done: make(chan struct{}),
	}
}

func (s *ioCopier) Name() string {
	return "ioCopier"
}

// This method always returns `nil, nil`.
func (s *ioCopier) Start(_ context.Context, _ Env, r io.ReadCloser) (io.ReadCloser, error) {
	go func() {
		bp := copyBufPool.Get().(*[]byte)
		// Strip all interfaces except Read from r so that
		// io.CopyBuffer always uses the provided pool buffer.
		// Without this, *os.File's WriterTo (added in Go 1.26)
		// causes CopyBuffer to call File.WriteTo, which falls
		// back to io.Copy with a fresh allocation, bypassing
		// the pool entirely.
		_, err := io.CopyBuffer(s.w, readerOnly{r}, *bp)
		copyBufPool.Put(bp)
		// We don't consider `ErrClosed` an error (FIXME: is this
		// correct?):
		if err != nil && !errors.Is(err, os.ErrClosed) {
			s.err = err
		}
		if err := r.Close(); err != nil && s.err == nil {
			s.err = err
		}
		if err := s.w.Close(); err != nil && s.err == nil {
			s.err = err
		}
		close(s.done)
	}()

	// FIXME: if `s.w.Write()` is blocking (e.g., because there is a
	// downstream process that is not reading from the other side),
	// there's no way to terminate the copy when the context expires.
	// This is not too bad, because the `io.Copy()` call will exit by
	// itself when its input is closed.
	//
	// We could, however, be smarter about exiting more quickly if the
	// context expires but `s.w.Write()` is not blocking.

	return nil, nil
}

func (s *ioCopier) Wait() error {
	<-s.done
	return s.err
}
