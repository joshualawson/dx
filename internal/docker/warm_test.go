package docker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeStep struct {
	Args   []string
	Stdout string
	Stderr string
	Code   int
	Mode   string
	Env    map[string]string
}

type fakePlan struct {
	Position int
	Steps    []fakeStep
}

func fakeScript(file string, args []string) int {
	data, err := os.ReadFile(file)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 99
	}
	var plan fakePlan
	if err := json.Unmarshal(data, &plan); err != nil || plan.Position >= len(plan.Steps) {
		_, _ = fmt.Fprintln(os.Stderr, "unexpected docker call", args)
		return 99
	}
	step := plan.Steps[plan.Position]
	if len(step.Args) != len(args) {
		_, _ = fmt.Fprintf(os.Stderr, "docker args = %#v, want %#v\n", args, step.Args)
		return 99
	}
	for i, want := range step.Args {
		if want != "*" && want != args[i] {
			_, _ = fmt.Fprintf(os.Stderr, "docker args = %#v, want %#v\n", args, step.Args)
			return 99
		}
	}
	for key, want := range step.Env {
		if got := os.Getenv(key); got != want {
			_, _ = fmt.Fprintf(os.Stderr, "docker environment %s = %q, want %q\n", key, got, want)
			return 99
		}
	}
	plan.Position++
	data, _ = json.Marshal(plan)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return 99
	}
	_, _ = fmt.Fprint(os.Stdout, step.Stdout)
	_, _ = fmt.Fprint(os.Stderr, step.Stderr)
	switch step.Mode {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "kill", "kill-delayed":
		if err := os.WriteFile(file+".kill", []byte(strings.Join(args, " ")), 0o600); err != nil {
			return 99
		}
		if step.Mode == "kill-delayed" {
			if !waitFakeFile(file + ".release") {
				return 98
			}
			if err := os.WriteFile(file+".forwarded", nil, 0o600); err != nil {
				return 99
			}
		}
	case "block":
		if err := os.WriteFile(file+".blocked", nil, 0o600); err != nil {
			return 99
		}
		time.Sleep(10 * time.Second)
	case "wait-kill", "wait-kill-exit":
		if step.Mode == "wait-kill-exit" {
			if err := os.WriteFile(file+".exec-pid", []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
				return 99
			}
		}
		if !waitFakeFile(file + ".kill") {
			return 98
		}
	}
	return step.Code
}

func waitFakeFile(file string) bool {
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(file); err == nil {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}

func scriptedRunner(t *testing.T, steps []fakeStep) (*Runner, string) {
	t.Helper()
	r := fakeRunner(t)
	dir := t.TempDir()
	script, log := filepath.Join(dir, "script.json"), filepath.Join(dir, "commands.jsonl")
	data, err := json.Marshal(fakePlan{Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, script, string(data))
	t.Setenv("DX_DOCKER_TEST_SCRIPT", script)
	t.Setenv("DX_DOCKER_TEST_LOG", log)
	t.Cleanup(func() {
		data, err := os.ReadFile(script)
		if err != nil {
			t.Error(err)
			return
		}
		var plan fakePlan
		if err := json.Unmarshal(data, &plan); err != nil || plan.Position != len(steps) {
			t.Errorf("consumed %d of %d scripted docker calls: %v", plan.Position, len(steps), err)
		}
	})
	return r, log
}

func inspectStep(name, state string) fakeStep {
	step := fakeStep{Args: []string{"container", "inspect", "--format", "{{.State.Running}}", name}, Stdout: state}
	if state == "missing" {
		step.Stdout, step.Stderr, step.Code = "", "Error: No such container: "+name, 1
	}
	return step
}

func cleanupStep(ids string) fakeStep {
	return fakeStep{Args: []string{"ps", "-a", "--filter", "label=dx.warm=1", "--filter", "status=exited", "--filter", "status=created", "--filter", "status=dead", "-q"}, Stdout: ids}
}

func createStep(spec RunSpec) fakeStep {
	name := WarmName(spec)
	labels := map[string]string{"dx": "1", "dx.warm": "1", "dx.key": strings.TrimPrefix(name, "dx-warm-")}
	for key, value := range spec.Labels {
		if key != "dx" && key != "dx.warm" && key != "dx.key" {
			labels[key] = value
		}
	}
	spec.Name, spec.Labels = name, labels
	spec.Detach, spec.Remove = true, true
	spec.Interactive, spec.TTY, spec.Init = false, false, false
	spec.Cmd, spec.Env, spec.Ports = nil, nil, nil
	return fakeStep{Args: append(RunArgs(spec), "dx-idle", "--timeout", "30m0s"), Stdout: "container-id\n"}
}

func TestWarmName(t *testing.T) {
	base := RunSpec{Image: "dx-go:1.25", User: "1000:1000", GroupAdd: []string{"123"}, Mounts: []Mount{{Type: Bind, Source: "/src", Target: "/src"}}, Network: "host", Labels: map[string]string{"dx.root": "/src", "dx.toolchain": "go"}}
	for _, tt := range []struct {
		name   string
		change func(*RunSpec)
		want   bool
	}{
		{"image", func(s *RunSpec) { s.Image = "dx-go:1.26" }, true},
		{"user", func(s *RunSpec) { s.User = "2000:2000" }, true},
		{"groups", func(s *RunSpec) { s.GroupAdd = []string{"456"} }, true},
		{"mount", func(s *RunSpec) { s.Mounts = []Mount{{Type: Bind, Source: "/other", Target: "/src"}} }, true},
		{"readonly", func(s *RunSpec) { s.Mounts = []Mount{{Type: Bind, Source: "/src", Target: "/src", ReadOnly: true}} }, true},
		{"network", func(s *RunSpec) { s.Network = "" }, true},
		{"labels", func(s *RunSpec) { s.Labels = map[string]string{"dx.root": "/other"} }, true},
		{"env", func(s *RunSpec) { s.Env = []string{"GH_TOKEN=secret"} }, false},
		{"workdir", func(s *RunSpec) { s.Workdir = "/src/sub" }, false},
		{"command", func(s *RunSpec) { s.Cmd = []string{"go", "test"} }, false},
		{"name", func(s *RunSpec) { s.Name = "ignored" }, false},
		{"ports", func(s *RunSpec) { s.Ports = []string{"8080:80"} }, false},
		{"interactive", func(s *RunSpec) { s.Interactive = true }, false},
		{"tty", func(s *RunSpec) { s.TTY = true }, false},
		{"init", func(s *RunSpec) { s.Init = true }, false},
		{"remove", func(s *RunSpec) { s.Remove = true }, false},
		{"detach", func(s *RunSpec) { s.Detach = true }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := base
			tt.change(&changed)
			if got := WarmName(changed) != WarmName(base); got != tt.want {
				t.Fatalf("name changed = %v, want %v", got, tt.want)
			}
		})
	}
	canonical := `{"Image":"dx-go:1.25","User":"1000:1000","GroupAdd":["123"],"Mounts":[{"Type":"bind","Source":"/src","Target":"/src","ReadOnly":false}],"Network":"host","Labels":{"dx.root":"/src","dx.toolchain":"go"}}`
	hash := sha256.Sum256([]byte(canonical))
	want := fmt.Sprintf("dx-warm-%x", hash[:8])
	for range 20 {
		if got := WarmName(base); got != want {
			t.Fatalf("WarmName() = %q, want %q", got, want)
		}
	}
	if WarmName(RunSpec{}) != WarmName(RunSpec{GroupAdd: []string{}, Mounts: []Mount{}, Labels: map[string]string{}}) {
		t.Fatal("nil and empty collections changed the name")
	}
	for _, spec := range []RunSpec{
		{GroupAdd: []string{"1", "2"}},
		{Mounts: []Mount{{Type: Tmpfs, Target: "/a"}, {Type: Tmpfs, Target: "/b"}}},
	} {
		before := WarmName(spec)
		if len(spec.GroupAdd) != 0 {
			spec.GroupAdd[0], spec.GroupAdd[1] = spec.GroupAdd[1], spec.GroupAdd[0]
		} else {
			spec.Mounts[0], spec.Mounts[1] = spec.Mounts[1], spec.Mounts[0]
		}
		if WarmName(spec) == before {
			t.Fatal("ordered fields were reordered before hashing")
		}
	}
}

func TestEnsureWarm(t *testing.T) {
	spec := RunSpec{Image: "dx-go:1.25", Name: "ignored", Workdir: "/src", User: "1000:1000", GroupAdd: []string{"123"}, Env: []string{"GH_TOKEN=secret"}, Ports: []string{"8080:80"}, Cmd: []string{"go", "test"}, Interactive: true, TTY: true, Init: true, Labels: map[string]string{"dx.root": "/src", "dx": "wrong", "dx.warm": "wrong", "dx.key": "wrong"}}
	name := WarmName(spec)
	create := createStep(spec)
	unavailable := inspectStep(name, "")
	unavailable.Code, unavailable.Stderr = 125, "Cannot connect to the Docker daemon"
	conflict := create
	conflict.Stdout, conflict.Code, conflict.Stderr = "", 125, "Conflict. The container name is already in use"
	badCleanup := cleanupStep("")
	badCleanup.Code, badCleanup.Stderr = 1, "cleanup failed"
	badCreate := create
	badCreate.Stdout, badCreate.Code, badCreate.Stderr = "", 125, "image pull failed"
	badRemove := fakeStep{Args: []string{"rm", "-f", name}, Code: 1, Stderr: "remove denied"}
	for _, tt := range []struct {
		name  string
		steps []fakeStep
		err   string
	}{
		{"running", []fakeStep{inspectStep(name, "true")}, ""},
		{"exited", []fakeStep{inspectStep(name, "false"), {Args: []string{"rm", "-f", name}}, cleanupStep(""), create}, ""},
		{"missing", []fakeStep{inspectStep(name, "missing"), cleanupStep(""), create}, ""},
		{"unavailable", []fakeStep{unavailable}, "Cannot connect"},
		{"invalid inspect", []fakeStep{inspectStep(name, "garbage")}, "unexpected running state"},
		{"cleanup ignored", []fakeStep{inspectStep(name, "missing"), badCleanup, create}, ""},
		{"create failure", []fakeStep{inspectStep(name, "missing"), cleanupStep(""), badCreate}, "image pull failed"},
		{"remove failure", []fakeStep{inspectStep(name, "false"), badRemove}, "remove denied"},
		{"name conflict", []fakeStep{inspectStep(name, "missing"), cleanupStep(""), conflict, inspectStep(name, "missing"), inspectStep(name, "false"), inspectStep(name, "true")}, ""},
		{"conflict daemon failed", []fakeStep{inspectStep(name, "missing"), cleanupStep(""), conflict, unavailable}, "Cannot connect"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := scriptedRunner(t, tt.steps)
			before, _ := json.Marshal(spec)
			got, err := r.EnsureWarm(context.Background(), spec, 30*time.Minute)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("EnsureWarm() = %q, %v, want %q error", got, err, tt.err)
				}
			} else if err != nil || got != name {
				t.Fatalf("EnsureWarm() = %q, %v, want %q", got, err, name)
			}
			after, _ := json.Marshal(spec)
			if string(before) != string(after) {
				t.Fatal("EnsureWarm mutated the spec")
			}
		})
	}
	t.Run("missing binary", func(t *testing.T) {
		r := &Runner{Bin: filepath.Join(t.TempDir(), "missing-docker")}
		if _, err := r.EnsureWarm(context.Background(), spec, time.Minute); err == nil || !strings.Contains(err.Error(), "docker must be installed") {
			t.Fatalf("EnsureWarm() = %v", err)
		}
	})
}

func TestWarmConflictCancellation(t *testing.T) {
	spec := RunSpec{Image: "dx-go"}
	name := WarmName(spec)
	conflict := createStep(spec)
	conflict.Code, conflict.Stderr = 125, "Conflict. The container name is already in use"
	blocked := inspectStep(name, "false")
	blocked.Mode = "block"
	r, _ := scriptedRunner(t, []fakeStep{inspectStep(name, "missing"), cleanupStep(""), conflict, blocked})
	marker := os.Getenv("DX_DOCKER_TEST_SCRIPT") + ".blocked"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	results := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_, err := r.EnsureWarm(ctx, spec, 30*time.Minute)
		results <- err
	}()
	defer func() { cancel(); <-stopped }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("conflict re-inspect did not start")
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-results; !errors.Is(err, context.Canceled) {
		t.Fatalf("EnsureWarm() = %v, want context canceled", err)
	}
}

func TestListWarm(t *testing.T) {
	args := []string{"ps", "--filter", "label=dx.warm=1", "--format", "{{json .}}"}
	for _, tt := range []struct {
		name   string
		output string
		code   int
		want   []WarmContainer
		err    string
	}{
		{name: "empty"},
		{
			name: "sorted rows",
			output: `{"Names":"z","Image":"dx-node:22","Labels":"dx.warm=1,dx.root=/z,dx.toolchain=node","Status":"Up 5 minutes","CreatedAt":"2026-01-02 03:04:05 +0000 UTC"}` + "\n\n" +
				`{"Names":"b","Image":"dx-go:1.25","Labels":"dx.root=/a=b,dx.toolchain=go","Status":"Up 1 minute","CreatedAt":"2026-01-02T04:04:05+01:00"}` + "\n" +
				`{"Names":"a","Image":"dx-go:1.25","Labels":"dx.root=/a=b","Status":"Up 2 minutes","CreatedAt":"2026-01-02 03:04:05 +0000 UTC"}`,
			want: []WarmContainer{
				{Name: "a", Image: "dx-go:1.25", Root: "/a=b", Status: "Up 2 minutes", Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
				{Name: "b", Image: "dx-go:1.25", Root: "/a=b", Toolchain: "go", Status: "Up 1 minute", Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
				{Name: "z", Image: "dx-node:22", Root: "/z", Toolchain: "node", Status: "Up 5 minutes", Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
			},
		},
		{name: "bad json", output: "{broken", err: "parse docker ps"},
		{name: "bad time", output: `{"Names":"bad","CreatedAt":"yesterday"}`, err: "creation time for \"bad\""},
		{name: "daemon error", code: 125, err: "daemon unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := scriptedRunner(t, []fakeStep{{Args: args, Stdout: tt.output, Code: tt.code, Stderr: "daemon unavailable"}})
			got, err := r.ListWarm(context.Background())
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("ListWarm() error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for i := range got {
				got[i].Created = got[i].Created.UTC()
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ListWarm() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStopWarm(t *testing.T) {
	for _, tt := range []struct {
		name  string
		names []string
		steps []fakeStep
		err   bool
	}{
		{name: "empty"},
		{name: "names", names: []string{"dx-warm-a", "dx-warm-b"}, steps: []fakeStep{{Args: []string{"stop", "--time", "5", "dx-warm-a", "dx-warm-b"}}}},
		{name: "failure", names: []string{"dx-warm-a"}, steps: []fakeStep{{Args: []string{"stop", "--time", "5", "dx-warm-a"}, Code: 1, Stderr: "stop failed"}}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := scriptedRunner(t, tt.steps)
			if err := r.StopWarm(context.Background(), tt.names...); (err != nil) != tt.err {
				t.Fatalf("StopWarm() = %v", err)
			}
		})
	}
}

func TestCleanupStale(t *testing.T) {
	failedList := cleanupStep("")
	failedList.Code, failedList.Stderr = 125, "daemon unavailable"
	for _, tt := range []struct {
		name  string
		steps []fakeStep
		err   bool
	}{
		{"empty", []fakeStep{cleanupStep("")}, false},
		{"stale", []fakeStep{cleanupStep("abc\ndef\n"), {Args: []string{"rm", "-f", "abc", "def"}}}, false},
		{"list failed", []fakeStep{failedList}, true},
		{"remove failed", []fakeStep{cleanupStep("abc"), {Args: []string{"rm", "-f", "abc"}, Code: 1}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := scriptedRunner(t, tt.steps)
			if err := r.CleanupStale(context.Background()); (err != nil) != tt.err {
				t.Fatalf("CleanupStale() = %v", err)
			}
		})
	}
}
