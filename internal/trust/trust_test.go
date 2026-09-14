package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestNeedsApproval(t *testing.T) {
	tests := []struct {
		name string
		doc  map[string]any
		want bool
	}{
		{"nil", nil, false},
		{"empty", map[string]any{}, false},
		{"root true", map[string]any{"root": true}, true},
		{"root false", map[string]any{"root": false}, false},
		{"root true append", map[string]any{"root+": true}, true},
		{"root false append", map[string]any{"root+": false}, false},
		{"root null", map[string]any{"root": nil}, true},
		{"root string", map[string]any{"root": "false"}, true},
		{"version", toolDoc("version", "1.25"), false},
		{"local", map[string]any{"local": []any{"go", "npm"}}, false},
		{"local append", map[string]any{"local+": []any{"cargo"}}, false},
		{"local root false and version", map[string]any{"local": []any{"go"}, "root": false, "tools": map[string]any{"go": map[string]any{"version": "1.25"}}}, false},
		{"local with env", map[string]any{"local": []any{"go"}, "env": map[string]any{"FOO": "bar"}}, true},
		{"version append", toolDoc("version+", []any{"1.25"}), false},
		{"empty tools", map[string]any{"tools": map[string]any{}}, false},
		{"empty tool", map[string]any{"tools": map[string]any{"go": map[string]any{}}}, false},
		{"image", toolDoc("image", "evil/go"), true},
		{"warm", toolDoc("warm", false), true},
		{"env", map[string]any{"env": map[string]any{"FOO": "bar"}}, true},
		{"empty env", map[string]any{"env": map[string]any{}}, true},
		{"docker", map[string]any{"docker": true}, true},
		{"docker false", map[string]any{"docker": false}, true},
		{"credentials", map[string]any{"credentials": false}, true},
		{"credentials append", map[string]any{"credentials+": []any{"ssh"}}, true},
		{"unknown", map[string]any{"surprise": nil}, true},
		{"unknown tool setting", toolDoc("surprise", true), true},
		{"malformed tools", map[string]any{"tools": "evil"}, true},
		{"malformed tool", map[string]any{"tools": map[string]any{"go": "evil"}}, true},
		{"dotted impostor", map[string]any{"tools.go.version": "1.25"}, true},
		{"all safe append keys", map[string]any{"root+": false, "tools+": map[string]any{"go+": map[string]any{"version+": "1.25"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsApproval(tt.doc); got != tt.want {
				t.Fatalf("NeedsApproval(%v) = %v, want %v", tt.doc, got, tt.want)
			}
		})
	}
}

func toolDoc(setting string, value any) map[string]any {
	return map[string]any{"tools": map[string]any{"go": map[string]any{setting: value}}}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name string
		doc  map[string]any
		want []string
	}{
		{"root true", map[string]any{"root": true}, []string{"root = true"}},
		{"root false", map[string]any{"root": false}, nil},
		{"root true append", map[string]any{"root+": true}, []string{"root = true"}},
		{"root false append", map[string]any{"root+": false}, nil},
		{"safe", map[string]any{"root": false, "tools": map[string]any{"go": map[string]any{"version": "1.25"}}}, nil},
		{"local omitted", map[string]any{"local": []any{"go"}, "local+": []any{"npm"}}, nil},
		{"local with env", map[string]any{"local": []any{"go"}, "env": map[string]any{"FOO": "bar"}}, []string{"env.FOO = bar"}},
		{"root and version", map[string]any{"root": true, "tools": map[string]any{"go": map[string]any{"version": "1.25"}}}, []string{"root = true"}},
		{"nested and sorted", map[string]any{
			"root":        true,
			"tools":       map[string]any{"go": map[string]any{"version": "1.25", "image": "evil/go", "warm": false}},
			"env":         map[string]any{"Z": 42, "FOO": "bar"},
			"docker":      true,
			"credentials": map[string]any{"ssh": false},
		}, []string{"credentials.ssh = false", "docker = true", "env.FOO = bar", "env.Z = 42", "root = true", "tools.go.image = evil/go", "tools.go.warm = false"}},
		{"append and lists", map[string]any{"credentials+": []any{"ssh", "aws"}, "env+": map[string]any{"FOO+": "bar"}, "unknown": []string{"a", "b"}}, []string{"credentials = [ssh, aws]", "env.FOO = bar", "unknown = [a, b]"}},
		{"empty values", map[string]any{"env": map[string]any{}, "credentials": []any{}, "unknown": nil}, []string{"credentials = []", "env = {}", "unknown = null"}},
		{"list of maps", map[string]any{"unknown": []any{map[string]any{"z": true, "a": []any{1, 2}}}}, []string{"unknown = [{a: [1, 2], z: true}]"}},
		{"control characters", map[string]any{"env": map[string]any{"FOO": "bar\nbaz\x1b"}}, []string{`env.FOO = "bar\nbaz\x1b"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Describe(tt.doc); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Describe() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDefaultPath(t *testing.T) {
	tests := []struct {
		name, goos, home string
		env              map[string]string
		want             string
	}{
		{"linux default", "linux", filepath.Join("home", "user"), nil, filepath.Join("home", "user", ".local", "state", "dx", "trust.json")},
		{"darwin default", "darwin", filepath.Join("Users", "user"), nil, filepath.Join("Users", "user", ".local", "state", "dx", "trust.json")},
		{"xdg", "linux", "unused", map[string]string{"XDG_STATE_HOME": filepath.Join("state", "custom")}, filepath.Join("state", "custom", "dx", "trust.json")},
		{"darwin xdg", "darwin", "unused", map[string]string{"XDG_STATE_HOME": "state"}, filepath.Join("state", "dx", "trust.json")},
		{"windows", "windows", `C:\Users\user`, map[string]string{"LOCALAPPDATA": `C:\Users\user\AppData\Local`, "XDG_STATE_HOME": "ignored", "APPDATA": "ignored"}, `C:\Users\user\AppData\Local\dx\trust.json`},
		{"windows forward slashes", "windows", "unused", map[string]string{"LOCALAPPDATA": "C:/Users/user/AppData/Local"}, `C:\Users\user\AppData\Local\dx\trust.json`},
		{"windows trailing slash", "windows", "unused", map[string]string{"LOCALAPPDATA": `C:\Local\`}, `C:\Local\dx\trust.json`},
		{"windows empty env", "windows", "unused", nil, `dx\trust.json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultPath(tt.goos, tt.home, func(key string) string { return tt.env[key] }); got != tt.want {
				t.Fatalf("DefaultPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnderDir(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name, path string
		dirs       []string
		goos       string
		want       bool
	}{
		{"child", filepath.Join(base, "repo", ".dx.yaml"), []string{base}, runtime.GOOS, true},
		{"equal", base, []string{base}, runtime.GOOS, true},
		{"prefix lookalike", base + "-other", []string{base}, runtime.GOOS, false},
		{"clean", filepath.Join(base, "repo") + string(filepath.Separator) + "..", []string{base}, runtime.GOOS, true},
		{"escape", base + string(filepath.Separator) + "..", []string{base}, runtime.GOOS, false},
		{"relative", filepath.Join("repo", ".dx.yaml"), []string{"."}, runtime.GOOS, true},
		{"no dirs", base, nil, runtime.GOOS, false},
		{"unix case sensitive", "/a/B/.dx.yaml", []string{"/a/b"}, "linux", false},
		{"multiple dirs", base, []string{base + "-other", base}, runtime.GOOS, true},
		{"windows case", `C:\SRC\Project\.dx.yaml`, []string{`c:\src\project`}, "windows", true},
		{"windows separators", `C:/src/project/.dx.yaml`, []string{`c:\src\project\`}, "windows", true},
		{"windows prefix lookalike", `C:\src\project-other\.dx.yaml`, []string{`c:\src\project`}, "windows", false},
		{"windows clean", `C:\src\temp\..\project\.dx.yaml`, []string{`c:\src\project`}, "windows", true},
		{"windows escape", `C:\src\project\..\.dx.yaml`, []string{`c:\src\project`}, "windows", false},
		{"windows root", `C:\..\src\.dx.yaml`, []string{`c:\`}, "windows", true},
		{"windows other drive", `D:\src\.dx.yaml`, []string{`c:\src`}, "windows", false},
		{"windows unc", `\\SERVER\Share\Repo\.dx.yaml`, []string{`\\server\share\repo`}, "windows", true},
		{"windows unc root", `\\SERVER\Share\..\Repo\.dx.yaml`, []string{`\\server\share`}, "windows", true},
		{"windows unc other share", `\\server\share2\repo\.dx.yaml`, []string{`\\server\share`}, "windows", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UnderDir(tt.path, tt.dirs, tt.goos); got != tt.want {
				t.Fatalf("UnderDir(%q, %q, %q) = %v, want %v", tt.path, tt.dirs, tt.goos, got, tt.want)
			}
		})
	}
}

func TestIsTrustedWithoutApproval(t *testing.T) {
	tests := []struct {
		name          string
		doc           map[string]any
		trusted, want bool
	}{
		{"version", toolDoc("version", "1.25"), false, true},
		{"local", map[string]any{"local": []any{"go"}}, false, true},
		{"root true", map[string]any{"root": true}, false, false},
		{"root false", map[string]any{"root": false}, false, true},
		{"missing store", toolDoc("image", "evil/go"), false, false},
		{"trusted directory", toolDoc("image", "evil/go"), true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			s := &Store{Path: filepath.Join(base, "state", "trust.json")}
			var dirs []string
			if tt.trusted {
				dirs = []string{base}
			}
			got, err := s.IsTrusted(filepath.Join(base, ".dx.yaml"), nil, tt.doc, dirs)
			if err != nil || got != tt.want {
				t.Fatalf("IsTrusted() = %v, %v, want %v, nil", got, err, tt.want)
			}
			if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
				t.Fatalf("read created store: %v", err)
			}
		})
	}
}

func TestStoreLifecycle(t *testing.T) {
	// Relative approvals must share a volume with the working directory.
	t.Chdir(t.TempDir())
	base := mustWorkingDir(t)
	s := &Store{Path: filepath.Join(base, "state", "dx", "trust.json")}
	path := filepath.Join(base, ".dx.yaml")
	other := filepath.Join(base, "other", ".dx.yaml")
	content := []byte("docker: true\n")
	doc := map[string]any{"docker": true}
	relative, err := filepath.Rel(mustWorkingDir(t), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(relative, content); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(other, content); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]map[string]string
	if err := json.Unmarshal(persisted, &stored); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if got := stored["approvals"][path]; got != hex.EncodeToString(sum[:]) {
		t.Fatalf("stored hash = %q", got)
	}
	s = &Store{Path: s.Path}
	tests := []struct {
		name, path string
		content    []byte
		want       bool
	}{
		{"approved", path, content, true},
		{"relative approved", relative, content, true},
		{"changed content", path, []byte("docker: false\n"), false},
		{"changed whitespace", path, append(append([]byte{}, content...), '\n'), false},
		{"different path", filepath.Join(base, "unapproved", ".dx.yaml"), content, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := s.IsTrusted(tt.path, tt.content, doc, nil); err != nil || got != tt.want {
				t.Fatalf("IsTrusted() = %v, %v, want %v, nil", got, err, tt.want)
			}
		})
	}
	if err := s.Revoke(relative); err != nil {
		t.Fatal(err)
	}
	if got, err := s.IsTrusted(path, content, doc, nil); err != nil || got {
		t.Fatalf("revoked IsTrusted() = %v, %v", got, err)
	}
	if got, err := s.IsTrusted(other, content, doc, nil); err != nil || !got {
		t.Fatalf("other IsTrusted() = %v, %v", got, err)
	}
	if err := s.Revoke(path); err != nil {
		t.Fatalf("repeat revoke: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(s.Path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "trust.json" {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func mustWorkingDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestCorruptStore(t *testing.T) {
	for _, content := range []string{"{broken", "", `{"approvals": []}`, `{"approvals": {"/a": 42}}`, `{"approvals": {}} trailing`} {
		t.Run(content, func(t *testing.T) {
			base := t.TempDir()
			s := &Store{Path: filepath.Join(base, "trust.json")}
			if err := os.WriteFile(s.Path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(base, ".dx.yaml")
			_, err := s.IsTrusted(path, nil, toolDoc("image", "evil/go"), nil)
			for _, err := range []error{err, s.Approve(path, nil), s.Revoke(path)} {
				if err == nil || !strings.Contains(err.Error(), strconv.Quote(s.Path)) {
					t.Fatalf("expected error naming %q, got %v", s.Path, err)
				}
			}
			if got, err := s.IsTrusted(path, nil, toolDoc("version", "1.25"), nil); err != nil || !got {
				t.Fatalf("safe doc consulted corrupt store: %v, %v", got, err)
			}
			if got, err := s.IsTrusted(path, nil, toolDoc("image", "evil/go"), []string{base}); err != nil || !got {
				t.Fatalf("trusted directory consulted corrupt store: %v, %v", got, err)
			}
			if got, _ := os.ReadFile(s.Path); string(got) != content {
				t.Fatal("corrupt store was overwritten")
			}
		})
	}
}

func TestStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not supported on Windows")
	}
	for _, operation := range []string{"approve", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			base := t.TempDir()
			s := &Store{Path: filepath.Join(base, "state", "dx", "trust.json")}
			path := filepath.Join(base, ".dx.yaml")
			var err error
			if operation == "approve" {
				err = s.Approve(path, nil)
			} else {
				err = s.Revoke(path)
			}
			if err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]os.FileMode{s.Path: 0600, filepath.Dir(s.Path): 0700, filepath.Join(base, "state"): 0700} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != want {
					t.Errorf("%s permissions = %o, want %o", path, got, want)
				}
			}
			if err := os.Chmod(s.Path, 0644); err != nil {
				t.Fatal(err)
			}
			if err := s.Approve(path, nil); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(s.Path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("replacement permissions: %v, %v", info, err)
			}
		})
	}
}

func TestStoreIOErrors(t *testing.T) {
	for _, operation := range []string{"read", "approve", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			base := t.TempDir()
			s := &Store{Path: base}
			path := filepath.Join(base, ".dx.yaml")
			var err error
			switch operation {
			case "read":
				_, err = s.IsTrusted(path, nil, toolDoc("image", "evil/go"), nil)
			case "approve":
				err = s.Approve(path, nil)
			case "revoke":
				err = s.Revoke(path)
			}
			if err == nil || !strings.Contains(err.Error(), strconv.Quote(s.Path)) {
				t.Fatalf("expected error naming %q, got %v", s.Path, err)
			}
		})
	}
}
