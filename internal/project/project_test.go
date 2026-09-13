package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestFindRootBoundaries(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(home, "repo")
	dir := filepath.Join(repo, "src")
	outside := filepath.Join(base, "outside", "src")
	lookalike := filepath.Join(base, "home2", "src")
	root := base
	for filepath.Dir(root) != root {
		root = filepath.Dir(root)
	}

	tests := []struct {
		name    string
		dir     string
		home    string
		goos    string
		markers []string
		want    string
	}{
		{"marker in dir", dir, home, "linux", []string{filepath.Join(dir, "go.mod")}, dir},
		{"marker in parent", dir, home, "linux", []string{filepath.Join(repo, "package.json")}, repo},
		{"nearest beats marker priority", dir, home, "linux", []string{filepath.Join(dir, "pyproject.toml"), filepath.Join(repo, ".git")}, dir},
		{"home marker ignored", dir, home, "linux", []string{filepath.Join(home, ".git")}, dir},
		{"above home marker ignored", dir, home, "linux", []string{filepath.Join(base, ".git")}, dir},
		{"dir is home", home, home, "linux", []string{filepath.Join(base, ".git")}, home},
		{"outside home", outside, home, "linux", []string{filepath.Join(filepath.Dir(outside), "Cargo.toml")}, filepath.Dir(outside)},
		{"no markers", dir, home, "linux", nil, dir},
		{"empty home parent marker", filepath.Join(cwd, "src"), "", "linux", []string{filepath.Join(cwd, "go.mod")}, cwd},
		{"empty home no markers", filepath.Join(cwd, "src"), "", "linux", nil, filepath.Join(cwd, "src")},
		{"empty home root marker ignored", outside, "", "linux", []string{filepath.Join(root, ".git")}, outside},
		{"prefix lookalike", lookalike, home, "linux", []string{filepath.Join(base, "go.mod")}, base},
		{"root marker ignored", outside, home, "linux", []string{filepath.Join(root, ".git")}, outside},
		{"dir is root", root, home, "linux", nil, root},
		{"home is root", outside, root, "linux", []string{filepath.Join(root, ".git")}, outside},
		{"windows home case", dir, strings.ToUpper(home), "windows", []string{filepath.Join(home, ".git")}, dir},
		{"windows dir is home case", home, strings.ToUpper(home), "windows", []string{filepath.Join(base, ".git")}, home},
		{"unix home case", dir, strings.ToUpper(home), "linux", []string{filepath.Join(home, ".git")}, home},
		{"darwin home case", dir, strings.ToUpper(home), "darwin", []string{filepath.Join(home, ".git")}, home},
		{"clean paths", dir + string(filepath.Separator) + ".." + string(filepath.Separator) + "src", home + string(filepath.Separator) + ".", "linux", []string{filepath.Join(repo, "go.mod")}, repo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var checked []string
			got, err := findRoot(tt.dir, tt.home, tt.goos, func(name string) bool {
				checked = append(checked, name)
				for _, marker := range tt.markers {
					if name == marker {
						return true
					}
				}
				return false
			})
			if err != nil || got != tt.want {
				t.Fatalf("findRoot() = %q, %v; want %q, nil", got, err, tt.want)
			}
			for _, name := range checked {
				parent := filepath.Dir(name)
				isHome := parent == tt.home || tt.goos == "windows" && strings.EqualFold(parent, tt.home)
				if isHome || parent == root {
					t.Errorf("checked excluded boundary marker %q", name)
				}
			}
		})
	}
}

func TestFindRootFilesystem(t *testing.T) {
	tests := []struct {
		name      string
		marker    string
		directory bool
	}{
		{"git directory", ".git", true},
		{"git worktree file", ".git", false},
		{"go", "go.mod", false},
		{"node", "package.json", false},
		{"rust", "Cargo.toml", false},
		{"python", "pyproject.toml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			repo := filepath.Join(home, "repo")
			dir := filepath.Join(repo, "src")
			mkdir(t, dir)
			marker := filepath.Join(repo, tt.marker)
			if tt.directory {
				mkdir(t, marker)
			} else {
				writeFile(t, marker)
			}
			got, err := FindRoot(dir, home)
			if err != nil || got != repo {
				t.Fatalf("FindRoot() = %q, %v; want %q, nil", got, err, repo)
			}
		})
	}
}

func TestFindRootRelative(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, "repo", "src")
	for _, tt := range []struct {
		name string
		path string
	}{
		{"relative", dir},
		{"empty dir", cwd},
	} {
		t.Run(tt.name, func(t *testing.T) {
			relDir, err := filepath.Rel(cwd, tt.path)
			if err != nil {
				t.Skipf("different volumes: %v", err)
			}
			relHome, err := filepath.Rel(cwd, home)
			if err != nil {
				t.Skipf("different volumes: %v", err)
			}
			if tt.name == "empty dir" {
				relDir = ""
			}
			got, err := findRoot(relDir, relHome, runtime.GOOS, func(string) bool { return false })
			if err != nil || got != tt.path {
				t.Fatalf("findRoot() = %q, %v; want %q, nil", got, err, tt.path)
			}
		})
	}
}

func TestFindRootKeepsSymlinks(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "target")
	link := filepath.Join(home, "link")
	mkdir(t, filepath.Join(target, "src"))
	writeFile(t, filepath.Join(target, "go.mod"))
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := FindRoot(filepath.Join(link, "src"), home)
	if err != nil || got != link {
		t.Fatalf("FindRoot() = %q, %v; want lexical path %q, nil", got, err, link)
	}
}

func TestPresentMarkers(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		dirs    []string
		missing bool
		want    []string
	}{
		{"all ordered", []string{"pyproject.toml", "Cargo.toml", "package.json", "go.mod"}, []string{".git"}, false, []string{".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml"}},
		{"subset ordered", []string{"pyproject.toml", ".git", "package.json"}, nil, false, []string{".git", "package.json", "pyproject.toml"}},
		{"absent", []string{"README.md"}, nil, false, nil},
		{"not recursive", nil, []string{filepath.Join("child", ".git")}, false, nil},
		{"missing directory", nil, nil, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				writeFile(t, filepath.Join(dir, name))
			}
			for _, name := range tt.dirs {
				mkdir(t, filepath.Join(dir, name))
			}
			if tt.missing {
				dir = filepath.Join(dir, "missing")
			}
			got, err := PresentMarkers(dir)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("PresentMarkers() = %v, %v; want %v, nil", got, err, tt.want)
			}
		})
	}
}

func TestPresentMarkersError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "invalid\x00")
	_, err := PresentMarkers(dir)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("PresentMarkers() error = %v; want wrapped path error", err)
	}
	if pathErr.Path != filepath.Join(dir, ".git") || !strings.Contains(err.Error(), "stat") {
		t.Fatalf("error does not identify marker: %v", err)
	}
}

func mkdir(t *testing.T, name string) {
	t.Helper()
	if err := os.MkdirAll(name, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(name, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
