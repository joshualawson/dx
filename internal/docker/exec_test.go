package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExecArgs(t *testing.T) {
	for _, tt := range []struct {
		name string
		spec ExecSpec
		args []string
		env  []string
	}{
		{name: "empty", args: []string{"exec"}},
		{
			name: "all fields",
			spec: ExecSpec{Container: "dx-warm-abc", Cmd: []string{"go", "test", "a b", ""}, Workdir: "/src", User: "1000:1000", Env: []string{"GH_TOKEN=old", "HOME=/container", "PATH=/bin", "DOCKER_HOST=unix:///vm", "DOCKER_CONTEXT=container", "GH_TOKEN=new", "EMPTY="}, Interactive: true, TTY: true},
			args: []string{"exec", "-i", "-t", "--workdir", "/src", "--user", "1000:1000", "-e", "HOME=/container", "-e", "PATH=/bin", "-e", "DOCKER_HOST=unix:///vm", "-e", "DOCKER_CONTEXT=container", "-e", "GH_TOKEN", "-e", "EMPTY", "dx-warm-abc", "go", "test", "a b", ""},
			env:  []string{"GH_TOKEN=new", "EMPTY="},
		},
		{name: "interactive only", spec: ExecSpec{Container: "name", Interactive: true}, args: []string{"exec", "-i", "name"}},
		{name: "tty only", spec: ExecSpec{Container: "name", TTY: true}, args: []string{"exec", "-t", "name"}},
		{
			name: "reserved duplicates",
			spec: ExecSpec{Env: []string{"HOME=old", "HOME=new", "PATH=old", "PATH=", "DOCKER_CUSTOM=old", "DOCKER_CUSTOM=new", "HOMELESS=a=b"}},
			args: []string{"exec", "-e", "HOME=new", "-e", "PATH=", "-e", "DOCKER_CUSTOM=new", "-e", "HOMELESS"},
			env:  []string{"HOMELESS=a=b"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := json.Marshal(tt.spec)
			if got := ExecArgs(tt.spec); !reflect.DeepEqual(got, tt.args) {
				t.Fatalf("ExecArgs() = %#v, want %#v", got, tt.args)
			}
			if got := ExecProcessEnv(tt.spec); !reflect.DeepEqual(got, tt.env) {
				t.Fatalf("ExecProcessEnv() = %#v, want %#v", got, tt.env)
			}
			after, _ := json.Marshal(tt.spec)
			if string(before) != string(after) {
				t.Fatal("exec helpers mutated the spec")
			}
		})
	}
}

func execStep(spec ExecSpec, code int) fakeStep {
	spec.Cmd = append([]string{"/bin/sh", "-c", "*", "dx-exec"}, spec.Cmd...)
	return fakeStep{Args: ExecArgs(spec), Code: code}
}

func wrappedPIDs(t *testing.T, calls [][]string) []string {
	t.Helper()
	pattern := regexp.MustCompile(`^echo \$\$ > (/tmp/dx-exec-[a-f0-9]{32}\.pid) && exec "\$@"$`)
	var files []string
	for _, args := range calls {
		for i, arg := range args {
			if arg != "/bin/sh" || i+3 >= len(args) || !strings.HasPrefix(args[i+2], "echo ") {
				continue
			}
			match := pattern.FindStringSubmatch(args[i+2])
			if len(match) != 2 || args[i+1] != "-c" || args[i+3] != "dx-exec" {
				t.Fatalf("invalid wrapper: %#v", args)
			}
			files = append(files, match[1])
		}
	}
	return files
}

func TestRunWarm(t *testing.T) {
	spec := RunSpec{Image: "dx-go"}
	name := WarmName(spec)
	command := ExecSpec{Container: name, Cmd: []string{"go", "test", "a b", `quote"and$`, ""}, Workdir: "/src", User: "1000:1000", Env: []string{"GH_TOKEN=old", "GH_TOKEN=secret", "HOME=/container", "DOCKER_CONTEXT=container"}, Interactive: true}
	for _, code := range []int{0, 1, 42, 125, 127, 255} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			step := execStep(command, code)
			step.Mode, step.Stdout, step.Stderr = "echo", "prefix\n", "command stderr\n"
			step.Env = map[string]string{"GH_TOKEN": "secret", "HOME": "host-home", "DOCKER_CONTEXT": "host-context"}
			steps := []fakeStep{inspectStep(name, "true"), step}
			if code != 0 {
				steps = append(steps, inspectStep(name, "true"))
			}
			r, log := scriptedRunner(t, steps)
			t.Setenv("HOME", "host-home")
			t.Setenv("DOCKER_CONTEXT", "host-context")
			r.Env = []string{"GH_TOKEN=runner-secret"}
			var stdout, stderr bytes.Buffer
			r.Stdin, r.Stdout, r.Stderr = strings.NewReader("stdin\n"), &stdout, &stderr
			input := command
			input.Container = "ignored"
			got, err := r.RunWarm(context.Background(), spec, input, 30*time.Minute)
			if err != nil || got != code {
				t.Fatalf("RunWarm() = %d, %v, want %d, nil", got, err, code)
			}
			if stdout.String() != "prefix\nstdin\n" || stderr.String() != "command stderr\n" {
				t.Fatalf("stdio = %q, %q", stdout.String(), stderr.String())
			}
			if !reflect.DeepEqual(r.Env, []string{"GH_TOKEN=runner-secret"}) || input.Container != "ignored" {
				t.Fatal("RunWarm mutated caller settings")
			}
			calls := loggedCommands(t, log)
			if files := wrappedPIDs(t, calls); len(files) != 1 {
				t.Fatalf("expected one wrapped exec, got %v", files)
			}
			for _, args := range calls {
				if strings.Contains(strings.Join(args, " "), "secret") {
					t.Fatalf("credentials leaked into argv: %#v", args)
				}
			}
		})
	}
}

func TestRunWarmRetry(t *testing.T) {
	spec := RunSpec{Image: "dx-go"}
	name := WarmName(spec)
	command := ExecSpec{Container: name, Cmd: []string{"go", "test"}}
	first := execStep(command, 125)
	pidMissing := fakeStep{Args: []string{"exec", name, "test", "-f", "*"}, Code: 1}
	pidExists := pidMissing
	pidExists.Code = 0
	pidUnknown := pidMissing
	pidUnknown.Code, pidUnknown.Stderr = 125, "container is not running"
	pidUnavailable := pidUnknown
	pidUnavailable.Code = 1
	unavailable := inspectStep(name, "")
	unavailable.Code, unavailable.Stderr = 125, "daemon unavailable"
	for _, tt := range []struct {
		name   string
		middle []fakeStep
		end    []fakeStep
		code   int
		tries  int
	}{
		{
			name:   "vanished before start",
			middle: []fakeStep{inspectStep(name, "missing"), inspectStep(name, "missing"), cleanupStep(""), createStep(spec)},
			end:    []fakeStep{execStep(command, 0)}, code: 0, tries: 2,
		},
		{
			name:   "retry at most once",
			middle: []fakeStep{inspectStep(name, "missing"), inspectStep(name, "missing"), cleanupStep(""), createStep(spec)},
			end:    []fakeStep{execStep(command, 125)}, code: 125, tries: 2,
		},
		{
			name:   "stopped without pid",
			middle: []fakeStep{inspectStep(name, "false"), pidMissing, inspectStep(name, "false"), {Args: []string{"rm", "-f", name}}, cleanupStep(""), createStep(spec)},
			end:    []fakeStep{execStep(command, 42)}, code: 42, tries: 2,
		},
		{name: "process already started", middle: []fakeStep{inspectStep(name, "false"), pidExists}, code: 125, tries: 1},
		{name: "pid cannot be checked", middle: []fakeStep{inspectStep(name, "false"), pidUnknown}, code: 125, tries: 1},
		{name: "pid check cli exit 1", middle: []fakeStep{inspectStep(name, "false"), pidUnavailable}, code: 125, tries: 1},
		{name: "still running", middle: []fakeStep{inspectStep(name, "true")}, code: 125, tries: 1},
		{name: "inspect unavailable", middle: []fakeStep{unavailable}, code: 125, tries: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			steps := append([]fakeStep{inspectStep(name, "true"), first}, tt.middle...)
			steps = append(steps, tt.end...)
			r, log := scriptedRunner(t, steps)
			code, err := r.RunWarm(context.Background(), spec, command, 30*time.Minute)
			if err != nil || code != tt.code {
				t.Fatalf("RunWarm() = %d, %v, want %d, nil", code, err, tt.code)
			}
			calls := loggedCommands(t, log)
			files := wrappedPIDs(t, calls)
			if len(files) != tt.tries || len(files) == 2 && files[0] == files[1] {
				t.Fatalf("unexpected exec pid files: %v", files)
			}
			for _, args := range calls {
				if len(args) == 5 && args[0] == "exec" && args[2] == "test" && args[4] != files[0] {
					t.Fatalf("checked wrong pid file: %#v, want %q", args, files[0])
				}
			}
		})
	}
}

func TestRunWarmMissingBinary(t *testing.T) {
	r := &Runner{Bin: filepath.Join(t.TempDir(), "missing-docker")}
	code, err := r.RunWarm(context.Background(), RunSpec{Image: "dx-go"}, ExecSpec{Cmd: []string{"go"}}, time.Minute)
	if code != -1 || err == nil || !strings.Contains(err.Error(), "docker must be installed") {
		t.Fatalf("RunWarm() = %d, %v", code, err)
	}
}
