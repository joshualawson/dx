//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package docker

func isTerminal(uintptr) bool {
	return false
}
