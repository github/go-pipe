// This file is mostly copied from the Go standard library, which is:
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package pipe

import "io"

// newNopCloser returns a ReadCloser with a no-op Close method wrapping
// the provided io.Reader r.
// If r implements io.WriterTo, the returned io.ReadCloser will implement io.WriterTo
// by forwarding calls to r.
func newNopCloser(r io.Reader) io.ReadCloser {
	if _, ok := r.(io.WriterTo); ok {
		return nopCloserWriterTo{r}
	}
	return nopCloser{r}
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

type nopCloserWriterTo struct {
	io.Reader
}

func (nopCloserWriterTo) Close() error { return nil }

func (c nopCloserWriterTo) WriteTo(w io.Writer) (n int64, err error) {
	return c.Reader.(io.WriterTo).WriteTo(w)
}
