package docker

import (
	"context"
	"fmt"
	"strings"
)

const containerSocket = "/var/run/docker.sock"

func (r *Runner) SocketMount(ctx context.Context, goos string, getenv func(string) string) (Mount, string, error) {
	var host string
	if goos == "linux" {
		if getenv != nil {
			host = getenv("DOCKER_HOST")
		}
		if host == "" {
			contextHost, err := r.Output(ctx, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
			if err != nil {
				return Mount{}, "", err
			}
			host = contextHost
		}
	}
	source, err := socketSource(goos, host)
	if err != nil {
		return Mount{}, "", err
	}
	var group string
	if goos == "linux" && !isDesktopSocket(strings.TrimPrefix(host, "unix://")) {
		group, err = socketGroup(source)
		if err != nil {
			return Mount{}, "", err
		}
	}
	return Mount{Type: Bind, Source: source, Target: containerSocket}, group, nil
}

func socketSource(goos, host string) (string, error) {
	switch goos {
	case "darwin", "windows":
		return containerSocket, nil
	case "linux":
		socket, err := parseSocketHost(host)
		if err != nil {
			return "", err
		}
		if isDesktopSocket(socket) {
			return containerSocket, nil
		}
		return socket, nil
	default:
		return "", fmt.Errorf("docker socket mounts are unsupported on %q", goos)
	}
}

func isDesktopSocket(socket string) bool {
	return strings.Contains(socket, "/.docker/desktop/")
}

func parseSocketHost(host string) (string, error) {
	if !strings.HasPrefix(host, "unix://") {
		return "", fmt.Errorf("docker host %q cannot be mounted: a local unix:// socket is required, not a tcp/ssh host", host)
	}
	socket := strings.TrimPrefix(host, "unix://")
	if socket == "" || !strings.HasPrefix(socket, "/") {
		return "", fmt.Errorf("docker host %q must name an absolute unix socket path", host)
	}
	return socket, nil
}
