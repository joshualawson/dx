package shim

import (
	"path/filepath"
	"strings"
)

func Dir(goos, home string, getenv func(string) string) string {
	if goos == "windows" {
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = joinWindowsPath(home, `AppData\Local`)
		}
		return joinWindowsPath(base, `dx\bin`)
	}
	base := getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "dx", "bin")
}

func ToolName(argv0, goos string) string {
	name := argv0[strings.LastIndexAny(argv0, `/\`)+1:]
	if goos == "windows" && strings.EqualFold(filepath.Ext(name), ".exe") {
		name = name[:len(name)-4]
	}
	if name == "dx" || (goos == "windows" && strings.EqualFold(name, "dx")) {
		return ""
	}
	return name
}

func OnPath(dir, pathEnv, goos string) bool {
	for _, entry := range pathEntries(pathEnv, goos) {
		if entry != "" && samePath(entry, dir, goos) {
			return true
		}
	}
	return false
}

func pathEntries(pathEnv, goos string) []string {
	separator := ":"
	if goos == "windows" {
		separator = ";"
	}
	return strings.Split(pathEnv, separator)
}

func samePath(a, b, goos string) bool {
	if goos == "windows" {
		return strings.EqualFold(cleanWindowsPath(a), cleanWindowsPath(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func joinWindowsPath(base, suffix string) string {
	base = strings.ReplaceAll(base, "/", `\`)
	if base != "" && !strings.HasSuffix(base, `\`) && !(len(base) == 2 && base[1] == ':') {
		base += `\`
	}
	return cleanWindowsPath(base + suffix)
}

// Host filepath rules cannot clean Windows drive and UNC paths in Linux tests.
func cleanWindowsPath(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	volume := ""
	if len(value) >= 2 && value[1] == ':' {
		volume, value = value[:2], value[2:]
	} else if strings.HasPrefix(value, "//") {
		parts := strings.SplitN(strings.TrimLeft(value, "/"), "/", 3)
		volume = "//" + parts[0]
		value = "/"
		if len(parts) >= 2 {
			volume += "/" + parts[1]
		}
		if len(parts) == 3 {
			value += parts[2]
		}
	}
	rooted := strings.HasPrefix(value, "/")
	var parts []string
	for _, part := range strings.Split(value, "/") {
		switch part {
		case "", ".":
		case "..":
			if len(parts) > 0 && parts[len(parts)-1] != ".." {
				parts = parts[:len(parts)-1]
			} else if !rooted {
				parts = append(parts, part)
			}
		default:
			parts = append(parts, part)
		}
	}
	result := strings.Join(parts, "/")
	if rooted {
		result = "/" + result
	} else if result == "" {
		result = "."
	}
	return strings.ReplaceAll(volume+result, "/", `\`)
}
