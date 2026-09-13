package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/docker"
	"github.com/joshualawson/dx/internal/route"
)

func TestUseWarm(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name, chain, image     string
		commandWarm, chainWarm *bool
		cold, ports, want      bool
	}{
		{name: "go default", chain: "go", want: true},
		{name: "node default", chain: "node", want: true},
		{name: "python default", chain: "python", want: true},
		{name: "rust default", chain: "rust", want: true},
		{name: "infra default", chain: "infra"},
		{name: "base default", chain: "base"},
		{name: "unknown default"},
		{name: "ports", chain: "go", ports: true, commandWarm: &yes},
		{name: "cold", chain: "go", cold: true, commandWarm: &yes},
		{name: "custom image", chain: "go", image: "golang:1.25", commandWarm: &yes},
		{name: "command true", chain: "infra", commandWarm: &yes, want: true},
		{name: "command false", chain: "go", commandWarm: &no},
		{name: "chain true", chain: "infra", chainWarm: &yes, want: true},
		{name: "chain false", chain: "go", chainWarm: &no},
		{name: "command true beats chain false", chain: "go", commandWarm: &yes, chainWarm: &no, want: true},
		{name: "command false beats chain true", chain: "go", commandWarm: &no, chainWarm: &yes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image := tt.image
			if image == "" {
				image = "dx-local/dx-" + tt.chain + ":dev"
			}
			in := invocation{Command: []string{"tool"}, Cold: tt.cold}
			if tt.ports {
				in.Ports = []string{"8080:80"}
			}
			cfg := config.Config{Tools: map[string]config.Tool{"tool": {Warm: tt.commandWarm}, tt.chain: {Warm: tt.chainWarm}}}
			if got := useWarm(in, cfg, route.Result{Image: image, Toolchain: tt.chain}); got != tt.want {
				t.Fatalf("warm = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestWarmCall(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, tty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", goos, tty), func(t *testing.T) {
				e, f, _, stderr := testEnv(t)
				e.GOOS, e.StdinTTY, e.StdoutTTY = goos, tty, tty
				f.volumeExists, f.code, f.group = false, 7, "987"
				e.Environ = []string{"TOKEN=host"}
				cfg := config.Config{Env: map[string]string{"TOKEN": "config"}, IdleTimeout: config.Duration(7 * time.Minute)}
				e.LoadConfig = func(config.Options) (*config.Result, error) { return &config.Result{Config: cfg}, nil }
				in := invocation{Command: []string{"go", "build", "-o", "hello", "."}, Docker: true}
				e.Args = append([]string{"--docker"}, in.Command...)
				image := resolve(cfg, "go", "", nil)
				expected, err := runSpec(context.Background(), e, in, cfg, e.Cwd, image)
				if err != nil {
					t.Fatal(err)
				}
				if code := Main(e); code != 7 {
					t.Fatalf("code = %d, stderr = %s", code, stderr)
				}
				if f.warmCalls != 1 || f.args != nil {
					t.Fatalf("warm=%d cold=%v", f.warmCalls, f.args)
				}
				if !reflect.DeepEqual(f.warmSpec, expected) {
					t.Fatalf("spec = %#v, want %#v", f.warmSpec, expected)
				}
				command := docker.ExecSpec{Cmd: in.Command, Workdir: expected.Workdir, User: expected.User, Env: expected.Env, Interactive: true, TTY: tty}
				if !reflect.DeepEqual(f.execSpec, command) || f.idleTimeout != 7*time.Minute {
					t.Fatalf("exec=%#v timeout=%s", f.execSpec, f.idleTimeout)
				}
				calls := 0
				if goos == "linux" {
					calls = 3
				}
				if f.callsAtWarm != calls {
					t.Fatalf("home preparation calls before warm = %d, want %d", f.callsAtWarm, calls)
				}
				if goos == "linux" {
					want := [][]string{
						{"volume", "inspect", "dx-home"},
						{"volume", "create", "--label", "dx=1", "dx-home"},
						{"run", "--rm", "--user", "0:0", "--mount", "type=volume,src=dx-home,dst=/h", image.Image, "/bin/sh", "-c", `owner=$1; shift; mkdir -p "$@" && chown "$owner" "$@"`, "dx-home", "1000:1001", "/h", "/h/.cargo", "/h/.config", "/h/.docker"},
					}
					if !reflect.DeepEqual(f.calls, want) {
						t.Fatalf("home calls = %v", f.calls)
					}
				}
			})
		}
	}
}

func TestWarmErrorsAndColdOverrides(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		args                  []string
		preparation, runError bool
		code                  int
	}{
		{name: "forced cold", args: []string{"--cold", "go", "version"}},
		{name: "ports cold", args: []string{"--port=8080:80", "go", "version"}},
		{name: "custom cold", args: []string{"--image=golang:1.25", "go", "version"}},
		{name: "preparation error", args: []string{"go", "version"}, preparation: true, code: 125},
		{name: "warm error", args: []string{"go", "version"}, runError: true, code: 125},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			e.Args = tt.args
			if tt.preparation {
				f.volumeExists = false
				f.outputErr = errors.New("docker volume create failed")
			}
			if tt.runError {
				f.runErr = errors.New("docker exec failed")
			}
			if code := Main(e); code != tt.code {
				t.Fatalf("code = %d, stderr = %s", code, stderr)
			}
			if tt.preparation {
				if f.warmCalls != 0 || f.args != nil {
					t.Fatal("started despite preparation failure")
				}
			} else if tt.runError {
				if f.warmCalls != 1 || !strings.Contains(stderr.String(), "dx: docker exec failed") {
					t.Fatalf("warm=%d stderr=%s", f.warmCalls, stderr)
				}
			} else if f.warmCalls != 0 || f.args == nil {
				t.Fatalf("warm=%d cold=%v", f.warmCalls, f.args)
			}
		})
	}
}

func TestWarmSubcommands(t *testing.T) {
	for _, tt := range []struct {
		name    string
		args    []string
		empty   bool
		want    string
		stopped []string
		bad     bool
	}{
		{name: "ps", args: []string{"ps"}, want: "NAME"},
		{name: "ps empty", args: []string{"ps"}, empty: true, want: "no warm containers\n"},
		{name: "stop project", args: []string{"stop"}, want: "dx-warm-a\ndx-warm-b\n", stopped: []string{"dx-warm-a", "dx-warm-b"}},
		{name: "stop all", args: []string{"stop", "--all"}, want: "dx-warm-a\ndx-warm-b\ndx-warm-c\n", stopped: []string{"dx-warm-a", "dx-warm-b", "dx-warm-c"}},
		{name: "stop empty", args: []string{"stop"}, empty: true, want: "no warm containers to stop\n"},
		{name: "stop all empty", args: []string{"stop", "--all"}, empty: true, want: "no warm containers to stop\n"},
		{name: "ps bad", args: []string{"ps", "--all"}, bad: true, want: "dx: usage: dx ps"},
		{name: "stop bad", args: []string{"stop", "other"}, bad: true, want: "dx: usage: dx stop [--all]"},
		{name: "stop repeated", args: []string{"stop", "--all", "--all"}, bad: true, want: "dx: usage: dx stop [--all]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			root := e.Cwd
			put(t, filepath.Join(root, "go.mod"), "module example.com/test\n")
			e.Cwd = filepath.Join(root, "nested")
			put(t, filepath.Join(e.Cwd, ".dx.yaml"), "docker: true\n")
			e.Args = tt.args
			if !tt.empty {
				f.containers = []docker.WarmContainer{
					{Name: "dx-warm-a", Toolchain: "go", Root: root, Image: "dx-go:1.25", Status: "Up 2 minutes"},
					{Name: "dx-warm-b", Toolchain: "node", Root: root, Image: "dx-node:22", Status: "Up 1 minute"},
					{Name: "dx-warm-c", Toolchain: "go", Root: root + "-other", Image: "dx-go:1.25", Status: "Up 3 minutes"},
				}
			}
			if len(tt.args) == 2 && tt.args[1] == "--all" {
				e.FindRoot = func(string, string) (string, error) { t.Fatal("--all searched root"); return "", nil }
			}
			code := Main(e)
			wantCode := 0
			if tt.bad {
				wantCode = 125
			}
			if code != wantCode || !strings.Contains(stdout.String()+stderr.String(), tt.want) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			if !reflect.DeepEqual(f.stopped, tt.stopped) {
				t.Fatalf("stopped = %v, want %v", f.stopped, tt.stopped)
			}
			if tt.name == "ps" {
				lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
				if len(lines) != 4 || !reflect.DeepEqual(strings.Fields(lines[0]), []string{"NAME", "TOOLCHAIN", "ROOT", "IMAGE", "STATUS"}) {
					t.Fatalf("table = %s", stdout)
				}
				for i, c := range f.containers {
					if !reflect.DeepEqual(strings.Fields(lines[i+1]), strings.Fields(c.Name+" "+c.Toolchain+" "+c.Root+" "+c.Image+" "+c.Status)) {
						t.Fatalf("row = %s", lines[i+1])
					}
				}
			}
			if tt.bad && f.listCalls != 0 {
				t.Fatal("invalid arguments called docker")
			}
		})
	}
}

func TestWarmSubcommandErrors(t *testing.T) {
	for _, name := range []string{"ps list", "stop list", "stop failure", "stop root", "no matching root"} {
		t.Run(name, func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			e.Args = []string{"stop"}
			failure := errors.New("injected docker failure")
			f.containers = []docker.WarmContainer{{Name: "dx-warm-a", Root: e.Cwd}}
			switch name {
			case "ps list":
				e.Args = []string{"ps"}
				f.listErr = failure
			case "stop list":
				f.listErr = failure
			case "stop failure":
				f.stopErr = failure
			case "stop root":
				e.FindRoot = func(string, string) (string, error) { return "", failure }
			case "no matching root":
				f.containers[0].Root += "-other"
			}
			code := Main(e)
			if name == "no matching root" {
				if code != 0 || stdout.String() != "no warm containers to stop\n" || len(f.stopped) != 0 {
					t.Fatalf("code=%d stdout=%s stopped=%v", code, stdout, f.stopped)
				}
			} else if code != 125 || !strings.Contains(stderr.String(), failure.Error()) || stdout.Len() != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
		})
	}
}

func TestDoctorWarmCount(t *testing.T) {
	for _, tt := range []struct {
		count int
		err   error
		want  string
	}{
		{want: "warm containers: 0\n"},
		{count: 2, want: "warm containers: 2\n"},
		{err: errors.New("docker ps failed"), want: "warm containers: docker ps failed\n"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			e.Args = []string{"doctor"}
			f.containers, f.listErr = make([]docker.WarmContainer, tt.count), tt.err
			if code := Main(e); code != 0 || !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
		})
	}
}
