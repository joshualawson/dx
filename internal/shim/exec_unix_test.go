//go:build unix

package shim

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("DX_SHIM_EXEC_TEST") == "1" {
		code, err := ExecLocal(os.Args[1], os.Args[2:], []string{"SHIM_TEST_VALUE=passed"})
		fmt.Fprintf(os.Stderr, "ExecLocal returned %d, %v\n", code, err)
		os.Exit(99)
	}
	os.Exit(m.Run())
}

func TestExecLocal(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); os.IsNotExist(err) {
		t.Skip("/bin/sh unavailable")
	}
	tests := []struct {
		name, script string
		args         []string
		want         int
	}{
		{"exit seven", "exit 7", nil, 7},
		{"exit zero", "exit 0", nil, 0},
		{"arguments", `[ "$#" -eq 2 ] && [ "$1" = "hello world" ] && [ "$2" = "--flag" ] || exit 8
exit 7`, []string{"hello world", "--flag"}, 7},
		{"environment", `[ "$SHIM_TEST_VALUE" = passed ] && [ -z "$DX_SHIM_EXEC_TEST" ] || exit 8
exit 7`, nil, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "local-tool")
			writeTestFile(t, script, "#!/bin/sh\n"+tt.script+"\n", 0755)
			testExe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(testExe, append([]string{script}, tt.args...)...)
			cmd.Env = append(os.Environ(), "DX_SHIM_EXEC_TEST=1")
			output, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("start subprocess: %v", err)
				}
				code = exit.ExitCode()
			}
			if code != tt.want {
				t.Fatalf("subprocess exit = %d, want %d: %s", code, tt.want, output)
			}
		})
	}
}

func TestExecLocalFailure(t *testing.T) {
	for _, name := range []string{"missing", "not-executable"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if name == "not-executable" {
				writeTestFile(t, path, "not executable", 0644)
			}
			code, err := ExecLocal(path, nil, nil)
			if code != -1 || err == nil || !strings.Contains(err.Error(), path) || errors.Unwrap(err) == nil {
				t.Fatalf("ExecLocal() = %d, %v", code, err)
			}
		})
	}
}
