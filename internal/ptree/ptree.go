// Package ptree contains utilities for dealing with Linux process trees.
package ptree

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	// initialReadBufSize is the starting capacity of the buffer used for
	// reading /proc files. /proc/<pid>/status is typically ~1 KiB and the
	// per-task "children" files are smaller, so 4 KiB covers the common
	// case in a single read.
	initialReadBufSize = 4 * 1024
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

// readBufPool reuses the byte buffer that holds the contents of a /proc file
// across calls to getProcessRSSAnon / walkChildrenFile, so that the
// per-poll work doesn't allocate (and then garbage-collect) a fresh buffer
// for every process in the tree.
var readBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, initialReadBufSize)
		return &b
	},
}

// readProcFile reads all of path into a buffer borrowed from readBufPool.
//
// On success, the returned slice is only valid until bufPtr is returned to
// the pool, which the caller MUST do (typically with
// `defer readBufPool.Put(bufPtr)`, placed after the error check).
//
// On error, bufPtr is nil and the buffer has already been released, so the
// caller must not Put it back.
//
// Compared to os.ReadFile, this skips the (useless for /proc) Stat call
// used to pre-size the buffer and reuses the buffer across calls. It also
// returns the underlying *[]byte rather than a closure so the release path
// allocates nothing.
func readProcFile(path string) (data []byte, bufPtr *[]byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	bufPtr = readBufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	for {
		if len(buf) == cap(buf) {
			// Grow via append; this only allocates if the pooled
			// buffer was too small. Subsequent calls will reuse
			// the grown buffer because we store it back below.
			buf = append(buf, 0)[:len(buf)]
		}
		n, rerr := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			*bufPtr = buf
			readBufPool.Put(bufPtr)
			return nil, nil, rerr
		}
		if n == 0 {
			// Defensive: io.Reader allows (0, nil) returns; treat
			// as EOF rather than spinning. Real *os.File on /proc
			// shouldn't hit this, but mocks or future runtime
			// behavior might.
			break
		}
	}
	*bufPtr = buf
	return buf, bufPtr, nil
}

// Return the RSSAnon of a single process `pid`.
func (pt ProcessTree) GetProcessRSSAnon(pid int) (uint64, error) {
	status := pt.path + "/" + strconv.Itoa(pid) + "/status"
	data, bufPtr, err := readProcFile(status)
	if os.IsNotExist(err) {
		// process is already gone
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer readBufPool.Put(bufPtr)

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
	// List the per-thread directories under /proc/<pid>/task and read each
	// task's "children" file directly. This avoids filepath.Glob, which
	// would Stat every match on top of the readdir we already need.
	taskDir := pt.path + "/" + strconv.Itoa(pid) + "/task"
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		// task/ should only contain numeric TID directories. Skip
		// anything else defensively; this mirrors the implicit
		// filtering that filepath.Glob("*/children") provided.
		// A byte-range check avoids the error allocation that
		// strconv.Atoi would incur for non-numeric names.
		if !isAllDigits(entry.Name()) {
			continue
		}
		pt.walkChildrenFile(taskDir+"/"+entry.Name()+"/children", walkFn, visited)
	}
}

func (pt ProcessTree) walkChildrenFile(filename string, walkFn func(int), visited map[int]bool) {
	data, bufPtr, err := readProcFile(filename)
	if err != nil {
		return
	}
	defer readBufPool.Put(bufPtr)

	// children is a whitespace-separated list of decimal PIDs. Parse it in
	// place to avoid the string(data) conversion and the []string allocated
	// by strings.Fields.
	i := 0
	for i < len(data) {
		for i < len(data) && isASCIISpace(data[i]) {
			i++
		}
		if i >= len(data) {
			return
		}
		pid := 0
		start := i
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			pid = pid*10 + int(data[i]-'0')
			i++
		}
		if i == start {
			// Not a digit; skip until next whitespace to stay in sync.
			for i < len(data) && !isASCIISpace(data[i]) {
				i++
			}
			continue
		}
		if i-start > 10 {
			// Realistic Linux PIDs fit in well under 10 digits
			// (PID_MAX is 2^22). A longer digit run can't be a
			// real PID and would risk silently overflowing the
			// int accumulator, so skip it.
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

// isAllDigits reports whether s is non-empty and consists entirely of ASCII
// decimal digits. Used as a cheap allocation-free numeric-name filter.
func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
