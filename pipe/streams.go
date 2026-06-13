package pipe

import (
	"io"
	"sync"
)

// InputStream represents `stdin` for a stage, which might or might
// not need to be closed when the stage is done with it. It usually
// holds an `io.Reader`, which can be retrieved using `Reader()`. Its
// `Close()` method closes the reader if necessary (i.e., if the
// `InputStream` was constructed using `ClosingInput()`. The
// `Close()` method is idempotent.
//
// A nil `*InputStream` is a valid value. Its `Reader()` method
// returns `nil` and `Close()` does nothing successfully.
type InputStream struct {
	reader io.Reader

	// once is used to ensure that `Close()` is only called once.
	once sync.Once

	// closer is set to `nil` after the first call to `Close()`.
	closer io.Closer

	// closeErr is set to the error returned by the first call to
	// `Close()`, and returned from that and any subsequent calls to
	// `Close()`.
	closeErr error
}

// The stage may read from r but must not close it.
func Input(r io.Reader) *InputStream {
	return &InputStream{reader: r}
}

// The stage is responsible for closing r.
func ClosingInput(r io.ReadCloser) *InputStream {
	return &InputStream{reader: r, closer: r}
}

func (s *InputStream) Reader() io.Reader {
	if s == nil {
		return nil
	}
	return s.reader
}

// Close closes the underlying reader if necessary. If `s` was
// constructed using `ClosingInput()`, then close the `io.ReadCloser`
// that was passed to that function. If `s` is `nil` or was
// constructed using `Input()`, then do nothing successfully.
func (s *InputStream) Close() error {
	if s == nil {
		return nil
	}

	s.once.Do(func() {
		if s.closer != nil {
			s.closeErr = s.closer.Close()
			s.closer = nil
		}
	})

	return s.closeErr
}

// OutputStream represents `stdout` for a stage, which might or might
// not need to be closed when the stage is done with it. It usually
// holds an `io.Writer`, which can be retrieved using `Writer()`. Its
// `Close()` method closes the writer if necessary (i.e., if the
// `OutputStream` was constructed using `ClosingOutput()`. The
// `Close()` method is idempotent.
//
// A nil `*OutputStream` is a valid value. Its `Writer()` method
// returns `nil` and `Close()` does nothing successfully.
type OutputStream struct {
	writer io.Writer

	// once is used to ensure that `Close()` is only called once.
	once sync.Once

	// closer is set to `nil` after the first call to `Close()`.
	closer io.Closer

	// closeErr is set to the error returned by the first call to
	// `Close()`, and returned from that and any subsequent calls to
	// `Close()`.
	closeErr error
}

// The stage may write to w but must not close it.
func Output(w io.Writer) *OutputStream {
	return &OutputStream{writer: w}
}

// The stage is responsible for closing w.
func ClosingOutput(w io.WriteCloser) *OutputStream {
	return &OutputStream{writer: w, closer: w}
}

func (s *OutputStream) Writer() io.Writer {
	if s == nil {
		return nil
	}
	return s.writer
}

// Close closes the underlying writer if necessary. If `s` was
// constructed using `ClosingOutput()`, then close the
// `io.WriteCloser` that was passed to that function. If `s` is `nil`
// or was constructed using `Output()`, then do nothing successfully.
func (s *OutputStream) Close() error {
	if s == nil {
		return nil
	}

	s.once.Do(func() {
		if s.closer != nil {
			s.closeErr = s.closer.Close()
			s.closer = nil
		}
	})

	return s.closeErr
}
