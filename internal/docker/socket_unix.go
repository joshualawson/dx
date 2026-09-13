//go:build unix

package docker

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

func socketGroup(socket string) (string, error) {
	info, err := os.Stat(socket)
	if err != nil {
		return "", fmt.Errorf("stat docker socket %q: %w", socket, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("read group of docker socket %q: unsupported file metadata", socket)
	}
	return strconv.FormatUint(uint64(stat.Gid), 10), nil
}
