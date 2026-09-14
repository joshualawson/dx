package shim

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("no local executable found")

func candidateNames(tool, pathext, goos string) []string {
	if goos != "windows" {
		return []string{tool}
	}
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	extensions := strings.Split(pathext, ";")
	for _, extension := range extensions {
		if extension != "" && strings.EqualFold(filepath.Ext(tool), extension) {
			return []string{tool}
		}
	}
	names := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		if extension != "" {
			names = append(names, tool+extension)
		}
	}
	return names
}

func FindLocal(tool, pathEnv, pathext, goos, dir, exe string) (string, error) {
	names := candidateNames(tool, pathext, goos)
	for _, entry := range pathEntries(pathEnv, goos) {
		if entry == "" || (dir != "" && samePath(entry, dir, goos)) {
			continue
		}
		for _, name := range names {
			candidate := filepath.Join(entry, name)
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if goos != "windows" && info.Mode().Perm()&0111 == 0 {
				continue
			}
			ok, err := IsShim(candidate, exe)
			if err != nil {
				return "", err
			}
			if !ok {
				// Explicit absolute paths prevent exec.Command from searching PATH again.
				absolute, err := filepath.Abs(candidate)
				if err != nil {
					return "", fmt.Errorf("resolve local executable %q: %w", candidate, err)
				}
				return absolute, nil
			}
		}
	}
	return "", fmt.Errorf("find local executable %q: %w", tool, ErrNotFound)
}
