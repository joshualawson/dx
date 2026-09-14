//go:build unix

package shim

import (
	"fmt"
	"path/filepath"
	"syscall"
)

func ExecLocal(path string, args, env []string) (int, error) {
	err := syscall.Exec(path, append([]string{filepath.Base(path)}, args...), env)
	return -1, fmt.Errorf("exec local executable %q: %w", path, err)
}
