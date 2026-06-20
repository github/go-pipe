//go:build !windows
// +build !windows

package pipe

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKillWithNilProcess(t *testing.T) {
	cmd := exec.Command("sleep", "100")
	stage := &commandStage{
		name: "test-command",
		cmd:  cmd,
		done: make(chan struct{}),
	}

	assert.NotPanics(t, func() {
		stage.Kill(context.Canceled)
	})
}
