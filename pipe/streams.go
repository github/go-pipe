package pipe

import "io"

type InputStream struct {
	reader io.Reader
	closer io.Closer
}

// The stage may read from r but must not close it.
func Input(r io.Reader) InputStream {
	return InputStream{reader: r}
}

// The stage is responsible for closing r.
func ClosingInput(r io.ReadCloser) InputStream {
	return InputStream{reader: r, closer: r}
}

func (s InputStream) Reader() io.Reader {
	return s.reader
}

// Closer returns the stream closer, or nil if the stream is non-closing.
func (s InputStream) Closer() io.Closer {
	return s.closer
}

func (s InputStream) Close() {
	if s.closer != nil {
		_ = s.closer.Close()
	}
}

type OutputStream struct {
	writer io.Writer
	closer io.Closer
}

// The stage may write to w but must not close it.
func Output(w io.Writer) OutputStream {
	return OutputStream{writer: w}
}

// The stage is responsible for closing w.
func ClosingOutput(w io.WriteCloser) OutputStream {
	return OutputStream{writer: w, closer: w}
}

func (s OutputStream) Writer() io.Writer {
	return s.writer
}

// Closer returns the stream closer, or nil if the stream is non-closing.
func (s OutputStream) Closer() io.Closer {
	return s.closer
}

func (s OutputStream) Close() {
	if s.closer != nil {
		_ = s.closer.Close()
	}
}
