// Package ptree contains utilities for dealing with Linux process trees.
package ptree

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	errNoRss  = errors.New("RssAnon was not found")
	rssAnonRE = regexp.MustCompile(`^RssAnon:\s*(\d+)\s+kB($|\s)`)
)

type ProcessTree struct {
	path string
}

func NewProcessTree(path string) ProcessTree {
	return ProcessTree{
		path: path,
	}
}

// Return the RSSAnon of a single process `pid`.
func (pt ProcessTree) GetProcessRSSAnon(pid int) (uint64, error) {
	status := pt.path + "/" + strconv.Itoa(pid) + "/status"
	f, err := os.Open(status)
	if os.IsNotExist(err) {
		// process is already gone
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if rss, ok := ParseRSSAnon(line); ok {
			return rss, nil
		}
	}
	if scan.Err() != nil {
		return 0, scan.Err()
	}
	return 0, errNoRss
}

// Return the total RSS of the tree of processes rooted at `pid`.
//
// If the passed root pid is that of a kernel thread, as a special case, we
// return zero and no error.
//
// Errors encountered while walking the children are ignored, since it can
// change while traversing it.
func (pt ProcessTree) GetProcessTreeRSSAnon(pid int) (uint64, error) {
	total, err := pt.GetProcessRSSAnon(pid)
	if err != nil {
		if err == errNoRss {
			// these are typically kernel threads, which don't have an address space to measure
			return 0, nil
		}
		return 0, err
	}

	pt.WalkChildren(pid, func(pid int) {
		mem, err := pt.GetProcessRSSAnon(pid)
		if err != nil {
			return
		}
		total += mem
	})

	return total, nil
}

func (pt ProcessTree) WalkChildren(pid int, walkFn func(int)) {
	pt.walkChildPids(pid, walkFn, map[int]bool{pid: true})
}

func (pt ProcessTree) walkChildPids(pid int, walkFn func(int), visited map[int]bool) {
	matches, err := filepath.Glob(pt.path + "/" + strconv.Itoa(pid) + "/task/*/children")
	if err != nil {
		return
	}

	for _, filename := range matches {
		pt.walkChildrenFile(filename, walkFn, visited)
	}
}

func (pt ProcessTree) walkChildrenFile(filename string, walkFn func(int), visited map[int]bool) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}

	for _, pidStr := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if visited[pid] {
			continue
		}

		walkFn(pid)
		visited[pid] = true
		pt.walkChildPids(pid, walkFn, visited)
	}
}

// parseRSSAnon parses an "RssAnon" line from /proc/*/status and returns the size.
// The entire line should be passed in, with or without the line ending. If the
// line looks like "RssAnon: 1234 kB", the byte size will be returned. If the
// line isn't parseable, (0, false) will be returned.
func ParseRSSAnon(s string) (uint64, bool) {
	m := rssAnonRE.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	kb, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb * 1024, true
}
