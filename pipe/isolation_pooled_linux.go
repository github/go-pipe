package pipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/containerd/cgroups/v3/cgroup2"
)

var mountpoint = "/sys/fs/cgroup"

// CgroupCache manages a cache of reusable cgroups
type CgroupCache struct {
	mu       sync.RWMutex
	cgroups  map[string]*cgroup2.Manager
	basePath string
}

// NewCgroupCache creates a new cgroup cache using the specified base path as the root for all cgroups
func NewCgroupCache(basePath string) *CgroupCache {
	return &CgroupCache{
		cgroups:  make(map[string]*cgroup2.Manager),
		basePath: basePath,
	}
}

// GetOrCreateCgroup returns an existing cgroup or creates a new one
func (cc *CgroupCache) GetOrCreateCgroup(name string, resources *cgroup2.Resources) (*cgroup2.Manager, error) {
	cgroupPath := fmt.Sprintf("%s/%s", cc.basePath, name)

	cc.mu.RLock()
	if manager, exists := cc.cgroups[cgroupPath]; exists {
		cc.mu.RUnlock()
		return manager, nil
	}
	cc.mu.RUnlock()

	cc.mu.Lock()
	defer cc.mu.Unlock()

	if manager, exists := cc.cgroups[cgroupPath]; exists {
		return manager, nil
	}

	manager, err := cgroup2.NewManager(
		mountpoint,
		cgroupPath,
		resources,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create cgroup %s: %w", name, err)
	}

	cc.cgroups[cgroupPath] = manager
	return manager, nil
}

// RemoveCgroup removes a cgroup from cache and deletes it
func (cc *CgroupCache) RemoveCgroup(name string) error {
	cgroupPath := fmt.Sprintf("%s/%s", cc.basePath, name)

	cc.mu.Lock()
	defer cc.mu.Unlock()

	if manager, exists := cc.cgroups[cgroupPath]; exists {
		delete(cc.cgroups, cgroupPath)
		return manager.Delete()
	}
	return nil
}

// CachedCgroupsV2Isolation implements IsolationPolicy with cgroup caching
type CachedCgroupsV2Isolation struct {
	cpu_quota  *int64
	cpu_period *uint64
	cpu_weight *uint64
	memory     *int64
	name       string
	cache      *CgroupCache
}

func NewCachedCgroupsV2IsolationPolicy(
	cpu_quota int64, cpu_period uint64, cpu_weight uint64,
	memory int64, name string, cacheBasePath string,
) (IsolationPolicy, error) {

	if cpu_quota < 0 || cpu_period == 0 || memory < 0 {
		return nil, fmt.Errorf("invalid cgroup parameters: cpu_quota=%d, cpu_period=%d, memory=%d", cpu_quota, cpu_period, memory)
	}

	return &CachedCgroupsV2Isolation{
		cpu_quota:  &cpu_quota,
		cpu_period: &cpu_period,
		cpu_weight: &cpu_weight,
		memory:     &memory,
		name:       name,
		cache:      NewCgroupCache(cacheBasePath),
	}, nil
}

func (c *CachedCgroupsV2Isolation) Setup(ctx context.Context, pid uint64) error {

	resources := &cgroup2.Resources{
		CPU: &cgroup2.CPU{
			Max:    cgroup2.NewCPUMax(c.cpu_quota, c.cpu_period),
			Weight: c.cpu_weight,
		},
		Memory: &cgroup2.Memory{
			Max: c.memory,
		},
	}

	manager, err := c.cache.GetOrCreateCgroup(c.name, resources)
	if err != nil {
		return fmt.Errorf("failed to get or create cached cgroup: %w", err)
	}

	manager.Delete()
	if err := manager.AddProc(pid); err != nil {
		return fmt.Errorf("failed to add process %d to cached cgroup %s: %w", pid, c.name, err)
	}

	return nil
}

func (c *CachedCgroupsV2Isolation) Teardown(ctx context.Context) error {
	// In the cached version, we don't delete the cgroup, just remove the process
	// There is no way to remove a process from a cgroup in cgroup2, it will be automatically removed
	// when the process exits.

	// One option is to move the process to the root cgroup, but we don't need to do that here.

	return nil
}

func (cc *CgroupCache) DiscoverExistingCgroups() ([]string, error) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	basePath := filepath.Join(mountpoint, cc.basePath)

	var cgroups []string

	// Check if base path exists
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return cgroups, nil // No cgroups under this base path
	}

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && path != basePath {
			// Get the relative name from the base path
			relativeName := strings.TrimPrefix(path, basePath+"/")
			cgroups = append(cgroups, relativeName)
		}

		return nil
	})

	return cgroups, err
}

// LoadExistingCgroups loads existing cgroups into the cache
func (cc *CgroupCache) LoadExistingCgroups() error {
	existingCgroups, err := cc.DiscoverExistingCgroups()
	if err != nil {
		return err
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	for _, cgroupName := range existingCgroups {
		cgroupPath := fmt.Sprintf("%s/%s", cc.basePath, cgroupName)
		manager, err := cgroup2.Load(cgroupPath, cgroup2.WithMountpoint(mountpoint))
		if err != nil {
			// Skip cgroups we can't load
			continue
		}

		cc.cgroups[cgroupName] = manager
	}

	return nil
}
