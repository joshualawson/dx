package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshualawson/dx/internal/config"
)

func TestShimOperationErrors(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		operation string
		doctor    bool
	}{
		{name: "install", args: []string{"shims", "install", "go"}, operation: "install"},
		{name: "uninstall", args: []string{"shims", "uninstall"}, operation: "uninstall"},
		{name: "list", args: []string{"shims", "list"}, operation: "list"},
		{name: "which list", args: []string{"which", "go"}, operation: "list"},
		{name: "doctor list", args: []string{"doctor"}, operation: "list", doctor: true},
		{name: "install executable", args: []string{"shims", "install", "go"}, operation: "executable"},
		{name: "which executable", args: []string{"which", "go"}, operation: "executable"},
		{name: "doctor executable", args: []string{"doctor"}, operation: "executable", doctor: true},
		{name: "local executable", args: []string{"version"}, operation: "local executable"},
		{name: "which path", args: []string{"which", "go"}, operation: "find"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			e.Args = tt.args
			installer := e.Shims.(*fakeInstaller)
			failure := errors.New("injected shim failure")
			switch tt.operation {
			case "install":
				installer.installErr = failure
			case "uninstall":
				installer.uninstallErr = failure
			case "list":
				installer.listErr = failure
			case "executable":
				e.ShimErr = failure
			case "local executable":
				e.ShimTool, e.ShimErr = "go", failure
			case "find":
				e.FindLocal = func(string, string, string, string, string, string) (string, error) { return "", failure }
			}
			if tt.operation == "local executable" || tt.operation == "find" {
				getenv := e.Getenv
				e.Getenv = func(k string) string {
					if k == "DX_LOCAL" {
						return "1"
					}
					return getenv(k)
				}
			}
			code := Main(e)
			if tt.doctor {
				if code != 0 || !strings.Contains(stdout.String(), "shims: injected shim failure\n") {
					t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
				}
			} else if code != 125 || stderr.String() != "dx: injected shim failure\n" {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if f.args != nil || f.warmCalls != 0 {
				t.Fatal("shim error started a container")
			}
		})
	}
}

func TestShimInspectionRequiresTrust(t *testing.T) {
	for _, args := range [][]string{{"shims", "list"}, {"which", "go"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			e, _, stdout, stderr := testEnv(t)
			e.Args = args
			filename := filepath.Join(e.Cwd, ".dx.yaml")
			put(t, filename, "tools: {go: {image: custom/go}}\n")
			if code := Main(e); code != 125 || !strings.Contains(stderr.String(), "dx trust "+filename) || stdout.Len() != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
		})
	}
}

func TestShimConfigLocalOrigin(t *testing.T) {
	e, _, stdout, stderr := testEnv(t)
	filename := filepath.Join(e.Home, "global.yaml")
	put(t, filename, "local: [go]\n")
	getenv := e.Getenv
	e.Getenv = func(k string) string {
		if k == "DX_CONFIG" {
			return filename
		}
		return getenv(k)
	}
	e.Args = []string{"which", "go"}
	e.FindLocal = func(string, string, string, string, string, string) (string, error) { return "/local/go", nil }
	if code := Main(e); code != 0 || !strings.Contains(stdout.String(), "go: local /local/go (local in "+filename+")\n") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestWhichProjectRouting(t *testing.T) {
	e, _, stdout, stderr := testEnv(t)
	put(t, filepath.Join(e.Cwd, "go.mod"), "module example.com/test\n")
	e.LoadConfig = func(config.Options) (*config.Result, error) { return &config.Result{Config: config.Config{}}, nil }
	e.Args = []string{"which", "make"}
	if code := Main(e); code != 0 || !strings.Contains(stdout.String(), "make: dx ghcr.io/joshualawson/dx-go:latest (go.mod in project), warm\n") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}
