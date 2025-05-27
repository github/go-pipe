package pipe

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/containerd/cgroups"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func NewCgroupsIsolationPolicy(cpu uint64, memory int64, name string, path string) (IsolationPolicy, error) {
	return &CgroupsIsolation{
		cpu:    cpu,
		memory: memory,
		name:   name,
		path:   path,
	}, nil
}

type CgroupsIsolation struct {
	cpu    uint64
	memory int64
	name   string
	path   string

	cgroupControl cgroups.Cgroup
}

func (c *CgroupsIsolation) Setup(ctx context.Context, pid ProcessId) error {
	cgroupName := fmt.Sprintf("%s-%d-%d", c.name, time.Now().UnixNano(), rand.Intn(10000))
	control, err := cgroups.New(
		cgroups.V1,
		cgroups.StaticPath(c.path+cgroupName),
		&specs.LinuxResources{
			CPU: &specs.LinuxCPU{
				Shares: &c.cpu,
			},
			Memory: &specs.LinuxMemory{
				Limit: &c.memory,
			},
		},
	)
	if err != nil {
		return err
	}

	if err := control.Add(cgroups.Process{Pid: int(pid)}); err != nil {
		control.Delete()
		return fmt.Errorf("failed to add process %d to cgroup %s: %w", pid, cgroupName, err)
	}

	c.cgroupControl = control

	return nil
}

func (c *CgroupsIsolation) Teardown(ctx context.Context) error {
	if c.cgroupControl == nil {
		return fmt.Errorf("cgroup control is not initialized")
	}

	return c.cgroupControl.Delete()
}
