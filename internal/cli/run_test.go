package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/docker"
	"github.com/joshualawson/dx/internal/hostenv"
	"github.com/joshualawson/dx/internal/route"
)

func TestRunSpecPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			e.GOOS = goos
			e.Home, e.Cwd = "/home/me", "/home/me/src/sub"
			root := "/home/me/src"
			if goos == "windows" {
				e.Home, e.Cwd, root = `C:\Users\me`, `C:\Users\me\src\sub`, `C:\Users\me\src`
			}
			e.StdinTTY, e.StdoutTTY = true, true
			e.Getenv = func(k string) string {
				if k == "SSH_AUTH_SOCK" {
					return "/agent.sock"
				}
				return ""
			}
			// Credential fixtures describe the simulated OS, not the test host.
			git, aws := "/home/me/.gitconfig", "/home/me/.aws"
			if goos == "windows" {
				git, aws = `C:\Users\me\.gitconfig`, `C:\Users\me\.aws`
			}
			e.Exists = func(p string) bool { return p == git || p == aws || p == "/agent.sock" }
			e.Environ = []string{"PATH=/host/bin", "HOME=/wrong", "TOKEN=host", "TOKEN=second", "SSH_AUTH_SOCK=/agent.sock", "USER=wrong"}
			f.group = "987"
			cfg := config.Config{Credentials: map[string]bool{"aws": false}, Env: map[string]string{"TOKEN": "config", "USER": "override"}}
			in := invocation{Command: []string{"go", "build", "-v"}, Docker: true}
			image := route.Result{Image: "example/dx-go:1", Toolchain: "go"}
			spec, err := runSpec(context.Background(), e, in, cfg, root, image)
			if err != nil {
				t.Fatal(err)
			}
			if !spec.Remove || !spec.Init || !spec.Interactive || !spec.TTY || spec.Detach {
				t.Fatalf("flags = %#v", spec)
			}
			if spec.Image != image.Image || !reflect.DeepEqual(spec.Cmd, in.Command) {
				t.Fatalf("command = %#v", spec)
			}
			if spec.Workdir != hostenv.ContainerPath(e.Cwd, goos) {
				t.Fatalf("workdir = %q", spec.Workdir)
			}
			if goos == "windows" && spec.Workdir != "/c/Users/me/src/sub" {
				t.Fatalf("windows workdir = %q", spec.Workdir)
			}
			wantLabels := map[string]string{"dx": "1", "dx.root": root, "dx.toolchain": "go"}
			if !reflect.DeepEqual(spec.Labels, wantLabels) {
				t.Fatalf("labels = %v", spec.Labels)
			}
			mounts := []docker.Mount{
				{Type: docker.Volume, Source: "dx-home", Target: hostenv.ContainerPath(e.Home, goos)},
				{Type: docker.Bind, Source: git, Target: hostenv.ContainerPath(git, goos)},
				{Type: docker.Bind, Source: root, Target: hostenv.ContainerPath(root, goos)},
				{Type: docker.Volume, Source: "dx-cache-go", Target: "/cache/go"},
			}
			if goos != "windows" {
				source := "/agent.sock"
				if goos == "darwin" {
					source = "/run/host-services/ssh-auth.sock"
				}
				mounts = append(mounts, docker.Mount{Type: docker.Bind, Source: source, Target: "/run/host-services/ssh-auth.sock"})
			}
			mounts = append(mounts, docker.Mount{Type: docker.Bind, Source: "/host/docker.sock", Target: "/var/run/docker.sock"})
			if goos == "linux" {
				if spec.User != "1000:1001" {
					t.Fatalf("user = %q", spec.User)
				}
				for _, name := range []string{"passwd", "group"} {
					filename := filepath.Join(e.StateDir, "identity", name)
					mounts = append(mounts, docker.Mount{Type: docker.Bind, Source: filename, Target: "/etc/" + name, ReadOnly: true})
					data, err := os.ReadFile(filename)
					if err != nil {
						t.Fatal(err)
					}
					want := hostenv.Passwd("me", 1000, 1001, "/home/me")
					if name == "group" {
						want = hostenv.Group("me", 1001, map[string]int{"docker": 987})
					}
					if string(data) != want {
						t.Fatalf("%s = %q, want %q", filename, data, want)
					}
				}
			} else if spec.User != "" {
				t.Fatalf("user = %q", spec.User)
			}
			if !reflect.DeepEqual(spec.Mounts, mounts) {
				t.Fatalf("mounts = %#v, want %#v", spec.Mounts, mounts)
			}
			if !reflect.DeepEqual(spec.GroupAdd, []string{"987"}) || f.socketCalls != 1 {
				t.Fatalf("socket groups = %v, calls = %d", spec.GroupAdd, f.socketCalls)
			}
			env := envMap(spec.Env)
			for k, v := range map[string]string{"HOME": hostenv.ContainerPath(e.Home, goos), "USER": "override", "LOGNAME": "me", "TOKEN": "config", "GOOS": goos, "GOARCH": "amd64"} {
				if env[k] != v {
					t.Fatalf("env[%s] = %q, want %q", k, env[k], v)
				}
			}
			if _, ok := env["PATH"]; ok {
				t.Fatal("host PATH leaked")
			}
			if goos != "windows" && env["SSH_AUTH_SOCK"] != "/run/host-services/ssh-auth.sock" {
				t.Fatalf("agent env = %v", env)
			}
			if goos == "windows" && env["SSH_AUTH_SOCK"] != "" {
				t.Fatal("windows agent forwarded")
			}
			if spec.Network != "host" || f.networkCalls != 1 || stderr.Len() != 0 {
				t.Fatalf("network = %q, stderr = %s", spec.Network, stderr)
			}
		})
	}
}

func TestToolchainCacheAndTargets(t *testing.T) {
	tests := []struct{ cmd, image, chain string }{
		{cmd: "go", chain: "go"}, {cmd: "npm", chain: "node"}, {cmd: "uv", chain: "python"},
		{cmd: "cargo", chain: "rust"}, {cmd: "terraform", chain: "infra"},
		{cmd: "sh", image: "busybox:1.37"}, {cmd: "other", chain: "base"},
		{cmd: "go", image: "golang:1.25", chain: "go"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			e, _, _, _ := testEnv(t)
			e.GOOS = "darwin"
			in := invocation{Command: []string{tt.cmd}, Image: tt.image}
			image := resolve(config.Config{}, tt.cmd, tt.image, nil)
			if image.Toolchain != tt.chain {
				t.Fatalf("chain = %s", image.Toolchain)
			}
			spec, err := runSpec(context.Background(), e, in, config.Config{}, e.Cwd, image)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, m := range spec.Mounts {
				if strings.HasPrefix(m.Source, "dx-cache-") {
					count++
					if m.Source != "dx-cache-"+tt.chain || m.Target != "/cache/"+tt.chain {
						t.Fatalf("cache = %#v", m)
					}
				}
			}
			want := 1
			if tt.chain == "" || tt.chain == "base" || tt.image != "" {
				want = 0
			}
			if count != want {
				t.Fatalf("cache count = %d, want %d", count, want)
			}
			_, hasOS := envMap(spec.Env)["GOOS"]
			_, hasArch := envMap(spec.Env)["GOARCH"]
			if hasOS || hasArch {
				t.Fatalf("env = %v", spec.Env)
			}
		})
	}
}

func TestEnvPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		host     []string
		cfg      map[string]string
		os, arch string
	}{
		{name: "defaults", os: "darwin", arch: "amd64"},
		{name: "host targets", host: []string{"GOOS=freebsd", "GOARCH=arm64"}, os: "freebsd", arch: "arm64"},
		{name: "one host target", host: []string{"GOOS=linux"}, os: "linux", arch: "amd64"},
		{name: "config targets", host: []string{"GOOS=freebsd", "GOARCH=arm64"}, cfg: map[string]string{"GOOS": "windows", "GOARCH": "386"}, os: "windows", arch: "386"},
		{name: "explicit empty", host: []string{"GOOS=", "GOARCH="}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _, _ := testEnv(t)
			e.GOOS, e.Environ = "darwin", tt.host
			spec, err := runSpec(context.Background(), e, invocation{Command: []string{"go", "build"}}, config.Config{Env: tt.cfg}, e.Cwd, route.Result{Image: "golang:1.25", Toolchain: "go"})
			if err != nil {
				t.Fatal(err)
			}
			env := envMap(spec.Env)
			if env["GOOS"] != tt.os || env["GOARCH"] != tt.arch {
				t.Fatalf("env = %v", env)
			}
		})
	}
}

func TestNetworkAndTTY(t *testing.T) {
	tests := []struct {
		name    string
		works   bool
		err     error
		ports   []string
		network string
		calls   int
	}{
		{name: "host", works: true, network: "host", calls: 1},
		{name: "bridge", calls: 1},
		{name: "failure", works: true, err: errors.New("docker probe\nfailed"), calls: 1},
		{name: "ports", works: true, ports: []string{"8080:80", "127.0.0.1:9090:90"}},
	}
	for _, tt := range tests {
		for _, stdin := range []bool{false, true} {
			for _, stdout := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t/%t", tt.name, stdin, stdout), func(t *testing.T) {
					e, f, _, stderr := testEnv(t)
					e.GOOS, e.StdinTTY, e.StdoutTTY = "darwin", stdin, stdout
					f.network, f.networkErr = tt.works, tt.err
					spec, err := runSpec(context.Background(), e, invocation{Command: []string{"sh"}, Ports: tt.ports}, config.Config{}, e.Cwd, route.Result{Image: "busybox:1.37"})
					if err != nil {
						t.Fatal(err)
					}
					if spec.Network != tt.network || f.networkCalls != tt.calls || !reflect.DeepEqual(spec.Ports, tt.ports) {
						t.Fatalf("network = %q, calls = %d, ports = %v", spec.Network, f.networkCalls, spec.Ports)
					}
					if spec.TTY != (stdin && stdout) || !spec.Interactive {
						t.Fatalf("tty = %t, interactive = %t", spec.TTY, spec.Interactive)
					}
					if tt.err != nil {
						if stderr.String() != "dx: warning: docker probe failed\n" {
							t.Fatalf("warning = %q", stderr.String())
						}
					} else if stderr.Len() != 0 {
						t.Fatalf("stderr = %s", stderr)
					}
				})
			}
		}
	}
}

func TestColdStartup(t *testing.T) {
	tests := []struct {
		name, goos     string
		exists, noHome bool
		calls          int
		outputErr      error
	}{
		{name: "linux new", goos: "linux", calls: 3},
		{name: "linux existing", goos: "linux", exists: true, calls: 1},
		{name: "linux no home", goos: "linux", noHome: true},
		{name: "darwin", goos: "darwin"}, {name: "windows", goos: "windows"},
		{name: "create failure", goos: "linux", calls: 2, outputErr: errors.New("docker volume create: denied")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, f, _, _ := testEnv(t)
			e.GOOS, f.volumeExists, f.outputErr, f.code = tt.goos, tt.exists, tt.outputErr, 7
			if tt.noHome {
				e.Home = ""
			}
			spec := docker.RunSpec{Image: "golang:1.25", Cmd: []string{"go", "version"}, Env: []string{"TOKEN=secret", "HOME=/container/home"}}
			code, err := startContainer(context.Background(), e, spec, false, 0)
			if tt.outputErr != nil {
				if !errors.Is(err, tt.outputErr) || f.args != nil {
					t.Fatalf("error = %v, run = %v", err, f.args)
				}
			} else {
				if err != nil || code != 7 {
					t.Fatalf("result = %d, %v", code, err)
				}
				if !reflect.DeepEqual(f.args, docker.RunArgs(spec)) || !reflect.DeepEqual(f.env, docker.ProcessEnv(spec)) {
					t.Fatalf("run args = %v, env = %v", f.args, f.env)
				}
			}
			if len(f.calls) != tt.calls {
				t.Fatalf("calls = %v, want %d", f.calls, tt.calls)
			}
			if tt.calls == 3 {
				want := [][]string{
					{"volume", "inspect", "dx-home"},
					{"volume", "create", "--label", "dx=1", "dx-home"},
					{"run", "--rm", "--user", "0:0", "--mount", "type=volume,src=dx-home,dst=/h", "golang:1.25", "/bin/sh", "-c", `owner=$1; shift; mkdir -p "$@" && chown "$owner" "$@"`, "dx-home", "1000:1001", "/h", "/h/.cargo", "/h/.config", "/h/.docker"},
				}
				if !reflect.DeepEqual(f.calls, want) {
					t.Fatalf("calls = %v", f.calls)
				}
			}
		})
	}
}

func TestIdentityOnlyWritesChanges(t *testing.T) {
	e, _, _, _ := testEnv(t)
	writes := 0
	e.WriteFile = func(p string, data []byte, mode os.FileMode) error { writes++; return os.WriteFile(p, data, mode) }
	for _, tt := range []struct{ gid, writes int }{{1001, 2}, {1001, 2}, {1002, 4}} {
		t.Run(fmt.Sprint(tt), func(t *testing.T) {
			e.GID = tt.gid
			if _, err := runSpec(context.Background(), e, invocation{Command: []string{"sh"}}, config.Config{}, e.Cwd, route.Result{Image: "busybox:1.37"}); err != nil {
				t.Fatal(err)
			}
			if writes != tt.writes {
				t.Fatalf("writes = %d, want %d", writes, tt.writes)
			}
		})
	}
}

func TestCredentialsAndSocketSwitches(t *testing.T) {
	tests := []struct {
		name                                   string
		cfg                                    config.Config
		flag, home, agent, credentials, socket bool
	}{
		{name: "defaults", home: true, agent: true, credentials: true},
		{name: "disabled", cfg: config.Config{Credentials: map[string]bool{"all": false}}, home: true},
		{name: "ssh disabled", cfg: config.Config{Credentials: map[string]bool{"ssh": false}}, home: true, credentials: true},
		{name: "docker config", cfg: config.Config{Docker: true}, home: true, agent: true, credentials: true, socket: true},
		{name: "docker flag", flag: true, home: true, agent: true, credentials: true, socket: true},
		{name: "no home", agent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, f, _, _ := testEnv(t)
			e.GOOS = "darwin"
			if !tt.home {
				e.Home = ""
			}
			e.Getenv = func(k string) string {
				if k == "SSH_AUTH_SOCK" {
					return "/agent"
				}
				return ""
			}
			e.Exists = func(string) bool { return true }
			spec, err := runSpec(context.Background(), e, invocation{Command: []string{"sh"}, Docker: tt.flag}, tt.cfg, e.Cwd, route.Result{Image: "busybox:1.37"})
			if err != nil {
				t.Fatal(err)
			}
			home, agent, credentials := false, false, false
			for _, m := range spec.Mounts {
				home = home || m.Source == "dx-home"
				agent = agent || m.Target == "/run/host-services/ssh-auth.sock"
				credentials = credentials || strings.HasSuffix(m.Target, "/.gitconfig")
			}
			if home != tt.home || agent != tt.agent || credentials != tt.credentials || (f.socketCalls == 1) != tt.socket {
				t.Fatalf("home=%t agent=%t credentials=%t socket=%d", home, agent, credentials, f.socketCalls)
			}
			if len(spec.GroupAdd) != 0 {
				t.Fatalf("empty socket group = %v", spec.GroupAdd)
			}
		})
	}
}

func envMap(entries []string) map[string]string {
	result := map[string]string{}
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		result[key] = value
	}
	return result
}
