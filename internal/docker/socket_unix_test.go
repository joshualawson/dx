//go:build unix

package docker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestSocketMountLinux(t *testing.T) {
	for _, source := range []string{"environment", "context"} {
		t.Run(source, func(t *testing.T) {
			r := fakeRunner(t)
			dir := t.TempDir()
			socket := filepath.Join(dir, "docker.sock")
			writeFixture(t, socket, "")
			info, err := os.Stat(socket)
			if err != nil {
				t.Fatal(err)
			}
			wantGroup := strconv.FormatUint(uint64(info.Sys().(*syscall.Stat_t).Gid), 10)
			var dockerHost string
			if source == "environment" {
				dockerHost = "unix://" + socket
				r.Bin = filepath.Join(dir, "must-not-run")
			} else {
				t.Setenv("DX_DOCKER_TEST_SOCKET", "unix://"+socket)
			}
			mount, group, err := r.SocketMount(context.Background(), "linux", func(string) string { return dockerHost })
			want := Mount{Type: Bind, Source: socket, Target: containerSocket}
			if err != nil || mount != want || group != wantGroup {
				t.Fatalf("SocketMount() = %+v, %q, %v, want %+v, %q", mount, group, err, want, wantGroup)
			}
		})
	}
}

func TestSocketMountMissingSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "missing.sock")
	r := &Runner{}
	_, _, err := r.SocketMount(context.Background(), "linux", func(string) string { return "unix://" + socket })
	if err == nil || !strings.Contains(err.Error(), socket) {
		t.Fatalf("SocketMount() error = %v", err)
	}
}
