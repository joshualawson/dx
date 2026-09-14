package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/route"
	"github.com/joshualawson/dx/internal/shim"
)

type fakeInstaller struct {
	installed, uninstalled                  []string
	entries                                 []shim.Entry
	installErr, uninstallErr, listErr       error
	installCount, uninstallCount, listCalls int
}

func (f *fakeInstaller) Install(tools []string) ([]string, error) {
	f.installed = append([]string{}, tools...)
	if f.installCount != 0 {
		return make([]string, f.installCount), f.installErr
	}
	return f.installed, f.installErr
}
func (f *fakeInstaller) Uninstall(tools []string) ([]string, error) {
	f.uninstalled = append([]string{}, tools...)
	return make([]string, f.uninstallCount), f.uninstallErr
}
func (f *fakeInstaller) List() ([]shim.Entry, error) {
	f.listCalls++
	return append([]shim.Entry{}, f.entries...), f.listErr
}

func TestShimMode(t *testing.T) {
	for _, tt := range []struct {
		name, tool, dxLocal string
		args                []string
		local               bool
	}{
		{name: "tool flags", tool: "go", args: []string{"--image", "x"}},
		{name: "reserved command", tool: "config", args: []string{"--explain"}},
		{name: "tool help", tool: "go", args: []string{"--help"}},
		{name: "empty arguments", tool: "go"},
		{name: "local one", tool: "go", args: []string{"version"}, dxLocal: "1", local: true},
		{name: "local true", tool: "go", args: []string{"version"}, dxLocal: "TrUe", local: true},
		{name: "local yes", tool: "go", args: []string{"version"}, dxLocal: "YES", local: true},
		{name: "local false", tool: "go", args: []string{"version"}, dxLocal: "false"},
		{name: "local zero", tool: "go", args: []string{"version"}, dxLocal: "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			e.ShimTool, e.Args = tt.tool, tt.args
			oldGetenv := e.Getenv
			e.Getenv = func(k string) string {
				if k == "DX_LOCAL" {
					return tt.dxLocal
				}
				return oldGetenv(k)
			}
			e.PathEnv, e.PathExt = "test-path", ".EXE;.CMD"
			e.Environ = []string{"TOKEN=host", "PATH=test-path"}
			found, executed := false, false
			e.FindLocal = func(tool, pathEnv, pathExt, goos, dir, exe string) (string, error) {
				found = true
				if tool != tt.tool || pathEnv != e.PathEnv || pathExt != e.PathExt || goos != e.GOOS || dir != e.ShimDir || exe != e.Exe {
					t.Fatalf("find inputs = %v", []string{tool, pathEnv, pathExt, goos, dir, exe})
				}
				return "/local/go", nil
			}
			e.ExecLocal = func(p string, args, env []string) (int, error) {
				executed = true
				if p != "/local/go" || !reflect.DeepEqual(args, tt.args) || !reflect.DeepEqual(env, e.Environ) {
					t.Fatalf("exec = %s %v %v", p, args, env)
				}
				return 7, nil
			}
			f.code = 7
			if code := Main(e); code != 7 {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if found != tt.local || executed != tt.local {
				t.Fatalf("found=%t executed=%t", found, executed)
			}
			if tt.local {
				if f.args != nil || f.warmCalls != 0 || len(f.calls) != 0 {
					t.Fatal("local mode called Docker")
				}
			} else {
				want := append([]string{tt.tool}, tt.args...)
				if f.warmCalls != 0 {
					if !reflect.DeepEqual(f.execSpec.Cmd, want) {
						t.Fatalf("warm cmd=%v want=%v", f.execSpec.Cmd, want)
					}
				} else if !reflect.DeepEqual(f.args[len(f.args)-len(want):], want) {
					t.Fatalf("cold args=%v want tail=%v", f.args, want)
				}
			}
		})
	}
}

func TestShimLocalConfigAndErrors(t *testing.T) {
	for _, tt := range []struct {
		name                                                         string
		dxLocal, configLocal, found, findError, execError, untrusted bool
		code                                                         int
		reason                                                       string
	}{
		{name: "config local", configLocal: true, found: true, code: 7},
		{name: "config missing", configLocal: true, code: 125, reason: "config"},
		{name: "env missing", dxLocal: true, code: 125, reason: "DX_LOCAL=1"},
		{name: "env precedence", dxLocal: true, configLocal: true, code: 125, reason: "DX_LOCAL=1"},
		{name: "find error", dxLocal: true, findError: true, code: 125},
		{name: "exec error", dxLocal: true, found: true, execError: true, code: 125},
		{name: "untrusted dx", untrusted: true, code: 125},
		{name: "untrusted local", dxLocal: true, untrusted: true, code: 125},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			e.ShimTool, e.Args = "go", []string{"version"}
			origin := filepath.Join(e.Home, ".config", "dx", "config.yaml")
			if !tt.untrusted {
				e.LoadConfig = func(config.Options) (*config.Result, error) {
					cfg := config.Config{}
					if tt.configLocal {
						cfg.Local = []string{"go"}
					}
					return &config.Result{Config: cfg, Origins: map[string]string{"local": origin}}, nil
				}
			} else {
				put(t, filepath.Join(e.Cwd, ".dx.yaml"), "local: [go]\ndocker: true\n")
			}
			getenv := e.Getenv
			e.Getenv = func(k string) string {
				if k == "DX_LOCAL" && tt.dxLocal {
					return "1"
				}
				return getenv(k)
			}
			e.FindLocal = func(string, string, string, string, string, string) (string, error) {
				if tt.untrusted {
					t.Fatal("untrusted shim searched PATH")
				}
				if tt.findError {
					return "", errors.New("find local go failed")
				}
				if !tt.found {
					return "", fmt.Errorf("search go: %w", shim.ErrNotFound)
				}
				return "/local/go", nil
			}
			e.ExecLocal = func(string, []string, []string) (int, error) {
				if tt.execError {
					return 0, errors.New("exec local go failed")
				}
				return 7, nil
			}
			if code := Main(e); code != tt.code {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if tt.reason != "" {
				reason := tt.reason
				if reason == "config" {
					reason = "local in " + origin
				}
				want := fmt.Sprintf("dx: go is set to run locally (%s) but no go was found on PATH outside %s\n", reason, e.ShimDir)
				if stderr.String() != want {
					t.Fatalf("stderr=%q want=%q", stderr.String(), want)
				}
			}
			if tt.untrusted && !strings.Contains(stderr.String(), "is not trusted; review it and run: dx trust") {
				t.Fatalf("stderr=%s", stderr)
			}
			if tt.findError && !strings.Contains(stderr.String(), "find local go failed") {
				t.Fatalf("stderr=%s", stderr)
			}
			if tt.execError && !strings.Contains(stderr.String(), "exec local go failed") {
				t.Fatalf("stderr=%s", stderr)
			}
			if f.args != nil || f.warmCalls != 0 || len(f.calls) != 0 {
				t.Fatal("local failure fell back to Docker")
			}
		})
	}
}

func TestDirectCommandIgnoresLocal(t *testing.T) {
	e, f, _, stderr := testEnv(t)
	e.Args = []string{"go", "version"}
	e.Getenv = func(string) string { return "1" }
	e.LoadConfig = func(config.Options) (*config.Result, error) {
		return &config.Result{Config: config.Config{Local: []string{"go"}}}, nil
	}
	e.FindLocal = func(string, string, string, string, string, string) (string, error) {
		t.Fatal("direct dx searched local")
		return "", nil
	}
	if code := Main(e); code != 0 || f.warmCalls != 1 {
		t.Fatalf("code=%d warm=%d stderr=%s", code, f.warmCalls, stderr)
	}
}

func TestShimsInstallUninstall(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, tt := range []struct {
			name   string
			args   []string
			onPath bool
		}{
			{name: "all", args: []string{"shims", "install"}},
			{name: "named", args: []string{"shims", "install", "go", "custom"}},
			{name: "on path", args: []string{"shims", "install", "go"}, onPath: true},
			{name: "uninstall all", args: []string{"shims", "uninstall"}},
			{name: "uninstall named", args: []string{"shims", "uninstall", "go"}},
		} {
			t.Run(goos+"/"+tt.name, func(t *testing.T) {
				e, _, stdout, stderr := testEnv(t)
				e.Args, e.GOOS = tt.args, goos
				f := e.Shims.(*fakeInstaller)
				f.uninstallCount = 3
				e.OnPath = func(dir, pathEnv, os string) bool {
					if dir != e.ShimDir || pathEnv != e.PathEnv || os != goos {
						t.Fatal("wrong on-path inputs")
					}
					return tt.onPath
				}
				e.LoadConfig = func(config.Options) (*config.Result, error) {
					t.Fatal("install/uninstall loaded config")
					return nil, nil
				}
				if code := Main(e); code != 0 {
					t.Fatalf("code=%d stderr=%s", code, stderr)
				}
				if tt.args[1] == "uninstall" {
					if !reflect.DeepEqual(f.uninstalled, tt.args[2:]) || stdout.String() != "removed 3 shims\n" {
						t.Fatalf("uninstalled=%v stdout=%s", f.uninstalled, stdout)
					}
					return
				}
				want := tt.args[2:]
				if len(want) == 0 {
					for tool := range route.Builtin {
						want = append(want, tool)
					}
					sort.Strings(want)
				}
				if !reflect.DeepEqual(f.installed, want) {
					t.Fatalf("installed=%v want=%v", f.installed, want)
				}
				header := fmt.Sprintf("installed %d shims in %s\n", len(want), e.ShimDir)
				hint := fmt.Sprintf("add to your shell profile: export PATH=\"%s:$PATH\"\n", e.ShimDir)
				if goos == "windows" {
					hint = fmt.Sprintf("add %s to the start of your user PATH (System Properties → Environment Variables)\n", e.ShimDir)
				}
				if tt.onPath {
					hint = ""
				}
				if stdout.String() != header+hint {
					t.Fatalf("stdout=%q want=%q", stdout.String(), header+hint)
				}
			})
		}
	}
}

func TestShimsList(t *testing.T) {
	for _, mode := range []string{"dx", "env", "config", "empty"} {
		t.Run(mode, func(t *testing.T) {
			e, _, stdout, stderr := testEnv(t)
			e.Args = []string{"shims", "list"}
			f := e.Shims.(*fakeInstaller)
			if mode != "empty" {
				f.entries = []shim.Entry{{Tool: "node", Path: "/shims/node"}, {Tool: "go", Path: "/shims/go", Stale: true}}
			}
			if mode == "env" {
				e.Getenv = func(string) string { return "yes" }
			}
			e.LoadConfig = func(config.Options) (*config.Result, error) {
				cfg := config.Config{}
				if mode == "config" {
					cfg.Local = []string{"go"}
				}
				return &config.Result{Config: cfg, Origins: map[string]string{"local": "/config.yaml"}}, nil
			}
			if code := Main(e); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if mode == "empty" {
				if stdout.String() != "no shims installed\n" {
					t.Fatalf("stdout=%s", stdout)
				}
				return
			}
			lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			goRuns, nodeRuns := "dx stale", "dx"
			if mode == "env" {
				goRuns, nodeRuns = "local (DX_LOCAL=1) stale", "local (DX_LOCAL=1)"
			}
			if mode == "config" {
				goRuns = "local (local in /config.yaml) stale"
			}
			want := []string{"TOOL RUNS PATH", "go " + goRuns + " /shims/go", "node " + nodeRuns + " /shims/node"}
			if len(lines) != len(want) {
				t.Fatalf("table=%s", stdout)
			}
			for i := range lines {
				if !reflect.DeepEqual(strings.Fields(lines[i]), strings.Fields(want[i])) {
					t.Fatalf("line=%q want=%q", lines[i], want[i])
				}
			}
		})
	}
}

func TestWhich(t *testing.T) {
	for _, tt := range []struct {
		name, mode               string
		stale, installed, onPath bool
		first                    string
		code                     int
	}{
		{name: "warm dx", first: "go: dx ghcr.io/joshualawson/dx-go:latest (built-in mapping for go), warm", installed: true},
		{name: "custom cold", mode: "custom", first: "go: dx golang:1.25 (tools.go.image in config), cold", stale: true, installed: true, onPath: true},
		{name: "config cold", mode: "cold", first: "go: dx ghcr.io/joshualawson/dx-go:latest (built-in mapping for go), cold"},
		{name: "env local", mode: "env", first: "go: local /local/go (DX_LOCAL=1)", installed: true, onPath: true},
		{name: "config local", mode: "config", first: "go: local /local/go (local in /config.yaml)"},
		{name: "local missing", mode: "missing", first: "go: local not found (DX_LOCAL=1)", installed: true, code: 125},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, f, stdout, stderr := testEnv(t)
			e.Args = []string{"which", "go"}
			if tt.installed {
				e.Shims.(*fakeInstaller).entries = []shim.Entry{{Tool: "go", Path: "/shims/go", Stale: tt.stale}}
			}
			e.OnPath = func(string, string, string) bool { return tt.onPath }
			e.Getenv = func(string) string {
				if tt.mode == "env" || tt.mode == "missing" {
					return "true"
				}
				return ""
			}
			e.LoadConfig = func(config.Options) (*config.Result, error) {
				cfg := config.Config{}
				switch tt.mode {
				case "config":
					cfg.Local = []string{"go"}
				case "custom":
					cfg.Tools = map[string]config.Tool{"go": {Image: "golang:1.25"}}
				case "cold":
					no := false
					cfg.Tools = map[string]config.Tool{"go": {Warm: &no}}
				}
				return &config.Result{Config: cfg, Origins: map[string]string{"local": "/config.yaml"}}, nil
			}
			e.FindLocal = func(string, string, string, string, string, string) (string, error) {
				if tt.mode == "missing" {
					return "", shim.ErrNotFound
				}
				return "/local/go", nil
			}
			if code := Main(e); code != tt.code {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			status := "not installed"
			if tt.installed {
				status = "installed"
			}
			if tt.stale {
				status = "stale"
			}
			pathStatus := "no"
			if tt.onPath {
				pathStatus = "yes"
			}
			want := tt.first + fmt.Sprintf("\nshim: %s in %s; shims dir on PATH: %s\n", status, e.ShimDir, pathStatus)
			if stdout.String() != want || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%s want=%q", stdout.String(), stderr, want)
			}
			if f.warmCalls != 0 || f.args != nil || len(f.calls) != 0 {
				t.Fatal("which started a container")
			}
		})
	}
}

func TestShimDiagnosticsAndUsage(t *testing.T) {
	for _, args := range [][]string{{"shims"}, {"shims", "other"}, {"shims", "list", "go"}, {"shims", "install", "--wat"}, {"which"}, {"which", "go", "node"}, {"which", "--bad"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			e, _, _, stderr := testEnv(t)
			e.Args = args
			if code := Main(e); code != 125 || !strings.Contains(stderr.String(), "dx: usage:") {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
		})
	}
	for _, onPath := range []bool{false, true} {
		t.Run(fmt.Sprint(onPath), func(t *testing.T) {
			e, _, stdout, stderr := testEnv(t)
			e.Args = []string{"doctor"}
			e.Shims.(*fakeInstaller).entries = []shim.Entry{{Tool: "go"}, {Tool: "node", Stale: true}}
			e.OnPath = func(string, string, string) bool { return onPath }
			if code := Main(e); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			status := "no"
			if onPath {
				status = "yes"
			}
			if !strings.Contains(stdout.String(), fmt.Sprintf("shims: %s (on PATH: %s, 2 installed, 1 stale)\n", e.ShimDir, status)) {
				t.Fatalf("stdout=%s", stdout)
			}
		})
	}
	e, _, stdout, _ := testEnv(t)
	e.Args = []string{"help"}
	if code := Main(e); code != 0 {
		t.Fatalf("code=%d", code)
	}
	for _, text := range []string{"dx shims install|uninstall [tool...]", "dx shims list", "dx which <tool>", "DX_LOCAL=1", "local: [tool]", "dx <tool> always uses the container"} {
		if !strings.Contains(stdout.String(), text) {
			t.Fatalf("help missing %q", text)
		}
	}
}
