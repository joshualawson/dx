package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func boolPtr(value bool) *bool { return &value }

func fixture(t *testing.T, files map[string]string, cwd string) (Options, string) {
	t.Helper()
	base := t.TempDir()
	if cwd == "" {
		cwd = "home/org/repo"
	}
	opts := Options{
		Cwd: filepath.Join(base, filepath.FromSlash(cwd)), Home: filepath.Join(base, "home"), GOOS: "linux",
		Getenv: func(key string) string {
			if key == "DX_CONFIG" {
				return filepath.Join(base, "global.yaml")
			}
			return ""
		},
	}
	for _, dir := range []string{opts.Cwd, opts.Home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		filename := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return opts, base
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		cwd     string
		goos    string
		check   func(*testing.T, *Result, string)
		sources []string
	}{
		{
			name: "defaults",
			check: func(t *testing.T, result *Result, base string) {
				want := Config{Tools: map[string]Tool{}, Env: map[string]string{}, Credentials: map[string]bool{"all": true}, Trusted: []string{}, IdleTimeout: Duration(30 * time.Minute)}
				if !reflect.DeepEqual(result.Config, want) {
					t.Fatalf("config = %#v, want %#v", result.Config, want)
				}
				wantOrigins := map[string]string{"root": "default", "docker": "default", "credentials.all": "default", "trusted": "default", "idle_timeout": "default"}
				if !reflect.DeepEqual(result.Origins, wantOrigins) {
					t.Errorf("origins = %#v, want %#v", result.Origins, wantOrigins)
				}
			},
		},
		{
			name:    "global only",
			files:   map[string]string{"global.yaml": "docker: true\nidle_timeout: 45m\ntrusted: ['~', ~/src, /srv/work, ~another]\ncredentials: {all: false, ssh: true}\ntools: {go: {image: custom/go, version: '1.25', warm: false}}\nenv: {GOPRIVATE: example.test/*}\n"},
			sources: []string{"global.yaml"},
			check: func(t *testing.T, result *Result, base string) {
				want := Config{
					Docker: true, IdleTimeout: Duration(45 * time.Minute), Credentials: map[string]bool{"all": false, "ssh": true},
					Tools:   map[string]Tool{"go": {Image: "custom/go", Version: "1.25", Warm: boolPtr(false)}},
					Env:     map[string]string{"GOPRIVATE": "example.test/*"},
					Trusted: []string{filepath.Join(base, "home"), filepath.Join(base, "home", "src"), "/srv/work", "~another"},
				}
				if !reflect.DeepEqual(result.Config, want) {
					t.Errorf("config = %#v, want %#v", result.Config, want)
				}
				assertOrigins(t, result, base, map[string]string{"docker": "global.yaml", "idle_timeout": "global.yaml", "tools.go.image": "global.yaml", "tools.go.version": "global.yaml", "tools.go.warm": "global.yaml", "env.GOPRIVATE": "global.yaml", "credentials.all": "global.yaml", "credentials.ssh": "global.yaml", "trusted": "global.yaml"})
			},
		},
		{
			name: "three layer recursive merge and scalar overrides",
			files: map[string]string{
				"global.yaml":            "docker: true\ntools: {go: {image: custom/go, version: '1.23', warm: true}, node: {version: '22'}}\nenv: {SHARED: global, GLOBAL: yes}\ncredentials: {all: false, git: true}\n",
				"home/org/.dx.yaml":      "docker: false\ntools: {go: {version: '1.24'}}\nenv: {SHARED: parent, PARENT: yes}\ncredentials: {ssh: true}\n",
				"home/org/repo/.dx.yaml": "tools: {go: {version: '1.25', warm: false}}\nenv: {SHARED: near, NEAR: yes}\ncredentials: {git: false}\n",
			},
			sources: []string{"global.yaml", "home/org/.dx.yaml", "home/org/repo/.dx.yaml"},
			check: func(t *testing.T, result *Result, base string) {
				cfg := result.Config
				if cfg.Docker || !reflect.DeepEqual(cfg.Tools, map[string]Tool{"go": {Image: "custom/go", Version: "1.25", Warm: boolPtr(false)}, "node": {Version: "22"}}) {
					t.Errorf("merged config = %#v", cfg)
				}
				if !reflect.DeepEqual(cfg.Env, map[string]string{"SHARED": "near", "GLOBAL": "yes", "PARENT": "yes", "NEAR": "yes"}) {
					t.Errorf("env = %#v", cfg.Env)
				}
				if !reflect.DeepEqual(cfg.Credentials, map[string]bool{"all": false, "git": false, "ssh": true}) {
					t.Errorf("credentials = %#v", cfg.Credentials)
				}
				assertOrigins(t, result, base, map[string]string{"docker": "home/org/.dx.yaml", "tools.go.image": "global.yaml", "tools.go.version": "home/org/repo/.dx.yaml", "tools.go.warm": "home/org/repo/.dx.yaml", "env.SHARED": "home/org/repo/.dx.yaml", "env.PARENT": "home/org/.dx.yaml", "env.GLOBAL": "global.yaml", "credentials.ssh": "home/org/.dx.yaml", "credentials.git": "home/org/repo/.dx.yaml"})
			},
		},
		{
			name:    "append global trusted to defaults",
			files:   map[string]string{"global.yaml": "trusted+: [~/src]\n"},
			sources: []string{"global.yaml"},
			check: func(t *testing.T, result *Result, base string) {
				if !reflect.DeepEqual(result.Config.Trusted, []string{filepath.Join(base, "home", "src")}) {
					t.Errorf("trusted = %#v", result.Config.Trusted)
				}
				assertOrigins(t, result, base, map[string]string{"trusted": "global.yaml"})
			},
		},
		{
			name:    "root prunes invalid farther project but keeps global",
			files:   map[string]string{"global.yaml": "env: {GLOBAL: kept}\nroot: true\n", "home/org/.dx.yaml": "unknown: [broken", "home/org/repo/.dx.yaml": "root: true\nenv: {PROJECT: kept}\n"},
			sources: []string{"global.yaml", "home/org/repo/.dx.yaml"},
			check: func(t *testing.T, result *Result, base string) {
				if !result.Config.Root || !reflect.DeepEqual(result.Config.Env, map[string]string{"GLOBAL": "kept", "PROJECT": "kept"}) {
					t.Errorf("config = %#v", result.Config)
				}
			},
		},
		{
			name:    "farther project root retains nearer layers",
			files:   map[string]string{"home/org/.dx.yaml": "root: true\ndocker: true\n", "home/org/repo/.dx.yaml": "docker: false\n"},
			sources: []string{"home/org/.dx.yaml", "home/org/repo/.dx.yaml"},
		},
		{
			name:    "global root does not prune projects",
			files:   map[string]string{"global.yaml": "root: true\n", "home/org/.dx.yaml": "env: {PARENT: kept}\n"},
			sources: []string{"global.yaml", "home/org/.dx.yaml"},
		},
		{
			name: "home config ignored", files: map[string]string{"home/.dx.yaml": "unknown: [broken"},
		},
		{
			name: "cwd at home ignores home config", cwd: "home", files: map[string]string{"home/.dx.yaml": "unknown: [broken"},
		},
		{
			name: "outside home walks beyond home depth", cwd: "outside/repo",
			files:   map[string]string{".dx.yaml": "root: true\n", "outside/.dx.yaml": "docker: true\n", "outside/repo/.dx.yaml": "env: {NEAR: kept}\n"},
			sources: []string{".dx.yaml", "outside/.dx.yaml", "outside/repo/.dx.yaml"},
		},
		{
			name: "home prefix is not containment", cwd: "home-other/repo",
			files:   map[string]string{".dx.yaml": "root: true\n", "home-other/.dx.yaml": "docker: true\n"},
			sources: []string{".dx.yaml", "home-other/.dx.yaml"},
		},
		{
			name: "empty files", files: map[string]string{"global.yaml": "", "home/org/.dx.yaml": "# empty\n", "home/org/repo/.dx.yaml": "---\n"},
			sources: []string{"global.yaml", "home/org/.dx.yaml", "home/org/repo/.dx.yaml"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts, base := fixture(t, test.files, test.cwd)
			if test.goos != "" {
				opts.GOOS = test.goos
			}
			result, err := Load(opts)
			if err != nil {
				t.Fatal(err)
			}
			var wantSources []Source
			for _, source := range test.sources {
				wantSources = append(wantSources, Source{Path: filepath.Join(base, filepath.FromSlash(source)), Global: source == "global.yaml"})
			}
			if !reflect.DeepEqual(result.Sources, wantSources) {
				t.Errorf("sources = %#v, want %#v", result.Sources, wantSources)
			}
			if test.check != nil {
				test.check(t, result, base)
			}
		})
	}
}

func assertOrigins(t *testing.T, result *Result, base string, want map[string]string) {
	t.Helper()
	for key, file := range want {
		if result.Origins[key] != filepath.Join(base, filepath.FromSlash(file)) {
			t.Errorf("origin[%q] = %q, want %q", key, result.Origins[key], file)
		}
	}
}

func errorNamesPath(err error, filename string) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, filename) || strings.Contains(text, strings.ReplaceAll(filename, `\`, `\\`))
}

func errorNamesSlashPath(err error, filename string) bool {
	return errorNamesPath(err, filepath.FromSlash(filename))
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name, content, key string
		global             bool
	}{
		{"unknown top level", "doccker: true\n", "doccker", false},
		{"unknown tool field", "tools: {go: {verison: '1.25'}}\n", "tools.go.verison", false},
		{"unknown credential", "credentials: {awss: true}\n", "credentials.awss", false},
		{"project trusted", "trusted: [~/src]\n", "trusted", false},
		{"project trusted append", "trusted+: [~/src]\n", "trusted+", false},
		{"invalid yaml", "tools:\n  go: [broken\n", "line", false},
		{"duplicate key", "docker: true\ndocker: false\n", "line", false},
		{"multiple docs", "docker: true\n---\ndocker: false\n", "multiple yaml documents", false},
		{"non map document", "[one, two]\n", "unmarshal", false},
		{"invalid map", "tools: [one, two]\n", "tools", false},
		{"invalid tool", "tools: {go: string}\n", "tools.go", false},
		{"invalid boolean", "docker: nonsense\n", "bool", false},
		{"invalid duration", "idle_timeout: forever\n", "duration", false},
		{"integer duration", "idle_timeout: 30\n", "duration", false},
		{"invalid env", "env: {BAD: [one, two]}\n", "string", false},
		{"append non list", "docker+: [true]\n", "non-list", false},
		{"append scalar", "trusted+: hello\n", "list", true},
		{"ambiguous append", "trusted: []\ntrusted+: []\n", "cannot appear together", true},
		{"numeric map key", "env: {1: value}\n", "string keys", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := "home/org/repo/.dx.yaml"
			if test.global {
				file = "global.yaml"
			}
			opts, _ := fixture(t, map[string]string{file: test.content}, "")
			result, err := Load(opts)
			if result != nil || err == nil || !errorNamesSlashPath(err, file) || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("Load = (%#v, %v), want error naming %s and %q", result, err, file, test.key)
			}
		})
	}
}

func TestInvalidFartherLayerCannotBeOverridden(t *testing.T) {
	opts, _ := fixture(t, map[string]string{"home/org/.dx.yaml": "idle_timeout: forever\n", "home/org/repo/.dx.yaml": "idle_timeout: 10m\n"}, "")
	if result, err := Load(opts); err == nil || result != nil {
		t.Fatalf("Load = (%#v, %v), want validation error", result, err)
	}
}

func TestReadError(t *testing.T) {
	opts, base := fixture(t, nil, "")
	name := filepath.Join(base, "global.yaml")
	if err := os.Mkdir(name, 0o755); err != nil {
		t.Fatal(err)
	}
	if result, err := Load(opts); err == nil || result != nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("Load = (%#v, %v), want read error naming %s", result, err, name)
	}
}

func TestCredentialEnabled(t *testing.T) {
	for _, test := range []struct {
		name        string
		credentials map[string]bool
		want        bool
	}{
		{"nil defaults on", nil, true},
		{"empty defaults on", map[string]bool{}, true},
		{"all on", map[string]bool{"all": true}, true},
		{"all off", map[string]bool{"all": false}, false},
		{"named on beats all off", map[string]bool{"all": false, "ssh": true}, true},
		{"named off beats all on", map[string]bool{"all": true, "ssh": false}, false},
		{"named off beats default", map[string]bool{"ssh": false}, false},
		{"other named does not affect default", map[string]bool{"aws": false}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := (Config{Credentials: test.credentials}).CredentialEnabled("ssh"); got != test.want {
				t.Errorf("CredentialEnabled = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	for _, test := range []struct {
		input string
		want  time.Duration
		bad   bool
	}{
		{"30m", 30 * time.Minute, false}, {"1h15m2s", time.Hour + 15*time.Minute + 2*time.Second, false},
		{"0s", 0, false}, {"-1s", -time.Second, false}, {"1.5s", 1500 * time.Millisecond, false},
		{"nonsense", 0, true}, {"30", 0, true}, {"[]", 0, true},
	} {
		t.Run(test.input, func(t *testing.T) {
			var value Duration
			err := yaml.Unmarshal([]byte(test.input), &value)
			if (err != nil) != test.bad || (!test.bad && time.Duration(value) != test.want) {
				t.Fatalf("duration = %v, error = %v", time.Duration(value), err)
			}
			if !test.bad {
				encoded, err := yaml.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var roundtrip Duration
				if err := yaml.Unmarshal(encoded, &roundtrip); err != nil || roundtrip != value {
					t.Errorf("round trip = %v, %v", roundtrip, err)
				}
			}
		})
	}
}

type trustCall struct {
	path    string
	content []byte
	doc     map[string]any
	dirs    []string
}

type fakeTrust struct {
	calls   []trustCall
	trusted map[string]bool
	err     error
}

func (f *fakeTrust) IsTrusted(path string, content []byte, doc map[string]any, dirs []string) (bool, error) {
	f.calls = append(f.calls, trustCall{path, content, doc, append([]string{}, dirs...)})
	return f.trusted[path], f.err
}

func TestTrust(t *testing.T) {
	for _, test := range []struct {
		name                                string
		approveNear, approveFar, root, fail bool
	}{
		{name: "all untrusted"}, {name: "all approved", approveNear: true, approveFar: true},
		{name: "one untrusted", approveNear: true}, {name: "pruned", root: true}, {name: "checker error", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			nearContent := "tools: {go: {version: '1.25'}}\n"
			if test.root {
				nearContent += "root: true\n"
			}
			opts, base := fixture(t, map[string]string{
				"global.yaml": "trusted+: ['~', ~/work, /srv/work]\n", "home/org/.dx.yaml": "docker: true\n", "home/org/repo/.dx.yaml": nearContent,
			}, "")
			near, far := filepath.Join(opts.Cwd, ".dx.yaml"), filepath.Join(base, "home", "org", ".dx.yaml")
			checker := &fakeTrust{trusted: map[string]bool{near: test.approveNear, far: test.approveFar}}
			sentinel := errors.New("approval store unavailable")
			if test.fail {
				checker.err = sentinel
			}
			opts.Trust = checker
			result, err := Load(opts)
			if test.fail {
				if !errors.Is(err, sentinel) || result != nil || (!errorNamesPath(err, near) && !errorNamesSlashPath(err, "home/org/repo/.dx.yaml")) {
					t.Fatalf("Load = (%#v, %v), want wrapped checker error", result, err)
				}
				return
			}
			wantPaths := []string{near}
			if !test.root {
				wantPaths = append(wantPaths, far)
			}
			if len(checker.calls) != len(wantPaths) {
				t.Fatalf("trust calls = %d, want %d", len(checker.calls), len(wantPaths))
			}
			var untrusted []string
			for i, wantPath := range wantPaths {
				call := checker.calls[i]
				wantDirs := []string{opts.Home, filepath.Join(opts.Home, "work"), "/srv/work"}
				if call.path != wantPath || !reflect.DeepEqual(call.dirs, wantDirs) {
					t.Errorf("call = %#v, want path %q and dirs %#v", call, wantPath, wantDirs)
				}
				content, readErr := os.ReadFile(wantPath)
				if readErr != nil || string(content) != string(call.content) {
					t.Errorf("raw content mismatch: %v", readErr)
				}
				var doc map[string]any
				if err := yaml.Unmarshal(content, &doc); err != nil || !reflect.DeepEqual(doc, call.doc) {
					t.Errorf("parsed doc mismatch: %v", err)
				}
				if !checker.trusted[wantPath] {
					untrusted = append(untrusted, wantPath)
				}
			}
			if len(untrusted) != 0 {
				var trustErr *UntrustedError
				if result != nil || !errors.As(err, &trustErr) || !reflect.DeepEqual(trustErr.Paths, untrusted) {
					t.Fatalf("Load = (%#v, %v), want untrusted %v", result, err, untrusted)
				}
				for _, file := range untrusted {
					if !strings.Contains(err.Error(), file) {
						t.Errorf("error %q does not name %q", err, file)
					}
				}
			} else if err != nil || result == nil {
				t.Fatalf("Load = (%#v, %v), want approved config", result, err)
			}
		})
	}
}
