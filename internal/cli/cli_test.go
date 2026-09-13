package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/docker"
	"github.com/joshualawson/dx/internal/project"
	"github.com/joshualawson/dx/internal/trust"
	"gopkg.in/yaml.v3"
)

type fakeDocker struct {
	calls                                                [][]string
	args, env                                            []string
	code                                                 int
	runErr, outputErr, networkErr, socketErr, contextErr error
	volumeExists, network                                bool
	networkCalls, socketCalls                            int
	cachePath                                            string
	group                                                string
	warmSpec                                             docker.RunSpec
	execSpec                                             docker.ExecSpec
	idleTimeout                                          time.Duration
	warmCalls, callsAtWarm, listCalls                    int
	containers                                           []docker.WarmContainer
	stopped                                              []string
	listErr, stopErr                                     error
}

func (f *fakeDocker) Run(ctx context.Context, args, env []string) (int, error) {
	if ctx.Done() != nil {
		return 0, errors.New("tool context must not be cancellable")
	}
	f.args, f.env = args, env
	return f.code, f.runErr
}
func (f *fakeDocker) RunWarm(ctx context.Context, spec docker.RunSpec, command docker.ExecSpec, idleTimeout time.Duration) (int, error) {
	if ctx.Done() != nil {
		return 0, errors.New("tool context must not be cancellable")
	}
	f.warmCalls++
	f.callsAtWarm = len(f.calls)
	f.warmSpec, f.execSpec, f.idleTimeout = spec, command, idleTimeout
	return f.code, f.runErr
}
func (f *fakeDocker) ListWarm(context.Context) ([]docker.WarmContainer, error) {
	f.listCalls++
	return f.containers, f.listErr
}
func (f *fakeDocker) StopWarm(_ context.Context, names ...string) error {
	f.stopped = append(f.stopped, names...)
	return f.stopErr
}
func (f *fakeDocker) Output(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if reflect.DeepEqual(args, []string{"volume", "inspect", "dx-home"}) && !f.volumeExists {
		return "", errors.New("volume does not exist")
	}
	return "", f.outputErr
}
func (f *fakeDocker) ContextName(context.Context) (string, error) {
	return "fake-context", f.contextErr
}
func (f *fakeDocker) HostNetworkWorks(_ context.Context, p string) (bool, error) {
	f.networkCalls++
	f.cachePath = p
	return f.network, f.networkErr
}
func (f *fakeDocker) SocketMount(context.Context, string, func(string) string) (docker.Mount, string, error) {
	f.socketCalls++
	return docker.Mount{Type: docker.Bind, Source: "/host/docker.sock", Target: "/var/run/docker.sock"}, f.group, f.socketErr
}

func testEnv(t *testing.T) (Env, *fakeDocker, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	cwd := filepath.Join(home, "src")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	f := &fakeDocker{volumeExists: true, network: true}
	state := filepath.Join(base, "state")
	vars := map[string]string{"XDG_STATE_HOME": state, "XDG_CONFIG_HOME": filepath.Join(base, "config")}
	e := Env{
		Cwd: cwd, Home: home, GOOS: "linux", GOARCH: "amd64", Username: "me", UID: 1000, GID: 1001,
		Getenv: func(k string) string { return vars[k] }, Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr,
		Exists:   func(p string) bool { _, err := os.Stat(p); return err == nil },
		StateDir: filepath.Join(state, "dx"), Docker: f, DockerBin: "/usr/bin/docker", Version: "1.2.3",
		LoadConfig: config.Load, FindRoot: project.FindRoot, PresentMarkers: project.PresentMarkers,
		ReadFile: os.ReadFile, WriteFile: os.WriteFile, MkdirAll: os.MkdirAll, ForgetHostNetwork: docker.ForgetHostNetwork,
	}
	return e, f, stdout, stderr
}

func put(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTrustFlow(t *testing.T) {
	tests := []struct {
		name, answer        string
		stdinTTY, stderrTTY bool
		code                int
		approved            bool
	}{
		{name: "nonterminal", answer: "y\n", code: 125},
		{name: "stderr pipe", answer: "y\n", stdinTTY: true, code: 125},
		{name: "stdin pipe", answer: "y\n", stderrTTY: true, code: 125},
		{name: "approve", answer: "y\n", stdinTTY: true, stderrTTY: true, approved: true},
		{name: "approve yes", answer: "YES\n", stdinTTY: true, stderrTTY: true, approved: true},
		{name: "deny", answer: "n\n", stdinTTY: true, stderrTTY: true, code: 125},
		{name: "default deny", answer: "\n", stdinTTY: true, stderrTTY: true, code: 125},
		{name: "eof deny", stdinTTY: true, stderrTTY: true, code: 125},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			filename := filepath.Join(e.Cwd, ".dx.yaml")
			content := "tools: {go: {image: golang:1.25}}\n"
			put(t, filename, content)
			e.Args = []string{"go", "version"}
			e.Stdin = strings.NewReader(tt.answer)
			e.StdinTTY, e.StderrTTY = tt.stdinTTY, tt.stderrTTY
			if code := Main(e); code != tt.code {
				t.Fatalf("code = %d, stderr = %s", code, stderr)
			}
			store := &trust.Store{Path: trust.DefaultPath(e.GOOS, e.Home, e.Getenv)}
			ok, err := store.IsTrusted(filename, []byte(content), map[string]any{"docker": true}, nil)
			if err != nil || ok != tt.approved {
				t.Fatalf("trusted = %t, %v", ok, err)
			}
			if tt.approved && !containsSequence(f.args, []string{"golang:1.25", "go", "version"}) {
				t.Fatalf("run args = %v", f.args)
			}
			if !tt.approved && f.args != nil {
				t.Fatal("ran an untrusted tool")
			}
			if tt.stdinTTY && tt.stderrTTY {
				for _, text := range []string{filename, "tools.go.image = golang:1.25", "Trust this file? [y/N]"} {
					if !strings.Contains(stderr.String(), text) {
						t.Fatalf("missing %q in %s", text, stderr)
					}
				}
			} else {
				want := fmt.Sprintf("dx: %s is not trusted; review it and run: dx trust %s\n", filename, filename)
				if stderr.String() != want {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestTrustCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		terminal bool
		answer   string
		code     int
	}{
		{name: "explicit yes", args: []string{"trust", "--yes", ".dx.yaml"}},
		{name: "discover yes", args: []string{"trust", "--yes"}},
		{name: "interactive", args: []string{"trust"}, terminal: true, answer: "y\ny\n"},
		{name: "deny", args: []string{"trust", ".dx.yaml"}, terminal: true, answer: "n\n", code: 125},
		{name: "requires terminal", args: []string{"trust", ".dx.yaml"}, code: 125},
		{name: "unknown", args: []string{"trust", "--wat"}, code: 125},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _, stderr := testEnv(t)
			parent := e.Cwd
			e.Cwd = filepath.Join(parent, "nested")
			put(t, filepath.Join(parent, ".dx.yaml"), "docker: true\n")
			put(t, filepath.Join(e.Cwd, ".dx.yaml"), "env: {TEST: value}\n")
			e.Args, e.Stdin = tt.args, strings.NewReader(tt.answer)
			e.StdinTTY, e.StderrTTY = tt.terminal, tt.terminal
			if code := Main(e); code != tt.code {
				t.Fatalf("code = %d, stderr = %s", code, stderr)
			}
			if tt.code == 0 {
				e.Args = []string{"trust", "--yes"}
				if code := Main(e); code != 0 {
					t.Fatalf("approve remaining: %d: %s", code, stderr)
				}
				if _, err := config.Load(options(e, &trust.Store{Path: trust.DefaultPath(e.GOOS, e.Home, e.Getenv)})); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCommandsAndErrors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		toolCode int
		runErr   error
		code     int
		text     string
	}{
		{name: "version", args: []string{"version"}, text: "dx 1.2.3\n"},
		{name: "version flag", args: []string{"--version"}, text: "dx 1.2.3\n"},
		{name: "help", args: []string{"help"}, text: "Subcommands are special only without preceding dx flags."},
		{name: "exit seven", args: []string{"--image", "busybox:1.37", "sh", "-c", "exit 7"}, toolCode: 7, code: 7},
		{name: "exit 125", args: []string{"go"}, toolCode: 125, code: 125},
		{name: "start error", args: []string{"go"}, runErr: errors.New("start docker: unavailable"), code: 125, text: "dx: start docker: unavailable"},
		{name: "unknown flag", args: []string{"--unknown"}, code: 125, text: "dx: unknown flag"},
		{name: "version arguments", args: []string{"version", "extra"}, code: 125, text: "dx: version does not accept arguments"},
		{name: "config arguments", args: []string{"config", "extra"}, code: 125, text: "dx: usage: dx config"},
		{name: "doctor arguments", args: []string{"doctor", "extra"}, code: 125, text: "dx: usage: dx doctor"},
		{name: "outside mount", args: []string{"--mount", "../other", "go"}, code: 125, text: "is outside the mounted folder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			e.Args, f.code, f.runErr = tt.args, tt.toolCode, tt.runErr
			code := Main(e)
			if code != tt.code {
				t.Fatalf("code = %d, want %d, stderr = %s", code, tt.code, stderr)
			}
			if !strings.Contains(stdout.String()+stderr.String(), tt.text) {
				t.Fatalf("output = %s%s, want %s", stdout, stderr, tt.text)
			}
			if tt.toolCode != 0 && stderr.Len() != 0 {
				t.Fatalf("tool exit emitted dx error: %s", stderr)
			}
		})
	}
}

func TestConfigOutput(t *testing.T) {
	for _, explain := range []bool{false, true} {
		t.Run(fmt.Sprint(explain), func(t *testing.T) {
			e, _, stdout, stderr := testEnv(t)
			filename := filepath.Join(e.Cwd, ".dx.yaml")
			put(t, filename, "tools: {go: {version: '1.25'}}\n")
			e.Args = []string{"config"}
			if explain {
				e.Args = append(e.Args, "--explain")
			}
			if code := Main(e); code != 0 {
				t.Fatalf("code = %d, stderr = %s", code, stderr)
			}
			if !explain {
				var cfg config.Config
				if err := yaml.Unmarshal(stdout.Bytes(), &cfg); err != nil {
					t.Fatal(err)
				}
				if cfg.Tools["go"].Version != "1.25" {
					t.Fatalf("config = %#v", cfg)
				}
				return
			}
			lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			var keys []string
			found := false
			for _, line := range lines[1:] {
				fields := strings.Fields(line)
				keys = append(keys, fields[0])
				if fields[0] == "tools.go.version" {
					found = true
					if len(fields) != 3 || fields[1] != `"1.25"` || fields[2] != filename {
						t.Fatalf("explain line = %q", line)
					}
				}
			}
			if !found || !sort.StringsAreSorted(keys) {
				t.Fatalf("explain = %s", stdout)
			}
		})
	}
}

func TestDoctor(t *testing.T) {
	for _, untrusted := range []bool{false, true} {
		t.Run(fmt.Sprint(untrusted), func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			filename := filepath.Join(e.Cwd, ".dx.yaml")
			if untrusted {
				put(t, filename, "tools: {go: {image: golang:1.25}}\n")
			}
			put(t, filepath.Join(e.Cwd, "go.mod"), "module example.com/test\n")
			rechecked := false
			e.ForgetHostNetwork = func(p string) error {
				if p != filepath.Join(e.StateDir, "network.json") {
					t.Fatalf("cache = %q", p)
				}
				rechecked = true
				return nil
			}
			e.Args = []string{"doctor", "--recheck"}
			if code := Main(e); code != 0 {
				t.Fatalf("code = %d: %s", code, stderr)
			}
			for _, text := range []string{"docker binary: /usr/bin/docker", "docker context: fake-context", "host networking: true", "home: " + e.Home, "state dir: " + e.StateDir, "mount root: " + e.Cwd, "markers: go.mod", "config sources:", "dx go image:"} {
				if !strings.Contains(stdout.String(), text) {
					t.Fatalf("missing %q in %s", text, stdout)
				}
			}
			if untrusted && (!strings.Contains(stdout.String(), "untrusted: "+filename) || !strings.Contains(stdout.String(), "golang:1.25")) {
				t.Fatalf("output = %s", stdout)
			}
			if !rechecked || f.networkCalls != 1 || f.args != nil || len(f.calls) != 0 {
				t.Fatalf("unexpected docker calls: %#v", f)
			}
			if e.Exists(trust.DefaultPath(e.GOOS, e.Home, e.Getenv)) {
				t.Fatal("doctor approved config")
			}
		})
	}
}

func containsSequence(args, sequence []string) bool {
	for i := 0; i+len(sequence) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(sequence)], sequence) {
			return true
		}
	}
	return false
}
