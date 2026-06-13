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

func (s InputStream) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
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

func (s OutputStream) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}
