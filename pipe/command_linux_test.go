//go:build linux

package pipe

import (
	"context"
	"testing"

	"github.com/github/go-pipe/pipe"
	"github.com/stretchr/testify/assert"
)

func TestRunInCgroup(t *testing.T) {
	t.Parallel()

	ip, err := NewCgroupsIsolationPolicy(1000, 1000000000, "test", "/tmp/")
	assert.NoError(t, err)

	ctx := context.Background()

	dir := t.TempDir()
	p := pipe.New(pipe.WithDir(dir))

	p.Add(CommandWithIsolationPolicy("", ip))
	assert.NoError(t, p.Start(ctx))
}
