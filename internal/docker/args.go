package docker

import (
	"encoding/csv"
	"sort"
	"strings"
)

type MountType string

const (
	Bind   MountType = "bind"
	Volume MountType = "volume"
	Tmpfs  MountType = "tmpfs"
)

type Mount struct {
	Type     MountType
	Source   string
	Target   string
	ReadOnly bool
}

type RunSpec struct {
	Image       string
	Cmd         []string
	Name        string
	Workdir     string
	User        string
	GroupAdd    []string
	Env         []string
	Mounts      []Mount
	Network     string
	Ports       []string
	Labels      map[string]string
	Interactive bool
	TTY         bool
	Init        bool
	Remove      bool
	Detach      bool
}

func RunArgs(spec RunSpec) []string {
	args := []string{"run"}
	for _, flag := range []struct {
		enabled bool
		name    string
	}{
		{spec.Interactive, "-i"},
		{spec.TTY, "-t"},
		{spec.Init, "--init"},
		{spec.Remove, "--rm"},
		{spec.Detach, "-d"},
	} {
		if flag.enabled {
			args = append(args, flag.name)
		}
	}
	for _, flag := range []struct {
		name  string
		value string
	}{
		{"--name", spec.Name},
		{"--workdir", spec.Workdir},
		{"--user", spec.User},
	} {
		if flag.value != "" {
			args = append(args, flag.name, flag.value)
		}
	}
	for _, group := range spec.GroupAdd {
		args = append(args, "--group-add", group)
	}
	args = append(args, envArgs(spec.Env)...)
	for _, mount := range spec.Mounts {
		args = append(args, "--mount", mountArg(mount))
	}
	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}
	for _, port := range spec.Ports {
		args = append(args, "-p", port)
	}
	keys := make([]string, 0, len(spec.Labels))
	for key := range spec.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	if spec.Image != "" {
		args = append(args, spec.Image)
	}
	return append(args, spec.Cmd...)
}

func ProcessEnv(spec RunSpec) []string {
	var env []string
	for _, entry := range uniqueEnv(spec.Env) {
		key, _, _ := strings.Cut(entry, "=")
		if !explicitEnv(key) {
			env = append(env, entry)
		}
	}
	return env
}

func envArgs(env []string) []string {
	var args []string
	for _, entry := range uniqueEnv(env) {
		key, _, _ := strings.Cut(entry, "=")
		if !explicitEnv(key) {
			entry = key
		}
		args = append(args, "-e", entry)
	}
	return args
}

func explicitEnv(key string) bool {
	// Container overrides must not reconfigure the Docker CLI itself.
	return key == "HOME" || key == "PATH" || strings.HasPrefix(key, "DOCKER_")
}

func uniqueEnv(env []string) []string {
	last := make(map[string]int, len(env))
	for i, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		last[key] = i
	}
	var result []string
	for i, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if last[key] == i {
			result = append(result, entry)
		}
	}
	return result
}

func mountArg(mount Mount) string {
	fields := make([]string, 0, 4)
	if mount.Type != "" {
		fields = append(fields, "type="+string(mount.Type))
	}
	if mount.Source != "" {
		fields = append(fields, "src="+mount.Source)
	}
	if mount.Target != "" {
		fields = append(fields, "dst="+mount.Target)
	}
	if mount.ReadOnly {
		fields = append(fields, "readonly")
	}
	var value strings.Builder
	writer := csv.NewWriter(&value)
	// Docker parses each mount as a CSV record, not as shell-quoted text.
	_ = writer.Write(fields)
	writer.Flush()
	return strings.TrimSuffix(value.String(), "\n")
}
