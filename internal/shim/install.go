package shim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

type Installer struct {
	Dir  string
	Exe  string
	GOOS string
}

var toolPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

func validateTools(tools []string, goos string) error {
	for _, tool := range tools {
		if !toolPattern.MatchString(tool) || ToolName(tool, goos) == "" {
			return fmt.Errorf("invalid shim tool %q", tool)
		}
	}
	return nil
}

func (i Installer) toolPath(tool string) string {
	if i.GOOS == "windows" {
		tool += ".exe"
	}
	return filepath.Join(i.Dir, tool)
}

func sortedTools(tools []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(tools))
	for _, tool := range tools {
		if !seen[tool] {
			seen[tool] = true
			result = append(result, tool)
		}
	}
	sort.Strings(result)
	return result
}

func (i Installer) checkDestination(path, mainPath string) error {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect shim %q: %w", path, err)
	}
	ok, _, err := inspect(path, i.Exe, mainPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("refusing to replace non-shim file %q", path)
	}
	return nil
}

func (i Installer) Install(tools []string) ([]string, error) {
	return i.install(tools, dxMainPath)
}

func (i Installer) install(tools []string, mainPath string) ([]string, error) {
	if len(tools) == 0 {
		return nil, fmt.Errorf("no shim tools specified")
	}
	if err := validateTools(tools, i.GOOS); err != nil {
		return nil, err
	}
	names := sortedTools(tools)
	for _, tool := range names {
		if err := i.checkDestination(i.toolPath(tool), mainPath); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(i.Dir, 0755); err != nil {
		return nil, fmt.Errorf("create shims folder %q: %w", i.Dir, err)
	}
	installed := make([]string, 0, len(names))
	for _, tool := range names {
		if err := i.installAt(i.toolPath(tool), mainPath); err != nil {
			return installed, err
		}
		installed = append(installed, tool)
	}
	return installed, nil
}

func (i Installer) installAt(destination, mainPath string) error {
	// A private staging directory keeps link and copy creation on the destination filesystem.
	stage, err := os.MkdirTemp(i.Dir, ".dx-shim-")
	if err != nil {
		return fmt.Errorf("stage shim %q: %w", destination, err)
	}
	defer os.RemoveAll(stage)
	temp := filepath.Join(stage, "shim")
	if i.GOOS == "windows" {
		err = linkOrCopy(i.Exe, temp, os.Link)
	} else {
		err = os.Symlink(i.Exe, temp)
	}
	if err != nil {
		return fmt.Errorf("create shim %q for %q: %w", destination, i.Exe, err)
	}
	if err := i.checkDestination(destination, mainPath); err != nil {
		return err
	}
	if err := os.Rename(temp, destination); err != nil {
		return fmt.Errorf("replace shim %q: %w", destination, err)
	}
	return nil
}

func linkOrCopy(source, destination string, link func(string, string) error) error {
	if err := link(source, destination); err == nil {
		return nil
	}
	return copyExecutable(source, destination)
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open executable %q: %w", source, err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("stat executable %q: %w", source, err)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create executable copy %q: %w", destination, err)
	}
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy executable %q to %q: %w", source, destination, err)
	}
	if err := output.Chmod(info.Mode()); err != nil {
		return fmt.Errorf("set executable mode %q: %w", destination, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close executable copy %q: %w", destination, err)
	}
	return nil
}

func (i Installer) Uninstall(tools []string) ([]string, error) {
	return i.uninstall(tools, dxMainPath)
}

func (i Installer) uninstall(tools []string, mainPath string) ([]string, error) {
	if err := validateTools(tools, i.GOOS); err != nil {
		return nil, err
	}
	var entries []Entry
	if len(tools) == 0 {
		var err error
		entries, err = i.list(mainPath)
		if err != nil {
			return nil, err
		}
	} else {
		for _, tool := range sortedTools(tools) {
			path := i.toolPath(tool)
			_, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("inspect shim %q: %w", path, err)
			}
			ok, _, err := inspect(path, i.Exe, mainPath)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("refusing to remove non-shim file %q", path)
			}
			entries = append(entries, Entry{Tool: tool, Path: path})
		}
	}
	removed := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := os.Remove(entry.Path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("remove shim %q: %w", entry.Path, err)
		}
		removed = append(removed, entry.Tool)
	}
	sort.Strings(removed)
	return removed, nil
}
