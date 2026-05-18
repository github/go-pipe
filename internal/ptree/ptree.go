// Package ptree contains utilities for dealing with Linux process trees.
package ptree

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var errNoRss = errors.New("RssAnon was not found")

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
	data, err := os.ReadFile(status)
	if os.IsNotExist(err) {
		// process is already gone
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	prefix := []byte("RssAnon:")
	rest := data
	for len(rest) > 0 {
		var line []byte
		if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
			line, rest = rest[:nl], rest[nl+1:]
		} else {
			line, rest = rest, nil
		}
		// Fast prefix check before paying for the string conversion.
		if !bytes.HasPrefix(line, prefix) {
			continue
		}
		if rss, ok := ParseRSSAnon(string(line)); ok {
			return rss, nil
		}
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
	const prefix = "RssAnon:"
	if !strings.HasPrefix(s, prefix) {
		return 0, false
	}
	s = s[len(prefix):]

	// Optional whitespace before the number.
	i := 0
	for i < len(s) && isASCIISpace(s[i]) {
		i++
	}

	// One or more digits.
	digitsStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digitsStart {
		return 0, false
	}
	kb, err := strconv.ParseUint(s[digitsStart:i], 10, 64)
	if err != nil {
		return 0, false
	}

	// At least one whitespace between the number and "kB".
	if i >= len(s) || !isASCIISpace(s[i]) {
		return 0, false
	}
	for i < len(s) && isASCIISpace(s[i]) {
		i++
	}

	// Literal "kB", then either end-of-string or whitespace.
	const unit = "kB"
	if !strings.HasPrefix(s[i:], unit) {
		return 0, false
	}
	i += len(unit)
	if i < len(s) && !isASCIISpace(s[i]) {
		return 0, false
	}
	return kb * 1024, true
}

// isASCIISpace matches the character class that Go's regexp engine uses for
// \s in non-Unicode mode: [\t\n\f\r ].
func isASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	}
	return false
}
