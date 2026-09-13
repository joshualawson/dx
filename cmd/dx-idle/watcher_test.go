package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func putStat(t *testing.T, root, pid, stat string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBusy(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string]string
		vanish  bool
		busy    bool
		err     bool
	}{
		{name: "empty"},
		{name: "only self", entries: map[string]string{"1": "unreadable self stat is irrelevant"}},
		{name: "running process", entries: map[string]string{"1": "1 (dx-idle) S", "2": "2 (server) R 1 2 3"}, busy: true},
		{name: "sleeping server", entries: map[string]string{"2": "2 (server) S 1 2 3"}, busy: true},
		{name: "stopped process", entries: map[string]string{"2": "2 (server) T 1 2 3"}, busy: true},
		{name: "zombie", entries: map[string]string{"2": "2 (server) Z 1 2 3"}},
		{name: "non numeric entries", entries: map[string]string{"self": "bad", "thread-self": "bad", "123abc": "bad", "+2": "bad", "-2": "bad", "١": "bad"}},
		{name: "vanished pid", vanish: true},
		{name: "complex running name", entries: map[string]string{"2": "2 (a ) Z (\n name) R 1 2 3"}, busy: true},
		{name: "complex zombie name", entries: map[string]string{"2": "2 (a ) R (\n name) Z 1 2 3"}},
		{name: "missing name", entries: map[string]string{"2": "2 malformed"}, busy: true, err: true},
		{name: "missing state", entries: map[string]string{"2": "2 (server)"}, busy: true, err: true},
		{name: "malformed state", entries: map[string]string{"2": "2 (server) invalid"}, busy: true, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for pid, stat := range tt.entries {
				putStat(t, root, pid, stat)
			}
			if tt.vanish {
				// A listed PID with no stat reproduces the disappearance race.
				if err := os.Mkdir(filepath.Join(root, "2"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			w := Watcher{ProcRoot: root, Self: 1}
			busy, err := w.Busy()
			if busy != tt.busy || (err != nil) != tt.err {
				t.Fatalf("Busy() = %v, %v; want busy %v, error %v", busy, err, tt.busy, tt.err)
			}
			if err != nil && !strings.Contains(err.Error(), filepath.Join(root, "2", "stat")) {
				t.Fatalf("error does not identify stat: %v", err)
			}
		})
	}
}

func TestBusyFilesystemErrors(t *testing.T) {
	for _, kind := range []string{"missing root", "stat is directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			wantPath := filepath.Join(root, "missing")
			w := Watcher{ProcRoot: wantPath, Self: 1}
			if kind == "stat is directory" {
				w.ProcRoot = root
				wantPath = filepath.Join(root, "2", "stat")
				if err := os.MkdirAll(wantPath, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			busy, err := w.Busy()
			if !busy {
				t.Fatal("failed scan must count as busy")
			}
			if err == nil || !strings.Contains(err.Error(), wantPath) || errors.Unwrap(err) == nil {
				t.Fatalf("expected wrapped error identifying %s, got %v", wantPath, err)
			}
		})
	}
}

func TestRunIdleTimer(t *testing.T) {
	type sample struct {
		second   int
		nextBusy bool
	}
	tests := []struct {
		name    string
		samples []sample
	}{
		{name: "startup grace and exact timeout", samples: []sample{{0, false}, {5, false}, {9, false}, {10, false}}},
		{name: "timeout after activity stops", samples: []sample{{0, true}, {5, true}, {20, false}, {25, false}, {30, false}}},
		{name: "activity resets timer", samples: []sample{{0, false}, {5, true}, {9, false}, {10, false}, {18, false}, {19, false}}},
		{name: "busy at timeout boundary", samples: []sample{{0, true}, {10, false}, {19, false}, {20, false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			index := 0
			w := Watcher{ProcRoot: root, Self: 1, Timeout: 10 * time.Second, Interval: time.Millisecond}
			w.Now = func() time.Time {
				if index >= len(tt.samples) {
					t.Fatal("watcher did not stop at timeout")
				}
				s := tt.samples[index]
				index++
				state := "Z"
				if s.nextBusy {
					state = "R"
				}
				putStat(t, root, "2", fmt.Sprintf("2 (server) %s 1 2 3", state))
				return time.Unix(int64(s.second), 0)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := w.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if index != len(tt.samples) {
				t.Fatalf("stopped at sample %d, want %d", index, len(tt.samples))
			}
		})
	}
}

func TestRunCancellation(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			w := Watcher{ProcRoot: t.TempDir(), Self: 1, Timeout: timeout, Interval: time.Millisecond}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			select {
			case err := <-done:
				t.Fatalf("exited before cancellation: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Run() = %v, want context.Canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not return promptly")
			}
		})
	}
}

func TestRunInvalidSettings(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		interval time.Duration
		want     string
	}{
		{"negative timeout", -1, time.Millisecond, "timeout"},
		{"zero interval", time.Second, 0, "interval"},
		{"negative interval", time.Second, -1, "interval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			w := Watcher{ProcRoot: t.TempDir(), Timeout: tt.timeout, Interval: tt.interval, Stderr: &output}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err := w.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run() = %v, want context.DeadlineExceeded", err)
			}
			if strings.Count(output.String(), tt.want) != 1 {
				t.Fatalf("expected one diagnostic containing %q, got %q", tt.want, output.String())
			}
		})
	}
}

func TestRunScanErrors(t *testing.T) {
	for _, kind := range []string{"missing root", "root is file", "stat is directory", "malformed stat"} {
		for _, logging := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/logging=%v", kind, logging), func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "proc")
				switch kind {
				case "root is file":
					if err := os.WriteFile(root, nil, 0o644); err != nil {
						t.Fatal(err)
					}
				case "stat is directory":
					// A directory reliably causes a read error even when tests run as root.
					if err := os.MkdirAll(filepath.Join(root, "2", "stat"), 0o755); err != nil {
						t.Fatal(err)
					}
				case "malformed stat":
					putStat(t, root, "2", "2 malformed")
				}
				var output bytes.Buffer
				w := Watcher{ProcRoot: root, Self: 1, Timeout: time.Second, Interval: time.Millisecond}
				if logging {
					w.Stderr = &output
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				calls := 0
				w.Now = func() time.Time {
					calls++
					if calls == 6 {
						cancel()
					}
					return time.Unix(int64(calls), 0)
				}
				if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("Run() = %v, want context.Canceled", err)
				}
				if calls < 6 {
					t.Fatalf("stopped after %d clock calls, want at least 6", calls)
				}
				if logging {
					_, err := w.Busy()
					if err == nil || output.String() != err.Error()+"\n" {
						t.Fatalf("expected scan error exactly once, got %q", output.String())
					}
				} else if output.Len() != 0 {
					t.Fatalf("unexpected output with nil Stderr: %q", output.String())
				}
			})
		}
	}
}

func TestRunScanErrorRecovery(t *testing.T) {
	for _, kind := range []string{"root", "stat"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "proc")
			if kind == "stat" {
				putStat(t, root, "2", "2 malformed")
			}
			var output bytes.Buffer
			w := Watcher{ProcRoot: root, Self: 1, Timeout: 2 * time.Second, Interval: time.Millisecond, Stderr: &output}
			second := -1
			w.Now = func() time.Time {
				second++
				if second > 5 {
					t.Fatal("watcher did not stop after scan recovered")
				}
				if second == 3 {
					if err := os.RemoveAll(root); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(root, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				return time.Unix(int64(second), 0)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := w.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if second != 5 {
				t.Fatalf("stopped at second %d, want 5", second)
			}
			if strings.Count(output.String(), "\n") != 1 {
				t.Fatalf("expected one diagnostic, got %q", output.String())
			}
		})
	}
}

func TestRunLogsDistinctErrorsOnce(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	w := Watcher{ProcRoot: root, Self: 1, Timeout: time.Second, Interval: time.Millisecond, Stderr: &output}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	calls := 0
	w.Now = func() time.Time {
		calls++
		stat := "2 malformed"
		if calls%2 == 0 {
			stat = "2 (server)"
		}
		putStat(t, root, "2", stat)
		if calls == 6 {
			cancel()
		}
		return time.Unix(int64(calls), 0)
	}
	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
	for _, message := range []string{"missing process name", "invalid process state"} {
		if strings.Count(output.String(), message) != 1 {
			t.Fatalf("expected %q exactly once, got %q", message, output.String())
		}
	}
	if strings.Count(output.String(), "\n") != 2 {
		t.Fatalf("expected two diagnostics, got %q", output.String())
	}
}

func TestRunDefaultClock(t *testing.T) {
	w := Watcher{ProcRoot: t.TempDir(), Self: 1, Timeout: 5 * time.Millisecond, Interval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < w.Timeout {
		t.Fatal("exited before startup idle window elapsed")
	}
}
