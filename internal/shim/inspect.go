package shim

import (
	"crypto/sha256"
	"debug/buildinfo"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const dxMainPath = "github.com/joshualawson/dx/cmd/dx"

func IsShim(path, exe string) (bool, error) {
	ok, _, err := inspect(path, exe, dxMainPath)
	return ok, err
}

func inspect(path, exe, mainPath string) (bool, bool, error) {
	linkInfo, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("inspect shim %q: %w", path, err)
	}
	isLink := linkInfo.Mode()&os.ModeSymlink != 0
	knownLink := false
	if isLink {
		target, err := os.Readlink(path)
		if err != nil {
			return false, false, fmt.Errorf("read shim %q: %w", path, err)
		}
		base := target[strings.LastIndexAny(target, `/\`)+1:]
		knownLink = base == "dx" || base == "dx.exe"
	}
	info, err := os.Stat(path)
	if err != nil {
		if knownLink {
			return true, true, nil
		}
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("stat shim %q: %w", path, err)
	}
	exeInfo, err := os.Stat(exe)
	if os.IsNotExist(err) {
		return knownLink, knownLink, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("stat dx executable %q: %w", exe, err)
	}
	if !info.Mode().IsRegular() || !exeInfo.Mode().IsRegular() {
		return knownLink, knownLink, nil
	}
	if os.SameFile(info, exeInfo) {
		return true, false, nil
	}
	if knownLink {
		return true, true, nil
	}
	if info.Size() == exeInfo.Size() {
		actual, err := fileHash(path)
		if err != nil {
			return false, false, err
		}
		expected, err := fileHash(exe)
		if err != nil {
			return false, false, err
		}
		if actual == expected {
			return true, isLink, nil
		}
	}
	// Embedded provenance survives upgrades that replace the original executable.
	ok := hasMainPath(path, mainPath)
	return ok, ok, nil
}

func hasMainPath(path, expected string) bool {
	info, err := buildinfo.ReadFile(path)
	return err == nil && info.Path == expected
}

func fileHash(path string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return sum, fmt.Errorf("open executable %q: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return sum, fmt.Errorf("hash executable %q: %w", path, err)
	}
	copy(sum[:], hash.Sum(nil))
	return sum, nil
}

type Entry struct {
	Tool  string
	Path  string
	Stale bool
}

func (i Installer) List() ([]Entry, error) {
	return i.list(dxMainPath)
}

func (i Installer) list(mainPath string) ([]Entry, error) {
	files, err := os.ReadDir(i.Dir)
	if os.IsNotExist(err) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list shims in %q: %w", i.Dir, err)
	}
	entries := make([]Entry, 0, len(files))
	for _, file := range files {
		path := filepath.Join(i.Dir, file.Name())
		ok, stale, err := inspect(path, i.Exe, mainPath)
		if err != nil {
			return nil, err
		}
		if ok {
			name := file.Name()
			if i.GOOS == "windows" && strings.HasSuffix(strings.ToLower(name), ".exe") {
				name = name[:len(name)-4]
			}
			entries = append(entries, Entry{Tool: name, Path: path, Stale: stale})
		}
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Tool < entries[b].Tool })
	return entries, nil
}
