package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketSource(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "plan9"} {
		for _, tt := range []struct {
			name string
			host string
			want string
		}{
			{"unix", "unix:///run/user/1000/docker.sock", "/run/user/1000/docker.sock"},
			{"unix spaces", "unix:///home/a b/docker.sock", "/home/a b/docker.sock"},
			{"linux desktop", "unix:///home/me/.docker/desktop/docker.sock", containerSocket},
			{"desktop substring only", "unix:///home/me/.docker/desktop-other/docker.sock", "/home/me/.docker/desktop-other/docker.sock"},
			{"relative desktop", "unix://relative/.docker/desktop/docker.sock", ""},
			{"tcp", "tcp://127.0.0.1:2375", ""},
			{"ssh", "ssh://remote", ""},
			{"unset", "", ""},
			{"empty socket", "unix://", ""},
			{"relative socket", "unix://relative.sock", ""},
			{"npipe", "npipe:////./pipe/docker_engine", ""},
		} {
			t.Run(goos+"/"+tt.name, func(t *testing.T) {
				want := tt.want
				if goos == "darwin" || goos == "windows" {
					want = containerSocket
				} else if goos == "plan9" {
					want = ""
				}
				got, err := socketSource(goos, tt.host)
				if want == "" {
					if err == nil {
						t.Fatalf("socketSource() = %q, expected error", got)
					}
					return
				}
				if err != nil || got != want {
					t.Fatalf("socketSource() = %q, %v, want %q", got, err, want)
				}
			})
		}
	}
}

func TestSocketMountVM(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			r := &Runner{Bin: filepath.Join(t.TempDir(), "must-not-run")}
			getenv := func(string) string {
				t.Fatal("VM socket must not read the host environment")
				return ""
			}
			got, group, err := r.SocketMount(context.Background(), goos, getenv)
			want := Mount{Type: Bind, Source: containerSocket, Target: containerSocket}
			if err != nil || got != want || group != "" {
				t.Fatalf("SocketMount() = %+v, %q, %v, want %+v, empty group", got, group, err, want)
			}
		})
	}
}

func TestSocketMountLinuxDesktop(t *testing.T) {
	for _, source := range []string{"environment", "context"} {
		t.Run(source, func(t *testing.T) {
			r := fakeRunner(t)
			host := "unix:///home/me/.docker/desktop/docker.sock"
			var dockerHost string
			if source == "environment" {
				dockerHost = host
				r.Bin = filepath.Join(t.TempDir(), "must-not-run")
			} else {
				t.Setenv("DX_DOCKER_TEST_SOCKET", host)
			}
			mount, group, err := r.SocketMount(context.Background(), "linux", func(string) string { return dockerHost })
			want := Mount{Type: Bind, Source: containerSocket, Target: containerSocket}
			if err != nil || mount != want || group != "" {
				t.Fatalf("SocketMount() = %+v, %q, %v, want %+v, empty group", mount, group, err, want)
			}
		})
	}
}

func TestSocketMountRemote(t *testing.T) {
	for _, tt := range []struct {
		name        string
		dockerHost  string
		contextHost string
		wantHost    string
	}{
		{"tcp context", "", "tcp://remote:2375", "tcp://remote:2375"},
		{"ssh context", "", "ssh://remote", "ssh://remote"},
		{"tcp environment", "tcp://remote:2375", "unix:///var/run/docker.sock", "tcp://remote:2375"},
		{"ssh environment", "ssh://remote", "unix:///var/run/docker.sock", "ssh://remote"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeRunner(t)
			log := filepath.Join(t.TempDir(), "commands.jsonl")
			t.Setenv("DX_DOCKER_TEST_LOG", log)
			t.Setenv("DX_DOCKER_TEST_SOCKET", tt.contextHost)
			if tt.dockerHost != "" {
				r.Bin = filepath.Join(t.TempDir(), "must-not-run")
			}
			_, _, err := r.SocketMount(context.Background(), "linux", func(key string) string {
				if key != "DOCKER_HOST" {
					t.Fatalf("unexpected environment key: %q", key)
				}
				return tt.dockerHost
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantHost) || !strings.Contains(err.Error(), "local unix:// socket") {
				t.Fatalf("SocketMount() error = %v", err)
			}
			if tt.dockerHost != "" {
				if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("explicit remote host invoked docker: %v", err)
				}
			} else {
				commands := loggedCommands(t, log)
				if len(commands) != 1 || len(commands[0]) != 4 || commands[0][1] != "inspect" {
					t.Fatalf("expected context inspect: %#v", commands)
				}
			}
		})
	}
}

func TestSocketMountContextFailure(t *testing.T) {
	r := &Runner{Bin: filepath.Join(t.TempDir(), "missing-docker")}
	if _, _, err := r.SocketMount(context.Background(), "linux", nil); err == nil || !strings.Contains(err.Error(), "docker must be installed") {
		t.Fatalf("SocketMount() error = %v", err)
	}
}
