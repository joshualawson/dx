package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type networkCheck struct {
	HostNetwork bool      `json:"host_network"`
	CheckedAt   time.Time `json:"checked_at"`
}

func (r *Runner) HostNetworkWorks(ctx context.Context, cachePath string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	name, err := r.ContextName(ctx)
	if err != nil {
		return false, err
	}
	cache := make(map[string]networkCheck)
	data, err := os.ReadFile(cachePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read host network cache %q: %w", cachePath, err)
	}
	if err == nil {
		if json.Unmarshal(data, &cache) != nil || cache == nil {
			cache = make(map[string]networkCheck)
		}
	}
	if check, ok := cache[name]; ok {
		return check.HostNetwork, nil
	}
	works, err := r.probeHostNetwork(ctx)
	if err != nil {
		return false, err
	}
	cache[name] = networkCheck{HostNetwork: works, CheckedAt: time.Now().UTC()}
	if err := writeNetworkCache(cachePath, cache); err != nil {
		return false, err
	}
	return works, nil
}

func ForgetHostNetwork(cachePath string) error {
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove host network cache %q: %w", cachePath, err)
	}
	return nil
}

func (r *Runner) probeHostNetwork(ctx context.Context) (bool, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return false, fmt.Errorf("listen for docker host network probe: %w", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("dx-probe"))
		}),
		ReadHeaderTimeout: 3 * time.Second,
	}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	output, err := r.Output(ctx, "run", "--rm", "--network", "host", "busybox:1.37", "wget", "-q", "-T", "3", "-O", "-", "http://"+listener.Addr().String()+"/")
	if ctx.Err() != nil {
		return false, fmt.Errorf("docker host network probe: %w", ctx.Err())
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() != 125 {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(output, "dx-probe"), nil
}

func writeNetworkCache(cachePath string, cache map[string]networkCheck) error {
	parent := filepath.Dir(cachePath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create host network cache directory %q: %w", parent, err)
	}
	file, err := os.CreateTemp(parent, ".dx-network-*")
	if err != nil {
		return fmt.Errorf("create host network cache %q: %w", cachePath, err)
	}
	defer os.Remove(file.Name())
	writeErr := json.NewEncoder(file).Encode(cache)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write host network cache %q: %w", cachePath, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close host network cache %q: %w", cachePath, closeErr)
	}
	// A partial write must not destroy answers already cached for other contexts.
	if err := os.Rename(file.Name(), cachePath); err != nil {
		return fmt.Errorf("replace host network cache %q: %w", cachePath, err)
	}
	return nil
}
