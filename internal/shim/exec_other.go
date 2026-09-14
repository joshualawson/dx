//go:build !unix && !windows

package shim

import "fmt"

func ExecLocal(path string, args, env []string) (int, error) {
	return -1, fmt.Errorf("exec local executable %q: unsupported operating system", path)
}
