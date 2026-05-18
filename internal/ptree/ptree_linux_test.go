package ptree_test

import (
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/github/go-pipe/internal/ptree"
	"github.com/stretchr/testify/require"
)

// TODO: use mocks, not the actual process tree
func TestGetProcessTreeRSS(t *testing.T) {
	mem := map[int]uint64{}
	treeMem := map[int]uint64{}

	pids := allPids(t)
	for _, pid := range pids {
		rss, err := ptree.GetProcessRSSAnon(pid)
		if err == nil {
			mem[pid] = rss
		}
		rss, err = ptree.GetProcessTreeRSSAnon(pid)
		if err == nil {
			treeMem[pid] = rss
		}
	}

	var less, equal, greater int
	for pid, treeRss := range treeMem {
		if rss, ok := mem[pid]; ok {
			switch {
			case treeRss > rss:
				greater++
			case treeRss == rss:
				equal++
			default:
				less++
			}
		}
	}
	require.Positive(t, greater) // detect process trees using more mem than their root process
	require.Positive(t, equal)   // process trees with no children have the same mem usage as root

	// We should *extremely* rarely find a tree using less memory than its root
	// process. However, this is a little racy since any process in the tree can
	// return memory to the OS between our measurements. Allow this to happen
	// once at most just to reduce the risk of flakiness.
	require.LessOrEqual(t, less, 1)
}

func allPids(t *testing.T) []int {
	procfs := os.DirFS("/proc")
	matches, err := fs.Glob(procfs, "[0-9]*[0-9]/task")
	require.Nil(t, err)

	var pids = make([]int, 0, len(matches))
	for _, m := range matches {
		ns := strings.SplitN(m, "/", 2)
		pid, err := strconv.Atoi(ns[0])
		require.Nil(t, err)
		pids = append(pids, pid)
	}
	require.NotEmpty(t, pids)
	return pids
}
