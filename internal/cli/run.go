package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/docker"
	"github.com/joshualawson/dx/internal/hostenv"
	"github.com/joshualawson/dx/internal/route"
	"github.com/joshualawson/dx/internal/trust"
)

func mountRoot(e Env, override string) (string, error) {
	var root string
	var err error
	if override != "" {
		root, err = absolute(e, override)
	} else {
		root, err = e.FindRoot(e.Cwd, e.Home)
	}
	if err != nil {
		return "", err
	}
	if !trust.UnderDir(e.Cwd, []string{root}, e.GOOS) {
		return "", fmt.Errorf("current folder %s is outside the mounted folder %s", e.Cwd, root)
	}
	return root, nil
}

func runSpec(ctx context.Context, e Env, in invocation, cfg config.Config, root string, image route.Result) (docker.RunSpec, error) {
	spec := docker.RunSpec{
		Image: image.Image, Cmd: in.Command, Remove: true, Init: true, Interactive: true,
		TTY: e.StdinTTY && e.StdoutTTY, Workdir: hostenv.ContainerPath(e.Cwd, e.GOOS),
		Labels: map[string]string{"dx": "1", "dx.root": root, "dx.toolchain": image.Toolchain}, Ports: in.Ports,
	}
	home := ""
	if e.Home != "" {
		home = hostenv.ContainerPath(e.Home, e.GOOS)
		spec.Mounts = append(spec.Mounts, docker.Mount{Type: docker.Volume, Source: "dx-home", Target: home})
		for _, m := range hostenv.CredentialMounts(e.Home, e.GOOS, cfg.CredentialEnabled, e.Exists) {
			spec.Mounts = append(spec.Mounts, bind(m))
		}
	}
	spec.Mounts = append(spec.Mounts, docker.Mount{Type: docker.Bind, Source: root, Target: hostenv.ContainerPath(root, e.GOOS)})
	if isDXImage(image.Image) {
		switch image.Toolchain {
		case "go", "node", "python", "rust", "infra":
			spec.Mounts = append(spec.Mounts, docker.Mount{Type: docker.Volume, Source: "dx-cache-" + image.Toolchain, Target: path.Join("/cache", image.Toolchain)})
		}
	}
	spec.Env = hostenv.FilterEnv(e.Environ, e.GOOS)
	if cfg.CredentialEnabled("ssh") {
		m, env := hostenv.SSHAgent(e.GOOS, e.Getenv, e.Exists)
		if m != nil {
			spec.Mounts = append(spec.Mounts, bind(*m))
		}
		spec.Env = append(spec.Env, env...)
	}
	extra := map[string]int{}
	if in.Docker || cfg.Docker {
		m, group, err := e.Docker.SocketMount(ctx, e.GOOS, e.Getenv)
		if err != nil {
			return spec, err
		}
		spec.Mounts = append(spec.Mounts, m)
		if group != "" {
			spec.GroupAdd = append(spec.GroupAdd, group)
			gid, err := strconv.Atoi(group)
			if err != nil {
				return spec, fmt.Errorf("parse docker socket group %q: %w", group, err)
			}
			extra["docker"] = gid
		}
	}
	if e.GOOS == "linux" {
		spec.User = fmt.Sprintf("%d:%d", e.UID, e.GID)
		for _, file := range []struct{ name, content, target string }{
			{"passwd", hostenv.Passwd(e.Username, e.UID, e.GID, home), "/etc/passwd"},
			{"group", hostenv.Group(e.Username, e.GID, extra), "/etc/group"},
		} {
			filename := hostJoin(e.GOOS, e.StateDir, "identity", file.name)
			if err := writeChanged(e, filename, file.content); err != nil {
				return spec, err
			}
			spec.Mounts = append(spec.Mounts, docker.Mount{Type: docker.Bind, Source: filename, Target: file.target, ReadOnly: true})
		}
	}
	spec.Env = append(spec.Env, "HOME="+home, "USER="+e.Username, "LOGNAME="+e.Username)
	if isGoBuild(in.Command) {
		for _, v := range []struct{ key, value string }{{"GOOS", e.GOOS}, {"GOARCH", e.GOARCH}} {
			value, found := lookupEnv(e.Environ, v.key, e.GOOS)
			if !found {
				value = e.Getenv(v.key)
				if value == "" {
					value = v.value
				}
			}
			spec.Env = append(spec.Env, v.key+"="+value)
		}
	}
	keys := make([]string, 0, len(cfg.Env))
	for key := range cfg.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		spec.Env = append(spec.Env, key+"="+cfg.Env[key])
	}
	spec.Env = mergeEnv(spec.Env)
	if len(in.Ports) == 0 {
		works, err := e.Docker.HostNetworkWorks(ctx, hostJoin(e.GOOS, e.StateDir, "network.json"))
		if err != nil {
			fmt.Fprintf(e.Stderr, "dx: warning: %s\n", strings.Join(strings.Fields(err.Error()), " "))
		} else if works {
			spec.Network = "host"
		}
	}
	return spec, nil
}

func isGoBuild(command []string) bool {
	if len(command) == 0 || command[0] != "go" {
		return false
	}
	for i := 1; i < len(command); i++ {
		// The directory consumed by -C is not the Go subcommand.
		if command[i] == "-C" {
			i++
			continue
		}
		if !strings.HasPrefix(command[i], "-") {
			return command[i] == "build"
		}
	}
	return false
}

func isDXImage(ref string) bool {
	ref, _, _ = strings.Cut(ref, "@")
	if tag := strings.LastIndex(ref, ":"); tag > strings.LastIndex(ref, "/") {
		ref = ref[:tag]
	}
	return strings.HasPrefix(ref[strings.LastIndex(ref, "/")+1:], "dx-")
}

func credentialParents(credentials []hostenv.Credential) []string {
	parents := map[string]bool{}
	for _, credential := range credentials {
		for _, relative := range credential.Paths {
			for dir := path.Dir(path.Join("/", relative)); dir != "/"; dir = path.Dir(dir) {
				parents[path.Join("/h", dir)] = true
			}
		}
	}
	folders := make([]string, 0, len(parents))
	for folder := range parents {
		folders = append(folders, folder)
	}
	sort.Strings(folders)
	return folders
}

func bind(m hostenv.Mount) docker.Mount {
	return docker.Mount{Type: docker.Bind, Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly}
}

func lookupEnv(env []string, key, goos string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && (k == key || (goos == "windows" && strings.EqualFold(k, key))) {
			return v, true
		}
	}
	return "", false
}

func mergeEnv(env []string) []string {
	result := make([]string, 0, len(env))
	positions := map[string]int{}
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if i, ok := positions[key]; ok {
			result[i] = entry
		} else {
			positions[key] = len(result)
			result = append(result, entry)
		}
	}
	return result
}

func writeChanged(e Env, filename, content string) error {
	old, err := e.ReadFile(filename)
	if err == nil && string(old) == content {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read identity file %q: %w", filename, err)
	}
	if err := e.MkdirAll(hostDir(filename, e.GOOS), 0700); err != nil {
		return fmt.Errorf("create identity directory for %q: %w", filename, err)
	}
	if err := e.WriteFile(filename, []byte(content), 0644); err != nil {
		return fmt.Errorf("write identity file %q: %w", filename, err)
	}
	return nil
}

func prepareHome(ctx context.Context, e Env, image string) error {
	if e.GOOS == "linux" && e.Home != "" {
		if _, err := e.Docker.Output(ctx, "volume", "inspect", "dx-home"); err != nil {
			if _, err := e.Docker.Output(ctx, "volume", "create", "--label", "dx=1", "dx-home"); err != nil {
				return err
			}
			// Pre-create credential parents before Docker can create them as root-owned bind targets.
			args := []string{"run", "--rm", "--user", "0:0", "--mount", "type=volume,src=dx-home,dst=/h", image,
				"/bin/sh", "-c", `owner=$1; shift; mkdir -p "$@" && chown "$owner" "$@"`, "dx-home", fmt.Sprintf("%d:%d", e.UID, e.GID), "/h"}
			args = append(args, credentialParents(hostenv.Credentials)...)
			if _, err := e.Docker.Output(ctx, args...); err != nil {
				return err
			}
		}
	}
	return nil
}
