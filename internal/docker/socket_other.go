//go:build !unix

package docker

import "fmt"

func socketGroup(socket string) (string, error) {
	return "", fmt.Errorf("read group of docker socket %q: unix file metadata is unavailable", socket)
}
