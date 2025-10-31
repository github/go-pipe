//go:build windows
// +build windows

package pipe

// runInOwnProcessGroup is not supported on Windows.
func (s *commandStage) runInOwnProcessGroup() {}

// kill is called to kill the process if the context expires. `err` is
// the corresponding value of `Context.Err()`.
func (s *commandStage) Kill(err error) {
	// Check if the process was started successfully before attempting to kill
	if s.cmd.Process == nil {
		return
	}

	select {
	case <-s.done:
		// Process has ended; no need to kill it again.
		return
	default:
	}

	// Record the `ctx.Err()`, which will be used as the error result
	// for this stage.
	s.ctxErr.Store(err)

	s.cmd.Process.Kill()
}
