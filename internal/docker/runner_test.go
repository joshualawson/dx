package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("DX_DOCKER_TEST_HELPER") == "1" {
		os.Exit(fakeDocker(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeDocker(args []string) int {
	if log := os.Getenv("DX_DOCKER_TEST_LOG"); log != "" {
		file, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return 90
		}
		_ = json.NewEncoder(file).Encode(args)
		_ = file.Close()
	}
	if script := os.Getenv("DX_DOCKER_TEST_SCRIPT"); script != "" {
		return fakeScript(script, args)
	}
	if len(args) == 0 {
		return 91
	}
	switch args[0] {
	case "exit":
		code, _ := strconv.Atoi(args[1])
		return code
	case "env":
		values := make(map[string]string)
		for _, key := range args[1:] {
			values[key] = os.Getenv(key)
		}
		_ = json.NewEncoder(os.Stdout).Encode(values)
		return 0
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		_, _ = fmt.Fprint(os.Stderr, "from stderr")
		return 0
	case "output":
		_, _ = fmt.Fprint(os.Stdout, "  result\n\t")
		_, _ = fmt.Fprintln(os.Stderr, "daemon unavailable")
		code, _ := strconv.Atoi(args[1])
		return code
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "context":
		if reflect.DeepEqual(args, []string{"context", "show"}) {
			if os.Getenv("DX_DOCKER_TEST_CONTEXT_FAIL") == "1" {
				_, _ = fmt.Fprintln(os.Stderr, "context unavailable")
				return 1
			}
			_, _ = fmt.Fprintln(os.Stdout, os.Getenv("DX_DOCKER_TEST_CONTEXT"))
			return 0
		}
		if reflect.DeepEqual(args, []string{"context", "inspect", "--format", "{{.Endpoints.docker.Host}}"}) {
			_, _ = fmt.Fprintln(os.Stdout, os.Getenv("DX_DOCKER_TEST_SOCKET"))
			return 0
		}
	case "run":
		want := []string{"run", "--rm", "--network", "host", "busybox:1.37", "wget", "-q", "-T", "3", "-O", "-"}
		if len(args) != len(want)+1 || !reflect.DeepEqual(args[:len(want)], want) {
			_, _ = fmt.Fprintln(os.Stderr, "unexpected probe arguments")
			return 92
		}
		probe := os.Getenv("DX_DOCKER_TEST_PROBE")
		if code, err := strconv.Atoi(probe); err == nil && code != 0 {
			_, _ = fmt.Fprintln(os.Stderr, "probe command failed")
			return code
		}
		switch probe {
		case "fail":
			_, _ = fmt.Fprintln(os.Stderr, "host network unavailable")
			return 1
		case "empty":
			return 0
		case "sleep":
			time.Sleep(time.Minute)
			return 0
		default:
			client := &http.Client{Timeout: 3 * time.Second}
			response, err := client.Get(args[len(args)-1])
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				return 1
			}
			defer response.Body.Close()
			_, _ = fmt.Fprint(os.Stdout, "prefix ")
			_, _ = io.Copy(os.Stdout, response.Body)
			_, _ = fmt.Fprint(os.Stdout, " suffix")
			return 0
		}
	}
	return fakePlatform(args)
}

func fakeRunner(t *testing.T) *Runner {
	t.Helper()
	// Re-executed race binaries need no extra delay after each fake CLI invocation.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("DX_DOCKER_TEST_HELPER", "1")
	t.Setenv("DX_DOCKER_TEST_LOG", "")
	t.Setenv("DX_DOCKER_TEST_SCRIPT", "")
	t.Setenv("DX_DOCKER_TEST_CONTEXT", "test-context")
	t.Setenv("DX_DOCKER_TEST_CONTEXT_FAIL", "")
	t.Setenv("DX_DOCKER_TEST_PROBE", "")
	t.Setenv("DX_DOCKER_TEST_SOCKET", "")
	return &Runner{Bin: os.Args[0]}
}

func TestRun(t *testing.T) {
	for _, code := range []int{0, 1, 42, 125, 127, 255} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			r := fakeRunner(t)
			got, err := r.Run(context.Background(), []string{"exit", strconv.Itoa(code)})
			if err != nil || got != code {
				t.Fatalf("Run() = %d, %v, want %d, nil", got, err, code)
			}
		})
	}
}

func TestRunnerMissingBinary(t *testing.T) {
	for _, method := range []string{"run", "output"} {
		t.Run(method, func(t *testing.T) {
			r := &Runner{Bin: filepath.Join(t.TempDir(), "missing-docker")}
			var err error
			if method == "run" {
				_, err = r.Run(context.Background(), []string{"version"})
			} else {
				_, err = r.Output(context.Background(), "version")
			}
			if err == nil || !strings.Contains(err.Error(), "docker must be installed") || !strings.Contains(err.Error(), strconv.Quote(r.Bin)) {
				t.Fatalf("missing binary error = %v", err)
			}
			if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
				t.Fatalf("error does not wrap a missing executable error: %v", err)
			}
		})
	}
	if got := (&Runner{}).binary(); got != "docker" {
		t.Fatalf("default binary = %q", got)
	}
}

func TestRunStdio(t *testing.T) {
	r := fakeRunner(t)
	var stdout, stderr bytes.Buffer
	r.Stdin, r.Stdout, r.Stderr = strings.NewReader("hello\n"), &stdout, &stderr
	code, err := r.Run(context.Background(), []string{"echo"})
	if err != nil || code != 0 || stdout.String() != "hello\n" || stderr.String() != "from stderr" {
		t.Fatalf("Run() = %d, %v, stdout %q, stderr %q", code, err, stdout.String(), stderr.String())
	}
}

func TestRunnerEnv(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  []string
		want string
	}{
		{"nil", nil, "inherited"},
		{"empty", []string{}, "inherited"},
		{"override", []string{"DX_DOCKER_TEST_VALUE=override"}, "override"},
		{"duplicate", []string{"DX_DOCKER_TEST_VALUE=first", "DX_DOCKER_TEST_VALUE=last"}, "last"},
		{"clear", []string{"DX_DOCKER_TEST_VALUE="}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeRunner(t)
			t.Setenv("DX_DOCKER_TEST_VALUE", "inherited")
			t.Setenv("DX_DOCKER_TEST_UNTOUCHED", "present")
			r.Env = tt.env
			var stdout bytes.Buffer
			r.Stdout = &stdout
			args := []string{"env", "DX_DOCKER_TEST_VALUE", "DX_DOCKER_TEST_UNTOUCHED"}
			code, err := r.Run(context.Background(), args)
			if err != nil || code != 0 {
				t.Fatalf("Run() = %d, %v", code, err)
			}
			var got map[string]string
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"DX_DOCKER_TEST_VALUE": tt.want, "DX_DOCKER_TEST_UNTOUCHED": "present"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Run environment = %#v, want %#v", got, want)
			}
			output, err := r.Output(context.Background(), args...)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(output), &got); err != nil {
				t.Fatal(err)
			}
			want["DX_DOCKER_TEST_VALUE"] = "inherited"
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Output environment = %#v, want %#v", got, want)
			}
			if got := os.Getenv("DX_DOCKER_TEST_VALUE"); got != "inherited" {
				t.Fatalf("Runner changed parent environment to %q", got)
			}
		})
	}
}

func TestRunnerProcessEnv(t *testing.T) {
	r := fakeRunner(t)
	for _, key := range []string{"GH_TOKEN", "AWS_SECRET_ACCESS_KEY", "HOME", "PATH", "DOCKER_CONTEXT"} {
		t.Setenv(key, "host-value")
	}
	spec := RunSpec{Env: []string{
		"GH_TOKEN=old-secret", "AWS_SECRET_ACCESS_KEY=a=b c", "GH_TOKEN=new-secret",
		"HOME=/container", "PATH=/container/bin", "DOCKER_CONTEXT=container",
	}}
	r.Env = ProcessEnv(spec)
	var stdout bytes.Buffer
	r.Stdout = &stdout
	code, err := r.Run(context.Background(), []string{"env", "GH_TOKEN", "AWS_SECRET_ACCESS_KEY", "HOME", "PATH", "DOCKER_CONTEXT"})
	if err != nil || code != 0 {
		t.Fatalf("Run() = %d, %v", code, err)
	}
	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"GH_TOKEN": "new-secret", "AWS_SECRET_ACCESS_KEY": "a=b c",
		"HOME": "host-value", "PATH": "host-value", "DOCKER_CONTEXT": "host-value",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("process environment = %#v, want %#v", got, want)
	}
}

func TestRunCancellation(t *testing.T) {
	r := fakeRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	code, err := r.Run(ctx, []string{"sleep"})
	if err != nil || code == 0 {
		t.Fatalf("cancelled Run() = %d, %v", code, err)
	}
}

func TestOutput(t *testing.T) {
	for _, code := range []int{0, 42} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			r := fakeRunner(t)
			output, err := r.Output(context.Background(), "output", strconv.Itoa(code))
			if output != "result" {
				t.Fatalf("Output() = %q", output)
			}
			if code == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != code || !strings.Contains(err.Error(), "daemon unavailable") || !strings.Contains(err.Error(), "output 42") {
				t.Fatalf("Output() error = %v", err)
			}
		})
	}
}

func TestContextName(t *testing.T) {
	for _, tt := range []struct{ output, want string }{
		{"", "default"}, {"  desktop-linux\n", "desktop-linux"}, {"remote", "remote"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			r := fakeRunner(t)
			t.Setenv("DX_DOCKER_TEST_CONTEXT", tt.output)
			got, err := r.ContextName(context.Background())
			if err != nil || got != tt.want {
				t.Fatalf("ContextName() = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
	t.Run("failure", func(t *testing.T) {
		r := fakeRunner(t)
		t.Setenv("DX_DOCKER_TEST_CONTEXT_FAIL", "1")
		if _, err := r.ContextName(context.Background()); err == nil {
			t.Fatal("expected context error")
		}
	})
}

func TestIsTerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "regular")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	device, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	for _, tt := range []struct {
		name string
		file *os.File
		want bool
	}{
		{"nil", nil, false}, {"regular", file, false}, {"pipe reader", reader, false},
		{"pipe writer", writer, false}, {"null device", device, false}, {"closed", closed, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTerminal(tt.file); got != tt.want {
				t.Fatalf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}
