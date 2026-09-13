package config

import (
	"path/filepath"
	"strings"
)

func GlobalPath(goos, home string, getenv func(string) string) string {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if override := getenv("DX_CONFIG"); override != "" {
		return override
	}
	if goos == "windows" {
		return joinPath(goos, getenv("APPDATA"), "dx", "config.yaml")
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = joinPath(goos, home, ".config")
	}
	return joinPath(goos, base, "dx", "config.yaml")
}

func absolutePath(goos, name string) (string, error) {
	if goos == "windows" && windowsStyle(name) {
		clean := cleanWindows(name)
		volume, rest := windowsVolume(strings.ReplaceAll(clean, `\`, "/"))
		if volume != "" && (strings.HasPrefix(rest, "/") || strings.HasPrefix(volume, "//")) {
			return clean, nil
		}
	}
	return filepath.Abs(name)
}

func joinPath(goos string, elems ...string) string {
	if goos == "windows" {
		for _, elem := range elems {
			if windowsStyle(elem) {
				return cleanWindows(strings.Join(elems, "/"))
			}
		}
	}
	return filepath.Join(elems...)
}

func parentPath(goos, name string) string {
	if goos != "windows" || !windowsStyle(name) {
		return filepath.Dir(name)
	}
	clean := strings.ReplaceAll(cleanWindows(name), `\`, "/")
	volume, rest := windowsVolume(clean)
	if rest == "" || rest == "/" {
		return cleanWindows(clean)
	}
	index := strings.LastIndex(rest, "/")
	if index < 0 {
		return cleanWindows(volume + ".")
	}
	if index == 0 {
		return cleanWindows(volume + "/")
	}
	return cleanWindows(volume + rest[:index])
}

func samePath(goos, a, b string) bool {
	return comparablePath(goos, a) == comparablePath(goos, b)
}

func containsPath(goos, parent, child string) bool {
	parent = comparablePath(goos, parent)
	child = comparablePath(goos, child)
	return child == parent || strings.HasPrefix(child, strings.TrimRight(parent, "/")+"/")
}

func comparablePath(goos, name string) string {
	if goos == "windows" {
		clean := strings.ReplaceAll(cleanWindows(name), `\`, "/")
		volume, rest := windowsVolume(clean)
		if strings.HasPrefix(volume, "//") && rest == "/" {
			clean = volume
		}
		return strings.ToLower(clean)
	}
	return filepath.ToSlash(filepath.Clean(name))
}

func windowsStyle(name string) bool {
	return strings.Contains(name, `\`) || (len(name) >= 2 && name[1] == ':') || strings.HasPrefix(name, "//")
}

// Windows paths must remain testable when the host uses POSIX filepath rules.
func cleanWindows(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	volume, rest := windowsVolume(name)
	clean := filepath.ToSlash(filepath.Clean(rest))
	if rest == "" && volume != "" {
		clean = ""
	}
	return strings.ReplaceAll(volume+clean, "/", `\`)
}

func windowsVolume(name string) (string, string) {
	if len(name) >= 2 && name[1] == ':' {
		return name[:2], name[2:]
	}
	if strings.HasPrefix(name, "//") {
		parts := strings.SplitN(name[2:], "/", 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			volume := "//" + parts[0] + "/" + parts[1]
			if len(parts) == 3 {
				return volume, "/" + parts[2]
			}
			return volume, ""
		}
	}
	return "", name
}
