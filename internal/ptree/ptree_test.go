package ptree_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/github/go-pipe/internal/ptree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeStatus creates only what GetProcessRSSAnon reads: <root>/<pid>/status.
// If rssKB is zero, the RssAnon line is omitted (mimicking kernel threads).
func writeStatus(t *testing.T, root string, pid int, rssKB uint64) {
	t.Helper()
	pidDir := filepath.Join(root, strconv.Itoa(pid))
	require.NoError(t, os.MkdirAll(pidDir, 0o755))
	var status string
	if rssKB > 0 {
		status = fmt.Sprintf("Name:\tfake\nRssAnon:\t%d kB\nVmSize:\t1000 kB\n", rssKB)
	} else {
		status = "Name:\tfake\nVmSize:\t1000 kB\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(pidDir, "status"), []byte(status), 0o600))
}

// writeChildren creates <root>/<pid>/task/<pid>/children containing the
// space-separated child pids. Only call this for processes that actually
// have children; getProcessTreeRSSAnon copes fine with the task/ directory
// being absent for leaves.
func writeChildren(t *testing.T, root string, pid int, children []int) {
	t.Helper()
	taskDir := filepath.Join(root, strconv.Itoa(pid), "task", strconv.Itoa(pid))
	require.NoError(t, os.MkdirAll(taskDir, 0o755))
	var buf bytes.Buffer
	for _, c := range children {
		fmt.Fprintf(&buf, "%d ", c)
	}
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "children"), buf.Bytes(), 0o600))
}

func TestGetProcessRSSAnon(t *testing.T) {
	const kb = 1024
	root := t.TempDir()
	writeStatus(t, root, 100, 15032)
	writeStatus(t, root, 101, 0) // kernel-thread-like: no RssAnon line

	pt := ptree.NewProcessTree(root)

	t.Run("reads RssAnon", func(t *testing.T) {
		rss, err := pt.GetProcessRSSAnon(100)
		require.NoError(t, err)
		assert.Equal(t, uint64(15032*kb), rss)
	})

	t.Run("missing RssAnon line returns an error", func(t *testing.T) {
		_, err := pt.GetProcessRSSAnon(101)
		assert.Error(t, err)
	})

	t.Run("missing pid returns (0, nil)", func(t *testing.T) {
		// A process that has already exited disappears from /proc; the function
		// treats that as a non-error zero.
		rss, err := pt.GetProcessRSSAnon(999)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), rss)
	})
}

func TestGetProcessTreeRSSAnon(t *testing.T) {
	const kb = 1024

	t.Run("leaf process returns its own RssAnon", func(t *testing.T) {
		root := t.TempDir()
		writeStatus(t, root, 100, 1000)

		pt := ptree.NewProcessTree(root)

		total, err := pt.GetProcessTreeRSSAnon(100)
		require.NoError(t, err)
		assert.Equal(t, uint64(1000*kb), total)
	})

	t.Run("sums root and descendants", func(t *testing.T) {
		// 100 -> {101 -> 103, 102}
		root := t.TempDir()
		writeStatus(t, root, 100, 1000)
		writeStatus(t, root, 101, 200)
		writeStatus(t, root, 102, 50)
		writeStatus(t, root, 103, 7)
		writeChildren(t, root, 100, []int{101, 102})
		writeChildren(t, root, 101, []int{103})

		pt := ptree.NewProcessTree(root)

		total, err := pt.GetProcessTreeRSSAnon(100)
		require.NoError(t, err)
		assert.Equal(t, uint64((1000+200+50+7)*kb), total)
	})

	t.Run("kernel-thread root returns (0, nil)", func(t *testing.T) {
		// Root has no RssAnon line; the function maps errNoRss to (0, nil).
		root := t.TempDir()
		writeStatus(t, root, 100, 0)

		pt := ptree.NewProcessTree(root)

		total, err := pt.GetProcessTreeRSSAnon(100)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), total)
	})
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
