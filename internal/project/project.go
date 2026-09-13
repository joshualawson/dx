package project

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var Markers = []string{".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml"}

func FindRoot(dir, home string) (string, error) {
	return findRoot(dir, home, runtime.GOOS, func(name string) bool {
		_, err := os.Stat(name)
		return err == nil
	})
}

func findRoot(dir, home, goos string, exists func(string) bool) (string, error) {
	start, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("make path %q absolute: %w", dir, err)
	}
	var absoluteHome string
	if home != "" {
		absoluteHome, err = filepath.Abs(home)
		if err != nil {
			return "", fmt.Errorf("make path %q absolute: %w", home, err)
		}
	}

	equal := func(a, b string) bool {
		if goos == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}

	for current := start; ; current = filepath.Dir(current) {
		parent := filepath.Dir(current)
		// Boundaries are excluded even when they contain project markers.
		if equal(current, absoluteHome) || equal(current, parent) {
			return start, nil
		}
		for _, marker := range Markers {
			if exists(filepath.Join(current, marker)) {
				return current, nil
			}
		}
	}
}

func PresentMarkers(dir string) ([]string, error) {
	var present []string
	for _, marker := range Markers {
		name := filepath.Join(dir, marker)
		if _, err := os.Stat(name); err == nil {
			present = append(present, marker)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat %q: %w", name, err)
		}
	}
	return present, nil
}
