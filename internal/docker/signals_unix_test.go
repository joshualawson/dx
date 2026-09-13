//go:build unix

package docker

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func fakePlatform(args []string) int {
	if args[0] != "signal-child" {
		return 93
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	_, _ = fmt.Fprintln(os.Stdout, "ready")
	return 40 + int((<-signals).(syscall.Signal))
}

type readyWriter struct {
	ready chan struct{}
	once  sync.Once
}

func (w *readyWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "ready") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func requireTerminal(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Open("/dev/tty")
	if err != nil {
		t.Skipf("controlling terminal unavailable: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if !IsTerminal(file) {
		t.Skip("/dev/tty is not a terminal")
	}
	return file
}

func TestRunSignals(t *testing.T) {
	for _, tt := range []struct {
		name     string
		signal   syscall.Signal
		terminal bool
	}{
		{"interrupt pipe", syscall.SIGINT, false},
		{"terminate pipe", syscall.SIGTERM, false},
		{"hangup pipe", syscall.SIGHUP, false},
		{"interrupt terminal", syscall.SIGINT, true},
		{"terminate terminal", syscall.SIGTERM, true},
		{"hangup terminal", syscall.SIGHUP, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeRunner(t)
			if tt.terminal {
				r.Stdin = requireTerminal(t)
			}
			ready := &readyWriter{ready: make(chan struct{})}
			r.Stdout = ready
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			type result struct {
				code int
				err  error
			}
			results := make(chan result, 1)
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				code, err := r.Run(ctx, []string{"signal-child"})
				results <- result{code, err}
			}()
			defer func() { cancel(); <-stopped }()
			select {
			case <-ready.ready:
			case <-ctx.Done():
				t.Fatal("child did not become ready")
			}
			if err := syscall.Kill(os.Getpid(), tt.signal); err != nil {
				t.Fatal(err)
			}
			wantSignal := tt.signal
			if tt.terminal && tt.signal == syscall.SIGINT {
				select {
				case got := <-results:
					t.Fatalf("terminal SIGINT was forwarded: %+v", got)
				case <-time.After(100 * time.Millisecond):
				}
				wantSignal = syscall.SIGTERM
				if err := syscall.Kill(os.Getpid(), wantSignal); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case got := <-results:
				if got.err != nil || got.code != 40+int(wantSignal) {
					t.Fatalf("Run() = %d, %v, want %d", got.code, got.err, 40+int(wantSignal))
				}
			case <-ctx.Done():
				t.Fatal("signal did not reach child")
			}
		})
	}
}
