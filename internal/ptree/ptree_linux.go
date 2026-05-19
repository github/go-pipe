package ptree

var DefaultProcessTree = ProcessTree{
	path: "/proc",
}

// Walk the child processes of the specified root process. walkFn will be called
// for each child found. It will not be called for the root process. Any errors
// will be ignored, since they may be just a consequence of the process tree
// changing during traversal.
func WalkChildren(pid int, walkFn func(int)) {
	DefaultProcessTree.WalkChildren(pid, walkFn)
}

func GetProcessRSSAnon(pid int) (uint64, error) {
	return DefaultProcessTree.GetProcessRSSAnon(pid)
}

func GetProcessTreeRSSAnon(pid int) (uint64, error) {
	return DefaultProcessTree.GetProcessTreeRSSAnon(pid)
}
