package docker

import (
	"os"
	"runtime"
)

func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	terminal := isTerminal(f.Fd())
	// The file's finalizer must not close its descriptor during the native query.
	runtime.KeepAlive(f)
	return terminal
}
