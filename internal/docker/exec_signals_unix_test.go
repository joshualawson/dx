//go:build unix

package docker

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func assertWarmForward(t *testing.T, args []string, container, pidFile, signalName string) {
	t.Helper()
	want := []string{"exec", container, "/bin/sh", "-c", warmForwardScript, "dx-exec-signal", pidFile, signalName}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("remote forward = %#v, want %#v", args, want)
	}
}

func TestWarmForwardScript(t *testing.T) {
	for _, tt := range []struct{ name, fragment string }{
		{"positional arguments", "f=$1\nsig=$2"},
		{"pid wait", "for i in 1 2 3 4 5 6 7 8 9 10; do"},
		{"nonempty pid file", `if [ -s "$f" ]; then`},
		{"root included", "pids=$(cat \"$f\") || exit 1\n        frontier=$pids"},
		{"pgrep fallback", "if command -v pgrep >/dev/null 2>&1; then"},
		{"breadth first", "while [ -n \"$frontier\" ]; do\n                frontier=$(for parent in $frontier; do pgrep -P \"$parent\" 2>/dev/null || :; done)\n                pids=\"$pids $frontier\""},
		{"one kill ignoring exited processes", `kill -"$sig" $pids 2>/dev/null || :`},
		{"wait interval and failure", "sleep 0.1\ndone\nexit 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(warmForwardScript, tt.fragment) {
				t.Fatalf("forward script missing %q", tt.fragment)
			}
		})
	}
	if strings.Count(warmForwardScript, "kill -") != 1 {
		t.Fatal("the tree must be signalled with a single kill call")
	}
}

func waitSignalCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for signal test condition")
		case <-ticker.C:
		}
	}
}

func TestWarmForwardOutlivesExec(t *testing.T) {
	container, pidFile := "dx-warm-test", `/tmp/pid with '$shell' characters.pid`
	command := ExecSpec{Container: container, Cmd: []string{"example"}}
	r, log := scriptedRunner(t, []fakeStep{
		{Args: ExecArgs(command), Mode: "wait-kill-exit", Stdout: "ready\n", Code: 42},
		{Args: []string{"exec", container, "/bin/sh", "-c", "*", "dx-exec-signal", pidFile, "INT"}, Mode: "kill-delayed"},
	})
	script := os.Getenv("DX_DOCKER_TEST_SCRIPT")
	ready := &readyWriter{ready: make(chan struct{})}
	r.Stdout = ready
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	type result struct {
		code int
		err  error
	}
	results := make(chan result, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		code, err := r.runWarmExec(ctx, command, pidFile)
		results <- result{code, err}
	}()
	defer func() {
		_ = os.WriteFile(script+".release", nil, 0o600)
		cancel()
		<-stopped
	}()
	select {
	case <-ready.ready:
	case <-ctx.Done():
		t.Fatal("warm exec did not become ready")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	waitSignalCondition(t, func() bool {
		data, err := os.ReadFile(script + ".exec-pid")
		if err != nil {
			return false
		}
		pid, err := strconv.Atoi(string(data))
		return err == nil && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	})
	cancel()
	select {
	case got := <-results:
		t.Fatalf("exec returned before the forward completed: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	writeFixture(t, script+".release", "")
	select {
	case got := <-results:
		if got.err != nil || got.code != 42 {
			t.Fatalf("runWarmExec() = %d, %v, want 42, nil", got.code, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("forward did not finish after release")
	}
	if _, err := os.Stat(script + ".forwarded"); err != nil {
		t.Fatalf("forward was cancelled before completion: %v", err)
	}
	calls := loggedCommands(t, log)
	if len(calls) != 2 {
		t.Fatalf("expected an exec and a forward, got %#v", calls)
	}
	assertWarmForward(t, calls[1], container, pidFile, "INT")
}

func TestWarmForwardDrainsBufferedSignals(t *testing.T) {
	for _, tt := range []struct {
		name        string
		signal      syscall.Signal
		signalName  string
		terminalTTY bool
	}{
		{"interrupt", syscall.SIGINT, "INT", false},
		{"terminate", syscall.SIGTERM, "TERM", false},
		{"hangup", syscall.SIGHUP, "HUP", false},
		{"tty interrupt skipped", syscall.SIGINT, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			container, pidFile := "dx-warm-test", "/tmp/dx-exec-buffered.pid"
			args := []string{"exec", container, "/bin/sh", "-c", "*", "dx-exec-signal", pidFile, "*"}
			steps := []fakeStep{{Args: args, Mode: "kill-delayed"}}
			if tt.signalName != "" {
				steps = append(steps, fakeStep{Args: args})
			}
			r, log := scriptedRunner(t, steps)
			script := os.Getenv("DX_DOCKER_TEST_SCRIPT")
			signals := make(chan os.Signal, 8)
			stop := sync.OnceFunc(forwardWarmSignals(r, container, pidFile, tt.terminalTTY, signals))
			defer func() {
				_ = os.WriteFile(script+".release", nil, 0o600)
				stop()
			}()
			signals <- syscall.SIGTERM
			waitSignalCondition(t, func() bool {
				_, err := os.Stat(script + ".kill")
				return err == nil
			})
			signals <- tt.signal
			if len(signals) != 1 {
				t.Fatal("expected a signal buffered behind the in-flight forward")
			}
			stopped := make(chan struct{})
			go func() { stop(); close(stopped) }()
			select {
			case <-stopped:
				t.Fatal("stop returned before the in-flight forward completed")
			case <-time.After(100 * time.Millisecond):
			}
			writeFixture(t, script+".release", "")
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("stop did not drain buffered signals")
			}
			if _, err := os.Stat(script + ".forwarded"); err != nil {
				t.Fatalf("in-flight forward was cancelled: %v", err)
			}
			calls := loggedCommands(t, log)
			if len(calls) != len(steps) {
				t.Fatalf("forward count = %d, want %d", len(calls), len(steps))
			}
			assertWarmForward(t, calls[0], container, pidFile, "TERM")
			if tt.signalName != "" {
				assertWarmForward(t, calls[1], container, pidFile, tt.signalName)
			}
		})
	}
}

func TestRunWarmSignals(t *testing.T) {
	for _, tt := range []struct {
		name     string
		signal   syscall.Signal
		terminal bool
		tty      bool
		killCode int
	}{
		{"interrupt pipe", syscall.SIGINT, false, false, 0},
		{"interrupt tty without terminal", syscall.SIGINT, false, true, 0},
		{"interrupt terminal without tty", syscall.SIGINT, true, false, 0},
		{"interrupt terminal tty", syscall.SIGINT, true, true, 0},
		{"terminate pipe", syscall.SIGTERM, false, false, 0},
		{"hangup pipe", syscall.SIGHUP, false, false, 0},
		{"terminate terminal tty", syscall.SIGTERM, true, true, 0},
		{"hangup terminal tty", syscall.SIGHUP, true, true, 0},
		{"kill failure ignored", syscall.SIGTERM, false, false, 125},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := RunSpec{Image: "dx-go"}
			name := WarmName(spec)
			command := ExecSpec{Container: name, Cmd: []string{"go", "test"}, TTY: tt.tty}
			step := execStep(command, 42)
			step.Mode, step.Stdout = "wait-kill", "ready\n"
			var terminal *os.File
			if tt.terminal {
				terminal = requireTerminal(t)
			}
			r, log := scriptedRunner(t, []fakeStep{
				inspectStep(name, "true"), step,
				{Args: []string{"exec", name, "/bin/sh", "-c", "*", "dx-exec-signal", "*", "*"}, Mode: "kill", Code: tt.killCode},
				inspectStep(name, "true"),
			})
			if terminal != nil {
				r.Stdin = terminal
			}
			ready := &readyWriter{ready: make(chan struct{})}
			r.Stdout = ready
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			type result struct {
				code int
				err  error
			}
			results := make(chan result, 1)
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				code, err := r.RunWarm(ctx, spec, command, 30*time.Minute)
				results <- result{code, err}
			}()
			defer func() { cancel(); <-stopped }()
			select {
			case <-ready.ready:
			case <-ctx.Done():
				t.Fatal("warm exec did not become ready")
			}
			if err := syscall.Kill(os.Getpid(), tt.signal); err != nil {
				t.Fatal(err)
			}
			wantSignal := tt.signal
			if tt.signal == syscall.SIGINT && tt.terminal && tt.tty {
				select {
				case got := <-results:
					t.Fatalf("terminal tty interrupt was forwarded: %+v", got)
				case <-time.After(100 * time.Millisecond):
				}
				wantSignal = syscall.SIGTERM
				if err := syscall.Kill(os.Getpid(), wantSignal); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case got := <-results:
				if got.err != nil || got.code != 42 {
					t.Fatalf("RunWarm() = %d, %v, want 42, nil", got.code, got.err)
				}
			case <-ctx.Done():
				t.Fatal("signal did not reach warm exec")
			}
			calls := loggedCommands(t, log)
			files := wrappedPIDs(t, calls)
			if len(files) != 1 || len(calls) != 4 {
				t.Fatalf("unexpected signal calls: %#v", calls)
			}
			signalName := map[syscall.Signal]string{syscall.SIGINT: "INT", syscall.SIGTERM: "TERM", syscall.SIGHUP: "HUP"}[wantSignal]
			assertWarmForward(t, calls[2], name, files[0], signalName)
		})
	}
}
