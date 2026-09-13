//go:build linux

package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
)

func TestReapChildren(t *testing.T) {
	type result struct {
		pid int
		err error
	}
	tests := []struct {
		name    string
		results []result
		wantErr error
	}{
		{name: "no children", results: []result{{-1, syscall.ECHILD}}},
		{name: "children still running", results: []result{{0, nil}}},
		{name: "drains coalesced signals", results: []result{{42, nil}, {43, nil}, {-1, syscall.ECHILD}}},
		{name: "stops before blocking", results: []result{{42, nil}, {0, nil}}},
		{name: "retries interruption", results: []result{{-1, syscall.EINTR}, {42, nil}, {0, nil}}},
		{name: "unexpected error", results: []result{{-1, syscall.EINVAL}}, wantErr: syscall.EINVAL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := reapChildren(func(pid int, status *syscall.WaitStatus, options int, usage *syscall.Rusage) (int, error) {
				if pid != -1 || status == nil || options != syscall.WNOHANG || usage != nil {
					t.Fatalf("unexpected Wait4 arguments: %d, %v, %d, %v", pid, status, options, usage)
				}
				if calls >= len(tt.results) {
					t.Fatal("Wait4 called after children were drained")
				}
				r := tt.results[calls]
				calls++
				return r.pid, r.err
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("reapChildren() = %v, want %v", err, tt.wantErr)
			}
			if calls != len(tt.results) {
				t.Fatalf("Wait4 called %d times, want %d", calls, len(tt.results))
			}
		})
	}
}

func TestRunFlagExitCodes(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code int
	}{
		{"unknown", []string{"--unknown"}, 2},
		{"invalid duration", []string{"--timeout=bad"}, 2},
		{"negative timeout", []string{"--timeout=-1s"}, 2},
		{"zero interval", []string{"--interval=0"}, 2},
		{"positional argument", []string{"extra"}, 2},
		{"help", []string{"--help"}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if code := run(tt.args, &output); code != tt.code {
				t.Fatalf("run() = %d, want %d", code, tt.code)
			}
			if output.Len() == 0 {
				t.Fatal("expected diagnostic output")
			}
			if tt.code == 2 {
				_, err := parseFlags(tt.args, io.Discard)
				if err == nil || strings.Count(output.String(), err.Error()) != 1 {
					t.Fatalf("expected error exactly once, got %q", output.String())
				}
			}
		})
	}
}
