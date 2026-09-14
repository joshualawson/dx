package shim

import (
	"debug/buildinfo"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testBinaryProvenance(t *testing.T) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return exe, info.Path
}

func TestHasMainPath(t *testing.T) {
	exe, mainPath := testBinaryProvenance(t)
	copyPath := filepath.Join(t.TempDir(), "tool.exe")
	if err := copyExecutable(exe, copyPath); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(t.TempDir(), "plain")
	writeTestFile(t, plain, "not a Go binary", 0755)
	tests := []struct {
		name, path, expected string
		want                 bool
	}{
		{"matching copy", copyPath, mainPath, true},
		{"different main path", copyPath, mainPath + "/other", false},
		{"not Go", plain, mainPath, false},
		{"missing", filepath.Join(t.TempDir(), "missing"), mainPath, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMainPath(tt.path, tt.expected); got != tt.want {
				t.Fatalf("hasMainPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInspectProvenance(t *testing.T) {
	exe, mainPath := testBinaryProvenance(t)
	exeInfo, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                                          string
		sameSize, sameContent, sameFile, matchingPath bool
		wantShim, wantStale                           bool
	}{
		{"different size", false, false, false, true, true, true},
		{"different hash", true, false, false, true, true, true},
		{"identical copy", true, true, false, true, true, false},
		{"same file", true, true, true, true, true, false},
		{"unrelated different size", false, false, false, false, false, false},
		{"unrelated different hash", true, false, false, false, false, false},
		{"identical copy ignores provenance", true, true, false, false, true, false},
		{"same file ignores provenance", true, true, true, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := testInstaller(t, "windows")
			candidate := filepath.Join(t.TempDir(), "tool.exe")
			if err := copyExecutable(exe, candidate); err != nil {
				t.Fatal(err)
			}
			if tt.sameContent {
				i.Exe = exe
			} else if tt.sameSize {
				if err := os.Truncate(i.Exe, exeInfo.Size()); err != nil {
					t.Fatal(err)
				}
			}
			if tt.sameFile {
				candidate = exe
			}
			expected := mainPath
			if !tt.matchingPath {
				expected += "/other"
			}
			ok, stale, err := inspect(candidate, i.Exe, expected)
			if err != nil || ok != tt.wantShim || stale != tt.wantStale {
				t.Fatalf("inspect() = %v, %v, %v, want %v, %v", ok, stale, err, tt.wantShim, tt.wantStale)
			}
		})
	}
}

func TestInstallerProvenance(t *testing.T) {
	exe, mainPath := testBinaryProvenance(t)
	for _, goos := range []string{"linux", "windows"} {
		for _, kind := range []string{"copy", "hard link"} {
			for _, operation := range []string{"install", "uninstall named", "uninstall all"} {
				t.Run(goos+"/"+kind+"/"+operation, func(t *testing.T) {
					i := testInstaller(t, goos)
					old := filepath.Join(t.TempDir(), "previous.exe")
					if err := copyExecutable(exe, old); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(i.Dir, 0755); err != nil {
						t.Fatal(err)
					}
					candidate := i.toolPath("go")
					if kind == "copy" {
						if err := copyExecutable(old, candidate); err != nil {
							t.Fatal(err)
						}
					} else if err := os.Link(old, candidate); err != nil {
						t.Fatal(err)
					}
					entries, err := i.list(mainPath)
					want := []Entry{{Tool: "go", Path: candidate, Stale: true}}
					if err != nil || !reflect.DeepEqual(entries, want) {
						t.Fatalf("list() = %+v, %v, want %+v", entries, err, want)
					}
					var names []string
					switch operation {
					case "install":
						names, err = i.install([]string{"go"}, mainPath)
					case "uninstall named":
						names, err = i.uninstall([]string{"go"}, mainPath)
					case "uninstall all":
						names, err = i.uninstall(nil, mainPath)
					}
					if err != nil || !reflect.DeepEqual(names, []string{"go"}) {
						t.Fatalf("%s = %v, %v", operation, names, err)
					}
					if operation == "install" {
						entries, err := i.List()
						want[0].Stale = false
						if err != nil || !reflect.DeepEqual(entries, want) {
							t.Fatalf("List() after replacement = %+v, %v", entries, err)
						}
					} else if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
						t.Fatalf("shim was not removed: %v", err)
					}
				})
			}
		}
	}
}

func TestInstallerRefusesDifferentProvenance(t *testing.T) {
	exe, mainPath := testBinaryProvenance(t)
	if mainPath == dxMainPath {
		t.Fatal("test binary unexpectedly has dx provenance")
	}
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			i := testInstaller(t, goos)
			if err := os.Mkdir(i.Dir, 0755); err != nil {
				t.Fatal(err)
			}
			candidate := i.toolPath("go")
			if err := copyExecutable(exe, candidate); err != nil {
				t.Fatal(err)
			}
			before, err := fileHash(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := IsShim(candidate, i.Exe); err != nil || ok {
				t.Fatalf("IsShim() = %v, %v", ok, err)
			}
			if entries, err := i.List(); err != nil || len(entries) != 0 {
				t.Fatalf("List() = %+v, %v", entries, err)
			}
			for _, operation := range []func([]string) ([]string, error){i.Install, i.Uninstall} {
				if _, err := operation([]string{"go"}); err == nil || !strings.Contains(err.Error(), candidate) {
					t.Fatalf("expected refusal naming %q, got %v", candidate, err)
				}
			}
			if names, err := i.Uninstall(nil); err != nil || len(names) != 0 {
				t.Fatalf("Uninstall(all) = %v, %v", names, err)
			}
			if after, err := fileHash(candidate); err != nil || after != before {
				t.Fatalf("unrelated binary changed: %v", err)
			}
		})
	}
}
