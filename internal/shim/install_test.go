package shim

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func testSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func testInstaller(t *testing.T, goos string) Installer {
	t.Helper()
	root := t.TempDir()
	exe := filepath.Join(root, "current", "dx")
	writeTestFile(t, exe, "current dx binary", 0755)
	return Installer{Dir: filepath.Join(root, "shims"), Exe: exe, GOOS: goos}
}

func TestInstall(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, Installer)
	}{
		{"new", func(t *testing.T, i Installer) {}},
		{"current symlink", func(t *testing.T, i Installer) { testSymlink(t, i.Exe, i.toolPath("go")) }},
		{"stale symlink", func(t *testing.T, i Installer) { testSymlink(t, "/somewhere/dx", i.toolPath("go")) }},
		{"old dx file", func(t *testing.T, i Installer) {
			old := filepath.Join(filepath.Dir(i.Exe), "old", "dx")
			writeTestFile(t, old, "older binary", 0755)
			testSymlink(t, old, i.toolPath("go"))
		}},
		{"identical copy", func(t *testing.T, i Installer) { writeTestFile(t, i.toolPath("go"), "current dx binary", 0755) }},
		{"hard link", func(t *testing.T, i Installer) {
			if err := os.Link(i.Exe, i.toolPath("go")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := testInstaller(t, "linux")
			if tt.name != "new" {
				if err := os.Mkdir(i.Dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			tt.setup(t, i)
			got, err := i.Install([]string{"npm", "go", "go", "g++", "python3.13"})
			want := []string{"g++", "go", "npm", "python3.13"}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("Install() = %v, %v, want %v", got, err, want)
			}
			for _, tool := range want {
				target, err := os.Readlink(i.toolPath(tool))
				if err != nil || target != i.Exe {
					t.Fatalf("shim %s target = %q, %v", tool, target, err)
				}
			}
			files, err := os.ReadDir(i.Dir)
			if err != nil || len(files) != len(want) {
				t.Fatalf("staging files left behind: %v, %v", files, err)
			}
		})
	}
}

func TestInstallInvalid(t *testing.T) {
	for _, name := range []string{"", "dx", ".", "..", "../go", "a/b", `a\b`, "_go", "-go", "go tool", "go\n", "go:tool", "gö"} {
		t.Run(name, func(t *testing.T) {
			i := testInstaller(t, "linux")
			if _, err := i.Install([]string{"valid", name}); err == nil || !strings.Contains(err.Error(), "invalid shim tool") {
				t.Fatalf("Install() error = %v", err)
			}
			if _, err := os.Lstat(i.Dir); !os.IsNotExist(err) {
				t.Fatalf("invalid tools created folder: %v", err)
			}
		})
	}
	for _, tools := range [][]string{nil, {}, {"DX"}, {"dx.exe"}, {"Dx.ExE"}} {
		t.Run(strings.Join(tools, ","), func(t *testing.T) {
			i := testInstaller(t, "windows")
			if _, err := i.Install(tools); err == nil {
				t.Fatal("Install() succeeded")
			}
			if _, err := os.Lstat(i.Dir); !os.IsNotExist(err) {
				t.Fatalf("invalid tools created folder: %v", err)
			}
		})
	}
}

func TestInstallRefusesNonShim(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			i := testInstaller(t, "linux")
			path := i.toolPath("go")
			writeTestFile(t, filepath.Join(i.Dir, "keep"), "keep", 0644)
			switch kind {
			case "file":
				writeTestFile(t, path, "real local tool", 0755)
			case "directory":
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				testSymlink(t, filepath.Join(i.Dir, "keep"), path)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := i.Install([]string{"go"}); err == nil || !strings.Contains(err.Error(), strconv.Quote(path)) {
				t.Fatalf("Install() error = %v", err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("non-shim changed: %v", err)
			}
		})
	}
}

func TestWindowsInstall(t *testing.T) {
	i := testInstaller(t, "windows")
	for _, tools := range [][]string{{"npm", "go"}, {"go"}} {
		if _, err := i.Install(tools); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"go", "npm"} {
		info, err := os.Lstat(i.toolPath(tool))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("Windows shim = %v, %v", info, err)
		}
		exeInfo, err := os.Stat(i.Exe)
		if err != nil || !os.SameFile(info, exeInfo) {
			t.Fatalf("expected hard link: %v", err)
		}
	}
	entries, err := i.List()
	if err != nil || len(entries) != 2 || entries[0].Tool != "go" || entries[1].Tool != "npm" {
		t.Fatalf("List() = %v, %v", entries, err)
	}
	removed, err := i.Uninstall([]string{"go"})
	if err != nil || !reflect.DeepEqual(removed, []string{"go"}) {
		t.Fatalf("Uninstall() = %v, %v", removed, err)
	}
}

func TestLinkOrCopyFallback(t *testing.T) {
	i := testInstaller(t, "windows")
	destination := filepath.Join(t.TempDir(), "go.exe")
	called := false
	link := func(source, target string) error {
		called = true
		if source != i.Exe || target != destination {
			t.Fatalf("link arguments = %q, %q", source, target)
		}
		return errors.New("different volume")
	}
	if err := linkOrCopy(i.Exe, destination, link); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("hard link was not attempted first")
	}
	ok, err := IsShim(destination, i.Exe)
	if err != nil || !ok {
		t.Fatalf("fallback copy IsShim() = %v, %v", ok, err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	exeInfo, err := os.Stat(i.Exe)
	if err != nil || os.SameFile(info, exeInfo) {
		t.Fatalf("fallback did not create a separate copy: %v", err)
	}
}

func TestCopyExecutable(t *testing.T) {
	i := testInstaller(t, "windows")
	if err := os.Chmod(i.Exe, 0751); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "go.exe")
	if err := copyExecutable(i.Exe, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "current dx binary" {
		t.Fatalf("copied content = %q, %v", got, err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0751 {
		t.Fatalf("copied mode = %o, want 751", info.Mode().Perm())
	}
	if err := copyExecutable(i.Exe, destination); err == nil {
		t.Fatal("copy overwrote an existing destination")
	}
}

func TestUninstall(t *testing.T) {
	tests := []struct {
		name                   string
		tools, want, remaining []string
	}{
		{"all", nil, []string{"go", "npm", "stale"}, nil},
		{"named", []string{"npm", "absent", "npm"}, []string{"npm"}, []string{"go", "stale"}},
		{"stale", []string{"stale"}, []string{"stale"}, []string{"go", "npm"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := testInstaller(t, "linux")
			if _, err := i.Install([]string{"npm", "go"}); err != nil {
				t.Fatal(err)
			}
			testSymlink(t, "/somewhere/dx", i.toolPath("stale"))
			writeTestFile(t, filepath.Join(i.Dir, "keep"), "not dx", 0644)
			got, err := i.Uninstall(tt.tools)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Uninstall() = %v, %v, want %v", got, err, tt.want)
			}
			entries, err := i.List()
			if err != nil {
				t.Fatal(err)
			}
			var remaining []string
			for _, entry := range entries {
				remaining = append(remaining, entry.Tool)
			}
			if !reflect.DeepEqual(remaining, tt.remaining) {
				t.Fatalf("remaining = %v, want %v", remaining, tt.remaining)
			}
			if data, err := os.ReadFile(filepath.Join(i.Dir, "keep")); err != nil || string(data) != "not dx" {
				t.Fatalf("non-shim removed: %q, %v", data, err)
			}
		})
	}
}

func TestUninstallRefusesNonShim(t *testing.T) {
	i := testInstaller(t, "linux")
	path := i.toolPath("go")
	writeTestFile(t, path, "not dx", 0755)
	if _, err := i.Uninstall([]string{"go"}); err == nil || !strings.Contains(err.Error(), strconv.Quote(path)) {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "not dx" {
		t.Fatalf("non-shim removed: %q, %v", data, err)
	}
	if _, err := i.Uninstall([]string{"../current/dx"}); err == nil {
		t.Fatal("Uninstall() accepted traversal")
	}
}

func TestMissingDir(t *testing.T) {
	i := testInstaller(t, "linux")
	for _, tools := range [][]string{nil, {"go"}} {
		got, err := i.Uninstall(tools)
		if err != nil || len(got) != 0 {
			t.Fatalf("Uninstall() = %v, %v", got, err)
		}
	}
	if entries, err := i.List(); err != nil || len(entries) != 0 {
		t.Fatalf("List() = %v, %v", entries, err)
	}
	if _, err := os.Stat(i.Dir); !os.IsNotExist(err) {
		t.Fatalf("missing folder was created: %v", err)
	}
}
