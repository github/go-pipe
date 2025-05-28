package pipe

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/containerd/cgroups"
	"github.com/containerd/cgroups/v3/cgroup2"
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

func (c *CgroupsIsolation) Setup(ctx context.Context, pid uint64) error {
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

type CgroupsV2Isolation struct {
	cpu_quota  *int64
	cpu_period *uint64
	cpu_weight *uint64
	memory     *int64
	name       string
	path       string

	manager *cgroup2.Manager
}

func NewCgroupsV2IsolationPolicy(
	cpu_quota int64, cpu_period uint64, cpu_weight uint64,
	memory int64, name string, path string,
) (IsolationPolicy, error) {

	if cpu_quota < 0 || cpu_period == 0 || memory < 0 {
		return nil, fmt.Errorf("invalid cgroup parameters: cpu_quota=%d, cpu_period=%d, memory=%d", cpu_quota, cpu_period, memory)
	}

	return &CgroupsV2Isolation{
		cpu_quota:  &cpu_quota,
		cpu_period: &cpu_period,
		cpu_weight: &cpu_weight,
		memory:     &memory,
		name:       name,
		path:       path,
	}, nil
}

func (c *CgroupsV2Isolation) Setup(ctx context.Context, pid uint64) error {
	cgroupName := fmt.Sprintf("%s-%d-%d", c.name, time.Now().UnixNano(), rand.Intn(10000))
	cgroupPath := c.path + cgroupName

	resources := &cgroup2.Resources{
		CPU: &cgroup2.CPU{
			Max:    cgroup2.NewCPUMax(c.cpu_quota, c.cpu_period),
			Weight: c.cpu_weight,
		},
		Memory: &cgroup2.Memory{
			Max: c.memory,
		},
	}

	manager, err := cgroup2.NewManager(
		"/sys/fs/cgroup",
		cgroupPath,
		resources,
	)
	if err != nil {
		return fmt.Errorf("failed to create cgroup manager: %w", err)
	}

	// Add the process to the cgroup
	if err := manager.AddProc(pid); err != nil {
		c.manager.Delete()
		return fmt.Errorf("failed to add process %d to cgroup %s: %w", pid, cgroupName, err)
	}

	c.manager = manager
	return nil
}

func (c *CgroupsV2Isolation) Teardown(ctx context.Context) error {
	if c.manager == nil {
		return fmt.Errorf("cgroup manager is not initialized")
	}
	return c.manager.Delete()
}
