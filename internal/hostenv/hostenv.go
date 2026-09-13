package hostenv

import (
	"path"
	"strings"
)

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

func ContainerPath(hostPath, goos string) string {
	if goos != "windows" {
		// POSIX cleaning must also work when the caller runs on Windows.
		return path.Clean(hostPath)
	}

	hostPath = strings.ReplaceAll(hostPath, `\`, "/")
	if len(hostPath) >= 2 && hostPath[1] == ':' && isDriveLetter(hostPath[0]) {
		return path.Join("/"+strings.ToLower(hostPath[:1]), path.Clean("/"+hostPath[2:]))
	}
	if strings.HasPrefix(hostPath, "//") {
		return path.Join("/unc", path.Clean("/"+strings.TrimLeft(hostPath, "/")))
	}
	return path.Clean(hostPath)
}

func isDriveLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

type Credential struct {
	Name  string
	Paths []string
}

var Credentials = []Credential{
	{"ssh", []string{".ssh"}},
	{"git", []string{".gitconfig", ".config/git"}},
	{"netrc", []string{".netrc"}},
	{"aws", []string{".aws"}},
	{"gcloud", []string{".config/gcloud"}},
	{"azure", []string{".azure"}},
	{"kube", []string{".kube"}},
	{"gh", []string{".config/gh"}},
	{"docker", []string{".docker/config.json"}},
	{"npm", []string{".npmrc"}},
	{"pypi", []string{".pypirc"}},
	{"cargo", []string{".cargo/credentials.toml"}},
	{"terraform", []string{".terraform.d"}},
}

func CredentialMounts(home, goos string, enabled func(name string) bool, exists func(hostPath string) bool) []Mount {
	var mounts []Mount
	for _, credential := range Credentials {
		if !enabled(credential.Name) {
			continue
		}
		for _, relative := range credential.Paths {
			source := path.Join(home, relative)
			if goos == "windows" {
				source = strings.ReplaceAll(strings.TrimRight(home, `\/`)+`\`+relative, "/", `\`)
			}
			if exists(source) {
				mounts = append(mounts, Mount{Source: source, Target: ContainerPath(source, goos)})
			}
		}
	}
	return mounts
}

func SSHAgent(goos string, getenv func(string) string, exists func(string) bool) (*Mount, []string) {
	sock := getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil
	}
	const target = "/run/host-services/ssh-auth.sock"
	switch goos {
	case "linux":
		if !exists(sock) {
			return nil, nil
		}
	case "darwin":
		sock = target
	case "windows":
		// Windows agent named pipes cannot be mounted as Unix sockets in Linux containers.
		return nil, nil
	default:
		return nil, nil
	}
	return &Mount{Source: sock, Target: target}, []string{"SSH_AUTH_SOCK=" + target}
}

func FilterEnv(environ []string, goos string) []string {
	var filtered []string
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if goos == "windows" {
			name = strings.ToUpper(name)
			if windowsEnvNames[name] {
				continue
			}
		}
		if excludedEnvNames[name] || excludedPrefix(name) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func excludedPrefix(name string) bool {
	for _, prefix := range []string{"LD_", "DYLD_", "DX_", "__CF", "XPC_", "CONDA_", "HOMEBREW_", "MISE_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

var excludedEnvNames = nameSet(`
PATH HOME SHELL USER LOGNAME PWD OLDPWD SHLVL HOSTNAME TMPDIR TEMP TMP _
DISPLAY XDG_RUNTIME_DIR XDG_CONFIG_HOME XDG_CACHE_HOME XDG_DATA_HOME XDG_STATE_HOME
SSH_AUTH_SOCK TERM_PROGRAM TERM_PROGRAM_VERSION TERM_SESSION_ID
GOROOT GOPATH GOBIN GOCACHE GOMODCACHE GOENV CARGO_HOME RUSTUP_HOME RUSTUP_TOOLCHAIN
NVM_DIR NVM_BIN NODE_PATH PYENV_ROOT VIRTUAL_ENV PYTHONHOME PYTHONPATH CONDA_PREFIX JAVA_HOME
DOCKER_HOST DOCKER_CONTEXT DOCKER_CERT_PATH DOCKER_TLS_VERIFY DOCKER_CONFIG
npm_config_prefix NPM_CONFIG_PREFIX PNPM_HOME VOLTA_HOME BUN_INSTALL ASDF_DIR ASDF_DATA_DIR
RBENV_ROOT GEM_HOME GEM_PATH SDKMAN_DIR PIPX_HOME PIPX_BIN_DIR
UV_PYTHON_INSTALL_DIR UV_TOOL_DIR UV_TOOL_BIN_DIR PKG_CONFIG_PATH LIBRARY_PATH CPATH
C_INCLUDE_PATH CPLUS_INCLUDE_PATH MANPATH INFOPATH TERMINFO TERMINFO_DIRS
XDG_DATA_DIRS XDG_CONFIG_DIRS GIT_EXEC_PATH
`)

var windowsEnvNames = nameSet(`
SYSTEMROOT SYSTEMDRIVE WINDIR COMSPEC PATHEXT PROGRAMFILES PROGRAMFILES(X86)
PROGRAMDATA APPDATA LOCALAPPDATA USERPROFILE HOMEDRIVE HOMEPATH OS
PROCESSOR_ARCHITECTURE PSMODULEPATH
`)

func nameSet(names string) map[string]bool {
	set := make(map[string]bool)
	for _, name := range strings.Fields(names) {
		set[name] = true
	}
	return set
}
