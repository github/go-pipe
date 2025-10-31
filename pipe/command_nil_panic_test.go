//go:build !windows
// +build !windows

package pipe

import (
	"context"
	"os/exec"
	"testing"
)

func TestKillWithNilProcess(t *testing.T) {
	cmd := exec.Command("sleep", "100")
	stage := &commandStage{
		name: "test-command",
		cmd:  cmd,
		done: make(chan struct{}),
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIC OCCURRED (bug not fixed): %v", r)
		}
	}()

	stage.Kill(context.Canceled)

	t.Log("SUCCESS: Kill() handled nil Process gracefully without panicking")
}

func TestKillWithFailedStart(t *testing.T) {
	ctx := context.Background()

	stage := Command("/this/path/does/not/exist/invalid_command_12345")

	_, err := stage.Start(ctx, Env{}, nil)
	if err == nil {
		t.Fatal("Expected start to fail, but it succeeded")
	}

	// At this point, if someone calls Kill (perhaps from a memory monitor
	// or other external component), it could panic if Process is nil

	// Note: In the current implementation, Kill won't be called from the
	// context goroutine because Start failed. But external callers like
	// MemoryLimit could potentially call Kill on a failed stage.
}
