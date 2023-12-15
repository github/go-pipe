// This file is mostly copied from the Go standard library, which is:
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package pipe

import "io"

// newReaderNopCloser returns a ReadCloser with a no-op Close method,
// wrapping the provided io.Reader `r`. If `r` implements
// `io.WriterTo`, the returned `io.ReadCloser` will also implement
// `io.WriterTo` by forwarding calls to `r`.
func newReaderNopCloser(r io.Reader) io.ReadCloser {
	if _, ok := r.(io.WriterTo); ok {
		return readerWriterToNopCloser{r}
	}
	return readerNopCloser{r}
}

// readerNopCloser is a ReadCloser that wraps a provided `io.Reader`,
// but whose `Close()` method does nothing. We don't need to check
// whether the wrapped reader also implements `io.WriterTo`, since
// it's always unwrapped before use.
type readerNopCloser struct {
	io.Reader
}

func (readerNopCloser) Close() error {
	return nil
}

// readerWriterToNopCloser is like `readerNopCloser` except that it
// also implements `io.WriterTo` by delegating `WriteTo()` to the
// wrapped `io.Reader` (which must also implement `io.WriterTo`).
type readerWriterToNopCloser struct {
	io.Reader
}

func (readerWriterToNopCloser) Close() error { return nil }

func (r readerWriterToNopCloser) WriteTo(w io.Writer) (n int64, err error) {
	return r.Reader.(io.WriterTo).WriteTo(w)
}

// writerNopCloser is a WriteCloser that wraps a provided `io.Writer`,
// but whose `Close()` method does nothing.
type writerNopCloser struct {
	io.Writer
}

func (w writerNopCloser) Close() error {
	return nil
}
