package shim

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCandidateNames(t *testing.T) {
	tests := []struct {
		name, tool, pathext, goos string
		want                      []string
	}{
		{"unix", "go", ".EXE;.BAT", "linux", []string{"go"}},
		{"unix extension", "go.exe", "", "darwin", []string{"go.exe"}},
		{"default", "go", "", "windows", []string{"go.COM", "go.EXE", "go.BAT", "go.CMD"}},
		{"ordered", "go", ".CMD;.exe;.COM", "windows", []string{"go.CMD", "go.exe", "go.COM"}},
		{"existing extension", "go.eXe", ".CMD;.EXE", "windows", []string{"go.eXe"}},
		{"default existing extension", "go.cmd", "", "windows", []string{"go.cmd"}},
		{"unknown extension", "go.test", ".EXE;.BAT", "windows", []string{"go.test.EXE", "go.test.BAT"}},
		{"empty extensions", "go", ";.EXE;;.CMD;", "windows", []string{"go.EXE", "go.CMD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := candidateNames(tt.tool, tt.pathext, tt.goos); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("candidateNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindLocal(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, Installer, string)
	}{
		{"shim dir", func(t *testing.T, i Installer, dir string) {
			writeTestFile(t, i.toolPath("go"), "not even a shim", 0755)
		}},
		{"symlink elsewhere", func(t *testing.T, i Installer, dir string) { testSymlink(t, i.Exe, filepath.Join(dir, "go")) }},
		{"copy elsewhere", func(t *testing.T, i Installer, dir string) {
			writeTestFile(t, filepath.Join(dir, "go"), "current dx binary", 0755)
		}},
		{"hard link elsewhere", func(t *testing.T, i Installer, dir string) {
			if err := os.Link(i.Exe, filepath.Join(dir, "go")); err != nil {
				t.Fatal(err)
			}
		}},
		{"non executable", func(t *testing.T, i Installer, dir string) {
			writeTestFile(t, filepath.Join(dir, "go"), "real tool", 0644)
		}},
		{"directory", func(t *testing.T, i Installer, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "go"), 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"stale existing symlink", func(t *testing.T, i Installer, dir string) {
			old := filepath.Join(t.TempDir(), "dx")
			writeTestFile(t, old, "old dx", 0755)
			testSymlink(t, old, filepath.Join(dir, "go"))
		}},
		{"missing candidate", func(t *testing.T, i Installer, dir string) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := testInstaller(t, "linux")
			first, real, last := t.TempDir(), t.TempDir(), t.TempDir()
			tt.setup(t, i, first)
			want := filepath.Join(real, "go")
			writeTestFile(t, want, "real tool", 0755)
			writeTestFile(t, filepath.Join(last, "go"), "later tool", 0755)
			pathEnv := strings.Join([]string{"", i.Dir + "/", first, "", real, last}, ":")
			got, err := FindLocal("go", pathEnv, "", "linux", i.Dir, i.Exe)
			if err != nil || got != want {
				t.Fatalf("FindLocal() = %q, %v, want %q", got, err, want)
			}
		})
	}
}

func TestFindLocalNotFound(t *testing.T) {
	i := testInstaller(t, "linux")
	if _, err := i.Install([]string{"go"}); err != nil {
		t.Fatal(err)
	}
	for _, pathEnv := range []string{"", "::", i.Dir, t.TempDir()} {
		got, err := FindLocal("go", pathEnv, "", "linux", i.Dir, i.Exe)
		if got != "" || !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "go") {
			t.Fatalf("FindLocal() = %q, %v, want ErrNotFound", got, err)
		}
	}
}

func TestFindLocalSymlinkedDir(t *testing.T) {
	i := testInstaller(t, "linux")
	if _, err := i.Install([]string{"go"}); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	testSymlink(t, i.Dir, alias)
	_, err := FindLocal("go", alias, "", "linux", i.Dir, i.Exe)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindLocal() error = %v, want ErrNotFound", err)
	}
}

func TestFindLocalWindows(t *testing.T) {
	i := testInstaller(t, "windows")
	first, second := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(first, "go.EXE"), "current dx binary", 0644)
	want := filepath.Join(first, "go.CMD")
	writeTestFile(t, want, "local tool", 0644)
	writeTestFile(t, filepath.Join(second, "go.EXE"), "later tool", 0644)
	got, err := FindLocal("go", first+";"+second, ".EXE;.CMD", "windows", i.Dir, i.Exe)
	if err != nil || got != want {
		t.Fatalf("FindLocal() = %q, %v, want %q", got, err, want)
	}
}
