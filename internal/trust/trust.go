package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Store binds approvals to both the config's location and its exact contents.
type Store struct{ Path string }

type approvals struct {
	Approvals map[string]string `json:"approvals"`
}

func DefaultPath(goos, home string, getenv func(string) string) string {
	if goos == "windows" {
		return windowsJoin(getenv("LOCALAPPDATA"), "dx", "trust.json")
	}
	base := getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "dx", "trust.json")
}

func (s *Store) IsTrusted(path string, content []byte, doc map[string]any, trustedDirs []string) (bool, error) {
	if !NeedsApproval(doc) || UnderDir(path, trustedDirs, runtime.GOOS) {
		return true, nil
	}
	key, err := absolutePath(path)
	if err != nil {
		return false, err
	}
	stored, err := s.load()
	if err != nil {
		return false, err
	}
	return stored.Approvals[key] == digest(content), nil
}

func (s *Store) Approve(path string, content []byte) error {
	key, err := absolutePath(path)
	if err != nil {
		return err
	}
	stored, err := s.load()
	if err != nil {
		return err
	}
	stored.Approvals[key] = digest(content)
	return s.save(stored)
}

func (s *Store) Revoke(path string) error {
	key, err := absolutePath(path)
	if err != nil {
		return err
	}
	stored, err := s.load()
	if err != nil {
		return err
	}
	delete(stored.Approvals, key)
	return s.save(stored)
}

func absolutePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", path, err)
	}
	return filepath.Clean(absolute), nil
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (s *Store) load() (approvals, error) {
	stored := approvals{Approvals: make(map[string]string)}
	content, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return stored, nil
	}
	if err != nil {
		return stored, fmt.Errorf("read trust store %q: %w", s.Path, err)
	}
	if err := json.Unmarshal(content, &stored); err != nil {
		return stored, fmt.Errorf("decode trust store %q: %w", s.Path, err)
	}
	if stored.Approvals == nil {
		stored.Approvals = make(map[string]string)
	}
	return stored, nil
}

func (s *Store) save(stored approvals) error {
	content, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trust store %q: %w", s.Path, err)
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create trust store directory %q: %w", dir, err)
	}
	file, err := os.CreateTemp(dir, ".trust-*")
	if err != nil {
		return fmt.Errorf("create temporary trust store for %q: %w", s.Path, err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("set trust store permissions %q: %w", file.Name(), err)
	}
	if _, err := file.Write(append(content, '\n')); err != nil {
		return fmt.Errorf("write trust store %q: %w", file.Name(), err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync trust store %q: %w", file.Name(), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close trust store %q: %w", file.Name(), err)
	}
	if err := os.Rename(file.Name(), s.Path); err != nil {
		return fmt.Errorf("replace trust store %q: %w", s.Path, err)
	}
	return nil
}

func NeedsApproval(doc map[string]any) bool {
	return len(Describe(doc)) != 0
}

func Describe(doc map[string]any) []string {
	var lines []string
	for key, value := range doc {
		base := strings.TrimSuffix(key, "+")
		if base == "local" {
			continue
		}
		if root, ok := value.(bool); base == "root" && ok && !root {
			continue
		}
		if tools, ok := value.(map[string]any); base == "tools" && ok {
			for name, tool := range tools {
				prefix := "tools." + strings.TrimSuffix(name, "+")
				if settings, ok := tool.(map[string]any); ok {
					for setting, value := range settings {
						setting = strings.TrimSuffix(setting, "+")
						if setting != "version" {
							flatten(prefix+"."+setting, value, &lines)
						}
					}
				} else {
					flatten(prefix, tool, &lines)
				}
			}
		} else {
			flatten(base, value, &lines)
		}
	}
	sort.Strings(lines)
	return lines
}

func flatten(key string, value any, lines *[]string) {
	if mapping, ok := value.(map[string]any); ok && len(mapping) != 0 {
		for child, value := range mapping {
			flatten(key+"."+strings.TrimSuffix(child, "+"), value, lines)
		}
		return
	}
	*lines = append(*lines, key+" = "+render(value))
}

func render(value any) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return strconv.Quote(value)
		}
		return value
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			parts[i] = render(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(value))
		for i, item := range value {
			parts[i] = render(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		parts := make([]string, 0, len(value))
		for key, item := range value {
			parts = append(parts, key+": "+render(item))
		}
		sort.Strings(parts)
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(value)
	}
}

func UnderDir(path string, dirs []string, goos string) bool {
	candidate, err := comparisonPath(path, goos)
	if err != nil {
		return false
	}
	for _, dir := range dirs {
		parent, err := comparisonPath(dir, goos)
		if err == nil && (candidate == parent || strings.HasPrefix(candidate, strings.TrimRight(parent, "/")+"/")) {
			return true
		}
	}
	return false
}

func comparisonPath(path, goos string) (string, error) {
	if goos == "windows" {
		path = strings.ReplaceAll(path, `\`, "/")
		// Preserve Windows roots when testing on a non-Windows host.
		if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
			return strings.ToLower(path[:2] + filepath.ToSlash(filepath.Clean(filepath.FromSlash(path[2:])))), nil
		}
		if strings.HasPrefix(path, "//") {
			parts := strings.SplitN(strings.TrimPrefix(path, "//"), "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				rest := "/"
				if len(parts) == 3 {
					rest += parts[2]
				}
				return strings.ToLower("//" + parts[0] + "/" + parts[1] + filepath.ToSlash(filepath.Clean(filepath.FromSlash(rest)))), nil
			}
		}
	}
	absolute, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.ToSlash(absolute)
	if goos == "windows" {
		absolute = strings.ToLower(absolute)
	}
	return absolute, nil
}

func windowsJoin(parts ...string) string {
	var joined string
	for _, part := range parts {
		part = strings.ReplaceAll(part, "/", `\`)
		if joined == "" {
			joined = part
		} else if part != "" {
			joined = strings.TrimRight(joined, `\`) + `\` + strings.TrimLeft(part, `\`)
		}
	}
	return joined
}
