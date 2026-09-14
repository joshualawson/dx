package shim

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIsShim(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, Installer, string)
		want  bool
	}{
		{"missing", func(t *testing.T, i Installer, path string) {}, false},
		{"exe itself", func(t *testing.T, i Installer, path string) {}, true},
		{"symlink", func(t *testing.T, i Installer, path string) { testSymlink(t, i.Exe, path) }, true},
		{"relative symlink", func(t *testing.T, i Installer, path string) {
			relative, err := filepath.Rel(filepath.Dir(path), i.Exe)
			if err != nil {
				t.Fatal(err)
			}
			testSymlink(t, relative, path)
		}, true},
		{"symlink chain", func(t *testing.T, i Installer, path string) {
			middle := filepath.Join(filepath.Dir(path), "middle")
			testSymlink(t, i.Exe, middle)
			testSymlink(t, middle, path)
		}, true},
		{"resolved non-dx name", func(t *testing.T, i Installer, path string) {
			if err := os.Rename(i.Exe, i.Exe+"-versioned"); err != nil {
				t.Fatal(err)
			}
			testSymlink(t, i.Exe+"-versioned", i.Exe)
			testSymlink(t, i.Exe+"-versioned", path)
		}, true},
		{"stale dx", func(t *testing.T, i Installer, path string) { testSymlink(t, "/somewhere/dx", path) }, true},
		{"stale dx exe", func(t *testing.T, i Installer, path string) { testSymlink(t, "/somewhere/dx.exe", path) }, true},
		{"other dx", func(t *testing.T, i Installer, path string) {
			old := filepath.Join(filepath.Dir(i.Exe), "old", "dx")
			writeTestFile(t, old, "old binary", 0755)
			testSymlink(t, old, path)
		}, true},
		{"hard link", func(t *testing.T, i Installer, path string) {
			if err := os.Link(i.Exe, path); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"copy", func(t *testing.T, i Installer, path string) { writeTestFile(t, path, "current dx binary", 0644) }, true},
		{"different size", func(t *testing.T, i Installer, path string) { writeTestFile(t, path, "different", 0755) }, false},
		{"different hash", func(t *testing.T, i Installer, path string) { writeTestFile(t, path, "current xx binary", 0755) }, false},
		{"matching prefix", func(t *testing.T, i Installer, path string) { writeTestFile(t, path, "current dx binarX", 0755) }, false},
		{"directory", func(t *testing.T, i Installer, path string) {
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"unrelated broken link", func(t *testing.T, i Installer, path string) { testSymlink(t, "/somewhere/other", path) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := testInstaller(t, "linux")
			path := filepath.Join(t.TempDir(), "go.exe")
			if tt.name == "exe itself" {
				path = i.Exe
			}
			tt.setup(t, i, path)
			got, err := IsShim(path, i.Exe)
			if err != nil || got != tt.want {
				t.Fatalf("IsShim() = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestList(t *testing.T) {
	i := testInstaller(t, "linux")
	if _, err := i.Install([]string{"npm", "go"}); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(filepath.Dir(i.Exe), "old", "dx")
	writeTestFile(t, old, "old binary", 0755)
	testSymlink(t, old, i.toolPath("stale"))
	testSymlink(t, "/somewhere/dx", i.toolPath("broken"))
	writeTestFile(t, i.toolPath("copy"), "current dx binary", 0755)
	if err := os.Link(i.Exe, i.toolPath("hardlink")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, i.toolPath("unrelated"), "not a shim", 0644)
	// Different content without Go provenance must remain untouched.
	writeTestFile(t, i.toolPath("changed-copy"), "current xx binary", 0755)
	want := []Entry{
		{Tool: "broken", Path: i.toolPath("broken"), Stale: true},
		{Tool: "copy", Path: i.toolPath("copy")},
		{Tool: "go", Path: i.toolPath("go")},
		{Tool: "hardlink", Path: i.toolPath("hardlink")},
		{Tool: "npm", Path: i.toolPath("npm")},
		{Tool: "stale", Path: i.toolPath("stale"), Stale: true},
	}
	got, err := i.List()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %+v, %v, want %+v", got, err, want)
	}
}

func TestListOldIdenticalSymlink(t *testing.T) {
	i := testInstaller(t, "linux")
	old := filepath.Join(t.TempDir(), "dx")
	writeTestFile(t, old, "current dx binary", 0755)
	if err := os.Mkdir(i.Dir, 0755); err != nil {
		t.Fatal(err)
	}
	testSymlink(t, old, i.toolPath("go"))
	got, err := i.List()
	if err != nil || len(got) != 1 || !got[0].Stale {
		t.Fatalf("List() = %+v, %v, want stale symlink", got, err)
	}
}
