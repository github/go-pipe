// This file is mostly copied from the Go standard library, which is:
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package pipe

import "io"

// newReaderNopCloser returns a ReadCloser with a no-op Close method, wrapping
// the provided io.Reader `r`. The wrapper deliberately hides `r`'s concrete
// type; use [UnwrapReader] to recover the underlying reader before use in
// situations where eg. use of io.WriterTo is important for performance.
func newReaderNopCloser(r io.Reader) io.ReadCloser {
	return readerNopCloser{r}
}

// readerNopCloser is a ReadCloser that wraps a provided `io.Reader`,
// but whose `Close()` method does nothing. It should be unwrapped (via
// [UnwrapReader]) before use.
type readerNopCloser struct {
	io.Reader
}

func (readerNopCloser) Close() error {
	return nil
}

// writerNopCloser is the stdout counterpart of [readerNopCloser]
type writerNopCloser struct {
	io.Writer
}

func (w writerNopCloser) Close() error {
	return nil
}

// UnwrapReader returns the underlying [io.Reader] that go-pipe wrapped around
// the `stdin` it passes to a [Stage]'s `Start` method. [Stage] implementations
// should call this before reading from `stdin` so it sees the caller's
// concrete reader type, because that allows for use of fast-path interfaces
// (e.g. `io.WriterTo`) and identity (e.g. `*os.File`, for direct fd passing).
//
// If `r` is not a go-pipe wrapper (including nil), it is returned unchanged.
func UnwrapReader(r io.Reader) io.Reader {
	if w, ok := r.(readerNopCloser); ok {
		return w.Reader
	}
	return r
}

// UnwrapWriter returns the underlying [io.Writer] that go-pipe wrapped around
// the `stdout` it passes to a [Stage]'s `Start` method. [Stage]
// implementations should call this before writing to `stdout` (see above).
//
// If `w` is not a go-pipe wrapper (including nil), it is returned unchanged.
func UnwrapWriter(w io.Writer) io.Writer {
	if n, ok := w.(writerNopCloser); ok {
		return n.Writer
	}
	return w
}

// unwrapNopCloser unwraps the object if it is some kind of nop
// closer, and returns the underlying object. This function is used
// only for testing.
func unwrapNopCloser(obj any) (any, bool) {
	switch obj := obj.(type) {
	case readerNopCloser:
		return obj.Reader, true
	case writerNopCloser:
		return obj.Writer, true
	default:
		return nil, false
	}
}
