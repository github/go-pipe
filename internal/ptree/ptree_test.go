package ptree_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/github/go-pipe/internal/ptree"
	"github.com/stretchr/testify/assert"
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

func TestWalkChildren(t *testing.T) {
	const depth = 5

	arg := "echo ready; read -r x;"
	for i := 0; i < depth; i++ {
		arg = fmt.Sprintf("sh -c %q", arg)
	}

	cmd := exec.Command("sh", "-c", arg)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	// Wait for the process to start by reading the expected output from the
	// innermost child.
	var ready [5]byte
	_, err = stdout.Read(ready[:])
	require.NoError(t, err, "process didn't appear to start successfully")
	require.Equal(t, "ready", string(ready[:]))

	var numChildren int
	ptree.WalkChildren(cmd.Process.Pid, func(pid int) {
		numChildren++
	})
	assert.Equal(t, depth, numChildren)

	// Gracefully exit the process tree.
	_, err = stdin.Write([]byte("\n"))
	require.NoError(t, err)
	require.NoError(t, stdin.Close())
	require.NoError(t, cmd.Wait())
}

func TestParseRss(t *testing.T) {
	const kb = 1024

	okExamples := []struct {
		input  string
		result uint64
	}{
		{
			input:  "RssAnon:\t   15032 kB",
			result: 15032 * kb,
		},
		{
			input:  "RssAnon:\t   15032 kB\n",
			result: 15032 * kb,
		},
		{
			input:  "RssAnon:\t99915032 kB",
			result: 99915032 * kb,
		},
		{
			input:  "RssAnon:\t       1 kB",
			result: kb,
		},
	}

	for _, example := range okExamples {
		rss, ok := ptree.ParseRSSAnon(example.input)
		if assert.Truef(t, ok, "should be able to parse %q", example.input) {
			assert.Equalf(t, example.result, rss, "value of %q", example.input)
		}
	}

	badExamples := []string{
		"",
		"\n",
		"RssAnon:\t 123",
		"RssAnonn:\t 123 kB",
		"RssAno:\t 123 kB",
		"Blah:\t 123 kB",
		"Blah:",
		"123",
	}

	for _, example := range badExamples {
		_, ok := ptree.ParseRSSAnon(example)
		assert.Falsef(t, ok, "should not be able to parse %q", example)
	}
}

func BenchmarkParseRss(b *testing.B) {
	b.Run("match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rss, ok := ptree.ParseRSSAnon("RssAnon:\t   15032 kB")
			require.True(b, ok)
			require.EqualValues(b, 15032*1024, rss)
		}
	})

	b.Run("no match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, ok := ptree.ParseRSSAnon("Other:\t   15032 kB")
			require.False(b, ok)
		}
	})
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
