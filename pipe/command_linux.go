//go:build linux

package pipe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
)

// On linux, we can limit or observe memory usage in command stages.
var _ LimitableStage = (*commandStage)(nil)

var (
	errProcessInfoMissing = errors.New("cmd.Process is nil")
)

func (s *commandStage) GetRSS(ctx context.Context) (uint64, error) {
	if s.cmd.Process == nil {
		return 0, errProcessInfoMissing
	}

	f, err := os.Open(fmt.Sprintf("/proc/%d/status", s.cmd.Process.Pid))
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if rss, ok := parseRSS(line); ok {
			return rss, nil
		}
	}
	if scan.Err() != nil {
		return 0, scan.Err()
	}
	return 0, errors.New("RssAnon was not found in /proc/[pid]/status")
}
