# go-pipe [![GoDoc](https://pkg.go.dev/badge/github.com/github/go-pipe/v2)](https://pkg.go.dev/github.com/github/go-pipe/v2)
A package used to easily build command pipelines in your Go applications

# Important
We have not thoroughly tested this package on OSs other than Linux, especially Windows. At this time, using this package on Windows based systems is considered experimental and will be supported only on a best effort basis.

# Migrating to v2

It's normal for pipelines to stop before all input has been consumed[^1]. If an earlier stage continues writing after that happens, the write side of the pipe can fail with `EPIPE`, `SIGPIPE`, or `io.ErrClosedPipe`.

In go-pipe v1 it was possible to get away without handling this case, because a command stage's stdin was connected in a way that often (but not necessarily!) drained the write side and hid the error from the previous stage feeding it. That was an implementation detail, not a guarantee. In go-pipe v2, producer stages are more likely to be connected directly to a command's stdin, and thus see the error themselves.

Fortunately, this is easily handled by wrapping the stage with `pipe.IgnoreError(stage, IsPipeError)`. If the producer only writes output and is otherwise stateless, that's the only thing needed.

If the producer also updates state, metrics, cursors, or has other side effects, in a way that depends on how much of the output was produced, then in addition to using `pipe.IgnoreError`, you must also ensure producer-owned state is brought to a consistent point before returning the error.

For example, if a stateful producer function must process its entire input for correctness regardless of whether it was read by the consumer, it should use a pattern like:

```go
var writeErr error
for _, item := range items {
	updateState(item)
	if writeErr == nil {
		_, writeErr = fmt.Fprintln(stdout, item)
	}
}
return writeErr
```

# Links

* [Docs](https://pkg.go.dev/github.com/github/go-pipe/v2)

[^1]: In `cat foo | head | grep -q`, for example, either `head` or `grep` could exit before its input is fully consumed.
