package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/trust"
)

func TestPreferredCwd(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "real", "repo")
	visible := filepath.Join(base, "visible", "repo")
	for _, tt := range []struct {
		name, pwd string
		same      bool
		want      string
	}{
		{name: "symlink", pwd: visible, same: true, want: visible},
		{name: "stale", pwd: filepath.Join(base, "wrong"), want: cwd},
		{name: "unset", want: cwd},
		{name: "relative", pwd: "repo", same: true, want: cwd},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := preferredCwd(cwd, tt.pwd, func(a, b string) bool {
				if a != cwd || b != tt.pwd {
					t.Fatalf("same-folder inputs = %q, %q, want %q, %q", a, b, cwd, tt.pwd)
				}
				return tt.same
			})
			if got != tt.want {
				t.Fatalf("cwd = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMountPaths(t *testing.T) {
	// Non-Windows branches intentionally delegate host paths to filepath.
	base := t.TempDir()
	src := filepath.Join(base, "home", "me", "src")
	home := filepath.Join(base, "Users", "me")
	tests := []struct {
		name, goos, cwd, mount, want string
		fail                         bool
	}{
		{name: "linux relative", goos: "linux", cwd: filepath.Join(src, "sub"), mount: "..", want: src},
		{name: "darwin absolute", goos: "darwin", cwd: filepath.Join(home, "src"), mount: home, want: home},
		{name: "outside", goos: "linux", cwd: src, mount: filepath.Join(base, "home", "me", "other"), fail: true},
		{name: "prefix sibling", goos: "linux", cwd: src + "-other", mount: src, fail: true},
		{name: "windows relative", goos: "windows", cwd: `C:\Users\me\src\sub`, mount: "..", want: `C:\Users\me\src`},
		{name: "windows case", goos: "windows", cwd: `C:\Users\me\src`, mount: `c:\users\me`, want: `c:\users\me`},
		{name: "windows slash", goos: "windows", cwd: `C:\Users\me\src`, mount: "C:/Users/me", want: `C:\Users\me`},
		{name: "windows drive root", goos: "windows", cwd: `C:\Users\me\src`, mount: `C:\`, want: `C:\`},
		{name: "windows rooted", goos: "windows", cwd: `C:\Users\me\src`, mount: `\Users\me`, want: `C:\Users\me`},
		{name: "windows other drive", goos: "windows", cwd: `C:\Users\me\src`, mount: `D:\Users\me`, fail: true},
		{name: "windows drive relative", goos: "windows", cwd: `C:\Users\me\src`, mount: `C:src`, fail: true},
		{name: "windows unc", goos: "windows", cwd: `\\server\share\src\sub`, mount: `\\server\share\src`, want: `\\server\share\src`},
		{name: "windows unc relative", goos: "windows", cwd: `\\server\share\src\sub`, mount: `..\..`, want: `\\server\share\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Env{GOOS: tt.goos, Cwd: tt.cwd}
			got, err := mountRoot(e, tt.mount)
			if tt.fail {
				if err == nil {
					t.Fatalf("accepted mount %s from %s", tt.mount, tt.cwd)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("mount = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestStatePaths(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", "me")
	state := filepath.Join(base, "state")
	for _, tt := range []struct{ goos, home, base, want string }{
		{goos: "linux", home: home, want: filepath.Join(home, ".local", "state", "dx")},
		{goos: "darwin", home: home, base: state, want: filepath.Join(state, "dx")},
		{goos: "windows", home: `C:\Users\me`, base: `C:\Users\me\AppData\Local`, want: `C:\Users\me\AppData\Local\dx`},
		{goos: "windows", base: `\\server\share\state`, want: `\\server\share\state\dx`},
	} {
		t.Run(tt.want, func(t *testing.T) {
			filename := trust.DefaultPath(tt.goos, tt.home, func(string) string { return tt.base })
			if got := hostDir(filename, tt.goos); got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
			wantCache := filepath.Join(tt.want, "network.json")
			if tt.goos == "windows" {
				wantCache = tt.want + `\network.json`
			}
			if got := hostJoin(tt.goos, tt.want, "network.json"); got != wantCache {
				t.Fatalf("cache = %q, want %q", got, wantCache)
			}
		})
	}
}

func TestMainInjectedPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			e.GOOS = goos
			if goos == "windows" {
				e.Cwd, e.Home = `C:\Users\me\src`, `C:\Users\me`
			}
			e.LoadConfig = func(opts config.Options) (*config.Result, error) {
				if opts.GOOS != goos || opts.Cwd != e.Cwd || opts.Home != e.Home || opts.Trust == nil {
					t.Fatalf("config options = %#v", opts)
				}
				return &config.Result{Config: config.Config{}}, nil
			}
			e.FindRoot = func(cwd, home string) (string, error) { return cwd, nil }
			e.PresentMarkers = func(root string) ([]string, error) { return []string{"go.mod"}, nil }
			e.Args = []string{"go", "build", "-o", "hello", "."}
			if code := Main(e); code != 0 {
				t.Fatalf("code = %d: %s", code, stderr)
			}
			if f.warmCalls != 1 || !containsSequence(f.execSpec.Cmd, []string{"go", "build", "-o", "hello", "."}) {
				t.Fatalf("warm calls = %d, command = %v", f.warmCalls, f.execSpec.Cmd)
			}
			if envMap(f.execSpec.Env)["GOOS"] != goos {
				t.Fatalf("env = %v", f.execSpec.Env)
			}
		})
	}
}

func TestHostErrors(t *testing.T) {
	for _, name := range []string{"cwd", "config", "root", "markers", "identity read", "identity mkdir", "identity write", "socket", "recheck"} {
		t.Run(name, func(t *testing.T) {
			e, f, _, stderr := testEnv(t)
			failure := errors.New("injected failure")
			e.Args = []string{"go"}
			switch name {
			case "cwd":
				e.Err = failure
			case "config":
				e.LoadConfig = func(config.Options) (*config.Result, error) { return nil, failure }
			case "root":
				e.FindRoot = func(string, string) (string, error) { return "", failure }
			case "markers":
				e.PresentMarkers = func(string) ([]string, error) { return nil, failure }
			case "identity read":
				e.ReadFile = func(string) ([]byte, error) { return nil, failure }
			case "identity mkdir":
				put(t, filepath.Join(e.StateDir, "identity"), "not a directory")
			case "identity write":
				e.WriteFile = func(string, []byte, os.FileMode) error { return failure }
			case "socket":
				e.Args = []string{"--docker", "go"}
				f.socketErr = failure
			case "recheck":
				e.Args = []string{"doctor", "--recheck"}
				e.ForgetHostNetwork = func(string) error { return failure }
			}
			if code := Main(e); code != 125 || !strings.HasPrefix(stderr.String(), "dx: ") || f.args != nil {
				t.Fatalf("code = %d, stderr = %s, run = %v", code, stderr, f.args)
			}
		})
	}
}
