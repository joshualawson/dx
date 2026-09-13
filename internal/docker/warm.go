package docker

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func WarmName(spec RunSpec) string {
	labels := spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	fixed := struct {
		Image    string
		User     string
		GroupAdd []string
		Mounts   []Mount
		Network  string
		Labels   map[string]string
	}{spec.Image, spec.User, append([]string{}, spec.GroupAdd...), append([]Mount{}, spec.Mounts...), spec.Network, labels}
	// JSON sorts map keys; empty collections must hash identically to nil collections.
	data, _ := json.Marshal(fixed)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("dx-warm-%x", hash[:8])
}

func (r *Runner) EnsureWarm(ctx context.Context, spec RunSpec, idleTimeout time.Duration) (string, error) {
	name := WarmName(spec)
	exists, running, err := r.warmState(ctx, name)
	if err != nil {
		return "", err
	}
	if running {
		return name, nil
	}
	if exists {
		if _, err := r.Output(ctx, "rm", "-f", name); err != nil {
			return "", err
		}
	}
	_ = r.CleanupStale(ctx)
	labels := make(map[string]string, len(spec.Labels)+3)
	for key, value := range spec.Labels {
		labels[key] = value
	}
	labels["dx"], labels["dx.warm"], labels["dx.key"] = "1", "1", strings.TrimPrefix(name, "dx-warm-")
	spec.Name, spec.Labels = name, labels
	spec.Detach, spec.Remove = true, true
	spec.Init, spec.Interactive, spec.TTY = false, false, false
	spec.Env, spec.Ports, spec.Cmd = nil, nil, nil
	args := append(RunArgs(spec), "dx-idle", "--timeout", idleTimeout.String())
	if _, err := r.Output(ctx, args...); err != nil {
		if !isNameConflict(err) {
			return "", err
		}
		if err := r.waitWarm(ctx, name); err != nil {
			return "", err
		}
	}
	return name, nil
}

func (r *Runner) warmState(ctx context.Context, name string) (bool, bool, error) {
	output, err := r.Output(ctx, "container", "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && ctx.Err() == nil {
			message := strings.ToLower(err.Error())
			if strings.Contains(message, "no such container") || strings.Contains(message, "no such object") {
				return false, false, nil
			}
		}
		return false, false, err
	}
	switch output {
	case "true":
		return true, true, nil
	case "false":
		return true, false, nil
	default:
		return false, false, fmt.Errorf("docker container inspect %q: unexpected running state %q", name, output)
	}
}

func isNameConflict(err error) bool {
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "container name") && strings.Contains(message, "already in use")
}

func (r *Runner) waitWarm(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, running, err := r.warmState(ctx, name)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for docker warm container %q: %w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}

type WarmContainer struct {
	Name      string
	Image     string
	Root      string
	Toolchain string
	Status    string
	Created   time.Time
}

func (r *Runner) ListWarm(ctx context.Context) ([]WarmContainer, error) {
	output, err := r.Output(ctx, "ps", "--filter", "label=dx.warm=1", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var containers []WarmContainer
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var row struct {
			Names     string
			Image     string
			Labels    string
			Status    string
			CreatedAt string
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("parse docker ps warm containers: %w", err)
		}
		created, err := parseCreatedAt(row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse docker ps creation time for %q: %w", row.Names, err)
		}
		container := WarmContainer{Name: row.Names, Image: row.Image, Status: row.Status, Created: created}
		for _, label := range strings.Split(row.Labels, ",") {
			key, value, _ := strings.Cut(label, "=")
			switch key {
			case "dx.root":
				container.Root = value
			case "dx.toolchain":
				container.Toolchain = value
			}
		}
		containers = append(containers, container)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read docker ps warm containers: %w", err)
	}
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].Root != containers[j].Root {
			return containers[i].Root < containers[j].Root
		}
		return containers[i].Name < containers[j].Name
	})
	return containers, nil
}

func parseCreatedAt(value string) (time.Time, error) {
	var err error
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", time.RFC3339Nano} {
		var created time.Time
		created, err = time.Parse(layout, value)
		if err == nil {
			return created, nil
		}
	}
	return time.Time{}, err
}

func (r *Runner) StopWarm(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	_, err := r.Output(ctx, append([]string{"stop", "--time", "5"}, names...)...)
	return err
}

func (r *Runner) CleanupStale(ctx context.Context) error {
	output, err := r.Output(ctx, "ps", "-a", "--filter", "label=dx.warm=1", "--filter", "status=exited", "--filter", "status=created", "--filter", "status=dead", "-q")
	if err != nil {
		return err
	}
	ids := strings.Fields(output)
	if len(ids) == 0 {
		return nil
	}
	_, err = r.Output(ctx, append([]string{"rm", "-f"}, ids...)...)
	return err
}
