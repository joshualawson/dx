package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func requireLoopback(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback networking unavailable: %v", err)
	}
	_ = listener.Close()
}

func writeFixture(t *testing.T, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loggedCommands(t *testing.T, log string) [][]string {
	t.Helper()
	file, err := os.Open(log)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var commands [][]string
	decoder := json.NewDecoder(file)
	for {
		var args []string
		err := decoder.Decode(&args)
		if errors.Is(err, io.EOF) {
			return commands
		}
		if err != nil {
			t.Fatal(err)
		}
		commands = append(commands, args)
	}
}

func readCache(t *testing.T, name string) map[string]networkCheck {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var cache map[string]networkCheck
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestHostNetworkCached(t *testing.T) {
	for _, works := range []bool{false, true} {
		t.Run(map[bool]string{false: "false", true: "true"}[works], func(t *testing.T) {
			r := fakeRunner(t)
			dir := t.TempDir()
			cachePath, log := filepath.Join(dir, "cache.json"), filepath.Join(dir, "commands.jsonl")
			t.Setenv("DX_DOCKER_TEST_LOG", log)
			old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := writeNetworkCache(cachePath, map[string]networkCheck{"test-context": {HostNetwork: works, CheckedAt: old}}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(cachePath)
			got, err := r.HostNetworkWorks(context.Background(), cachePath)
			if err != nil || got != works {
				t.Fatalf("HostNetworkWorks() = %v, %v, want %v", got, err, works)
			}
			if got := loggedCommands(t, log); !reflect.DeepEqual(got, [][]string{{"context", "show"}}) {
				t.Fatalf("cached result ran extra commands: %#v", got)
			}
			after, _ := os.ReadFile(cachePath)
			if string(after) != string(before) {
				t.Fatal("cached result rewrote cache")
			}
		})
	}
}

func TestHostNetworkProbeCache(t *testing.T) {
	requireLoopback(t)
	for _, tt := range []struct {
		name    string
		initial string
		probe   string
		want    bool
	}{
		{"missing", "", "", true},
		{"corrupt", "{broken", "", true},
		{"corrupt time", `{"test-context":{"host_network":false,"checked_at":"invalid"}}`, "", true},
		{"null", "null", "", true},
		{"other context", `{"other":{"host_network":false,"checked_at":"2000-01-01T00:00:00Z"}}`, "", true},
		{"container failure", "", "fail", false},
		{"container exit 1", "", "1", false},
		{"container exit 2", "", "2", false},
		{"container exit 126", "", "126", false},
		{"container exit 127", "", "127", false},
		{"container exit 255", "", "255", false},
		{"wrong body", "", "empty", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeRunner(t)
			dir := t.TempDir()
			cachePath, log := filepath.Join(dir, "nested", "cache.json"), filepath.Join(dir, "commands.jsonl")
			t.Setenv("DX_DOCKER_TEST_LOG", log)
			t.Setenv("DX_DOCKER_TEST_PROBE", tt.probe)
			if tt.initial != "" {
				writeFixture(t, cachePath, tt.initial)
			}
			start := time.Now().Add(-time.Second)
			for range 2 {
				got, err := r.HostNetworkWorks(context.Background(), cachePath)
				if err != nil || got != tt.want {
					t.Fatalf("HostNetworkWorks() = %v, %v, want %v", got, err, tt.want)
				}
			}
			cache := readCache(t, cachePath)
			check, ok := cache["test-context"]
			if !ok || check.HostNetwork != tt.want || check.CheckedAt.Before(start) || check.CheckedAt.After(time.Now()) {
				t.Fatalf("unexpected cached check: %+v", check)
			}
			if tt.name == "other context" {
				if _, ok := cache["other"]; !ok {
					t.Fatal("lost other context's answer")
				}
			}
			commands := loggedCommands(t, log)
			if len(commands) != 3 || commands[0][0] != "context" || commands[1][0] != "run" || commands[2][0] != "context" {
				t.Fatalf("expected one probe across two calls: %#v", commands)
			}
		})
	}
}

func TestHostNetworkDockerFailureNotCached(t *testing.T) {
	requireLoopback(t)
	for _, tt := range []struct {
		name    string
		initial string
	}{
		{"missing", ""},
		{"other context", `{"other":{"host_network":true,"checked_at":"2000-01-01T00:00:00Z"}}`},
		{"corrupt", "{broken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeRunner(t)
			cachePath := filepath.Join(t.TempDir(), "cache.json")
			if tt.initial != "" {
				writeFixture(t, cachePath, tt.initial)
			}
			t.Setenv("DX_DOCKER_TEST_PROBE", "125")
			works, err := r.HostNetworkWorks(context.Background(), cachePath)
			var exitErr *exec.ExitError
			if works || !errors.As(err, &exitErr) || exitErr.ExitCode() != 125 || !strings.Contains(err.Error(), "probe command failed") {
				t.Fatalf("HostNetworkWorks() = %v, %v, want docker exit 125 error", works, err)
			}
			data, err := os.ReadFile(cachePath)
			if tt.initial == "" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("docker failure created a cache: %q, %v", data, err)
				}
			} else if err != nil || string(data) != tt.initial {
				t.Fatalf("docker failure changed the cache: %q, %v", data, err)
			}
			t.Setenv("DX_DOCKER_TEST_PROBE", "")
			works, err = r.HostNetworkWorks(context.Background(), cachePath)
			if err != nil || !works {
				t.Fatalf("retry = %v, %v, want true, nil", works, err)
			}
			if cache := readCache(t, cachePath); !cache["test-context"].HostNetwork {
				t.Fatalf("successful retry was not cached: %+v", cache)
			}
		})
	}
}

func TestForgetHostNetwork(t *testing.T) {
	for _, name := range []string{"existing", "corrupt", "missing", "missing parent", "remove error"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cachePath := filepath.Join(dir, "cache.json")
			unrelated := filepath.Join(dir, "unrelated.json")
			writeFixture(t, unrelated, "keep")
			switch name {
			case "existing":
				writeFixture(t, cachePath, `{"local":{"host_network":false,"checked_at":"2000-01-01T00:00:00Z"}}`)
			case "corrupt":
				writeFixture(t, cachePath, "{broken")
			case "missing parent":
				cachePath = filepath.Join(dir, "missing", "cache.json")
			case "remove error":
				writeFixture(t, filepath.Join(cachePath, "child"), "keep")
			}
			for range 2 {
				err := ForgetHostNetwork(cachePath)
				if name == "remove error" {
					var pathErr *os.PathError
					if !errors.As(err, &pathErr) || !strings.Contains(err.Error(), cachePath) {
						t.Fatalf("ForgetHostNetwork() = %v, want wrapped file error", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("cache still exists: %v", err)
					}
				}
			}
			if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
				t.Fatalf("unrelated file changed: %q, %v", data, err)
			}
		})
	}
}

func TestForgetHostNetworkRecheck(t *testing.T) {
	requireLoopback(t)
	r := fakeRunner(t)
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	t.Setenv("DX_DOCKER_TEST_PROBE", "fail")
	if works, err := r.HostNetworkWorks(context.Background(), cachePath); err != nil || works {
		t.Fatalf("initial check = %v, %v, want false, nil", works, err)
	}
	t.Setenv("DX_DOCKER_TEST_PROBE", "")
	if works, err := r.HostNetworkWorks(context.Background(), cachePath); err != nil || works {
		t.Fatalf("cached check = %v, %v, want false, nil", works, err)
	}
	if err := ForgetHostNetwork(cachePath); err != nil {
		t.Fatal(err)
	}
	if works, err := r.HostNetworkWorks(context.Background(), cachePath); err != nil || !works {
		t.Fatalf("recheck = %v, %v, want true, nil", works, err)
	}
}

func TestHostNetworkContextIsolation(t *testing.T) {
	requireLoopback(t)
	r := fakeRunner(t)
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	for _, tt := range []struct {
		name  string
		probe string
		want  bool
	}{
		{"local", "", true}, {"remote", "fail", false}, {"local", "fail", true}, {"", "", true},
	} {
		t.Setenv("DX_DOCKER_TEST_CONTEXT", tt.name)
		t.Setenv("DX_DOCKER_TEST_PROBE", tt.probe)
		got, err := r.HostNetworkWorks(context.Background(), cachePath)
		if err != nil || got != tt.want {
			t.Fatalf("context %q = %v, %v, want %v", tt.name, got, err, tt.want)
		}
	}
	if cache := readCache(t, cachePath); len(cache) != 3 || !cache["default"].HostNetwork {
		t.Fatalf("unexpected cache: %+v", cache)
	}
}

func TestHostNetworkErrors(t *testing.T) {
	for _, name := range []string{"context", "cache read", "cache directory", "cancelled", "missing binary"} {
		t.Run(name, func(t *testing.T) {
			r := fakeRunner(t)
			cachePath := filepath.Join(t.TempDir(), "cache.json")
			ctx := context.Background()
			want := ""
			switch name {
			case "context":
				t.Setenv("DX_DOCKER_TEST_CONTEXT_FAIL", "1")
				want = "context unavailable"
			case "cache read":
				if err := os.Mkdir(cachePath, 0o700); err != nil {
					t.Fatal(err)
				}
				want = cachePath
			case "cache directory":
				writeFixture(t, cachePath, "not a directory")
				cachePath = filepath.Join(cachePath, "child.json")
				want = cachePath
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = "context canceled"
			case "missing binary":
				r.Bin = filepath.Join(t.TempDir(), "missing-docker")
				want = "docker must be installed"
			}
			_, err := r.HostNetworkWorks(ctx, cachePath)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want containing %q", err, want)
			}
		})
	}
}

func TestHostNetworkProbeCancellation(t *testing.T) {
	requireLoopback(t)
	r := fakeRunner(t)
	t.Setenv("DX_DOCKER_TEST_PROBE", "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	_, err := r.HostNetworkWorks(ctx, cachePath)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled probe cached an answer: %v", err)
	}
}

func TestWriteNetworkCacheErrors(t *testing.T) {
	for _, name := range []string{"parent is file", "target is directory"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			cachePath := filepath.Join(root, "cache.json")
			if name == "parent is file" {
				writeFixture(t, cachePath, "file")
				cachePath = filepath.Join(cachePath, "cache.json")
			} else if err := os.Mkdir(cachePath, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := writeNetworkCache(cachePath, map[string]networkCheck{}); err == nil || !strings.Contains(err.Error(), root) {
				t.Fatalf("writeNetworkCache() = %v", err)
			}
			leftovers, err := filepath.Glob(filepath.Join(root, ".dx-network-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files remain: %v, %v", leftovers, err)
			}
		})
	}
}

func TestHostNetworkRealDocker(t *testing.T) {
	if testing.Short() {
		t.Skip("real Docker probe disabled in short mode")
	}
	bin, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not on PATH")
	}
	requireLoopback(t)
	r := &Runner{Bin: bin}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err = r.Output(ctx, "info", "--format", "{{.ServerVersion}}")
	cancel()
	if err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	cachePath := filepath.Join(t.TempDir(), "cache.json")
	works, err := r.HostNetworkWorks(context.Background(), cachePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("host networking available: %v", works)
	if cache := readCache(t, cachePath); len(cache) != 1 {
		t.Fatalf("unexpected cache: %+v", cache)
	}
}
