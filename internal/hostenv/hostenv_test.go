package hostenv

import (
	"reflect"
	"strings"
	"testing"
)

func TestContainerPath(t *testing.T) {
	tests := []struct {
		name string
		goos string
		host string
		want string
	}{
		{"linux absolute", "linux", "/home/josh/src", "/home/josh/src"},
		{"linux clean", "linux", "/home//josh/./src/../app///", "/home/josh/app"},
		{"linux root", "linux", "/", "/"},
		{"linux relative", "linux", "src/../app/", "app"},
		{"linux backslash", "linux", `/home/josh/a\b/`, `/home/josh/a\b`},
		{"darwin absolute", "darwin", "/Users/josh/src", "/Users/josh/src"},
		{"darwin clean", "darwin", "/Users//josh/./src/../app/", "/Users/josh/app"},
		{"darwin root", "darwin", "///", "/"},
		{"windows uppercase drive", "windows", `C:\Users\josh\src`, "/c/Users/josh/src"},
		{"windows lowercase drive", "windows", `d:\Users\Josh\Src`, "/d/Users/Josh/Src"},
		{"windows drive root", "windows", `C:\`, "/c"},
		{"windows forward drive root", "windows", "z:///", "/z"},
		{"windows mixed separators", "windows", `C:\Users/josh\Src/`, "/c/Users/josh/Src"},
		{"windows trailing separators", "windows", `C:\Users\josh\\`, "/c/Users/josh"},
		{"windows clean", "windows", `C:\Users\.\josh\src\..\app`, "/c/Users/josh/app"},
		{"windows cannot escape drive", "windows", `C:\..\..\src`, "/c/src"},
		{"windows UNC", "windows", `\\server\share\dir`, "/unc/server/share/dir"},
		{"windows UNC root", "windows", `\\Server\Share\`, "/unc/Server/Share"},
		{"windows UNC mixed", "windows", `//Server\Share/dir\\`, "/unc/Server/Share/dir"},
		{"windows rooted", "windows", `\Users\josh\`, "/Users/josh"},
		{"windows relative", "windows", `src\..\app\`, "app"},
		{"windows invalid drive", "windows", `1:\src`, "1:/src"},
		{"empty linux", "linux", "", "."},
		{"empty windows", "windows", "", "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainerPath(tt.host, tt.goos); got != tt.want {
				t.Errorf("ContainerPath(%q, %q) = %q, want %q", tt.host, tt.goos, got, tt.want)
			}
		})
	}
}

func TestCredentials(t *testing.T) {
	want := []Credential{
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
	if !reflect.DeepEqual(Credentials, want) {
		t.Errorf("Credentials = %#v, want %#v", Credentials, want)
	}
}

func TestCredentialMounts(t *testing.T) {
	homes := []struct {
		name   string
		goos   string
		home   string
		source string
		target string
	}{
		{"linux", "linux", "/home/josh", "/home/josh/", "/home/josh/"},
		{"linux trailing", "linux", "/home/josh///", "/home/josh/", "/home/josh/"},
		{"darwin", "darwin", "/Users/josh", "/Users/josh/", "/Users/josh/"},
		{"windows", "windows", `C:\Users\Josh`, `C:\Users\Josh\`, "/c/Users/Josh/"},
		{"windows trailing", "windows", `C:\Users\Josh\\`, `C:\Users\Josh\`, "/c/Users/Josh/"},
		{"windows mixed", "windows", `C:/Users\Josh/`, `C:\Users\Josh\`, "/c/Users/Josh/"},
		{"windows root", "windows", `D:\`, `D:\`, "/d/"},
		{"windows UNC", "windows", `\\Server\Share\Josh`, `\\Server\Share\Josh\`, "/unc/Server/Share/Josh/"},
	}
	scenarios := []struct {
		name     string
		disabled map[string]bool
		missing  map[string]bool
		none     bool
	}{
		{name: "all enabled and present"},
		{name: "disabled", disabled: map[string]bool{"ssh": true, "git": true, "cargo": true}},
		{name: "missing", missing: map[string]bool{".gitconfig": true, ".config/gcloud": true, ".docker/config.json": true}},
		{name: "disabled and missing", disabled: map[string]bool{"aws": true}, missing: map[string]bool{".config/git": true}},
		{name: "none enabled", none: true},
	}
	for _, home := range homes {
		for _, scenario := range scenarios {
			t.Run(home.name+"/"+scenario.name, func(t *testing.T) {
				var want []Mount
				var wantChecked []string
				var wantEnabled []string
				existing := make(map[string]bool)
				for _, credential := range Credentials {
					wantEnabled = append(wantEnabled, credential.Name)
					if scenario.none || scenario.disabled[credential.Name] {
						continue
					}
					for _, relative := range credential.Paths {
						sourceRelative := relative
						if home.goos == "windows" {
							sourceRelative = strings.ReplaceAll(relative, "/", `\`)
						}
						source := home.source + sourceRelative
						wantChecked = append(wantChecked, source)
						if !scenario.missing[relative] {
							existing[source] = true
							want = append(want, Mount{Source: source, Target: home.target + relative})
						}
					}
				}
				var checked, enabled []string
				got := CredentialMounts(home.home, home.goos, func(name string) bool {
					enabled = append(enabled, name)
					return !scenario.none && !scenario.disabled[name]
				}, func(source string) bool {
					checked = append(checked, source)
					return existing[source]
				})
				if !reflect.DeepEqual(got, want) {
					t.Errorf("CredentialMounts() = %#v, want %#v", got, want)
				}
				if !reflect.DeepEqual(checked, wantChecked) {
					t.Errorf("existence checks = %q, want %q", checked, wantChecked)
				}
				if !reflect.DeepEqual(enabled, wantEnabled) {
					t.Errorf("enabled checks = %q, want %q", enabled, wantEnabled)
				}
			})
		}
	}
}

func TestSSHAgent(t *testing.T) {
	const target = "/run/host-services/ssh-auth.sock"
	for _, goos := range []string{"linux", "darwin", "windows", "unknown"} {
		for _, sock := range []string{"", "/tmp/agent.sock"} {
			for _, present := range []bool{false, true} {
				name := goos + "/unset"
				if sock != "" {
					name = goos + "/set"
				}
				if present {
					name += "/present"
				} else {
					name += "/missing"
				}
				t.Run(name, func(t *testing.T) {
					var checked []string
					mount, env := SSHAgent(goos, func(key string) string {
						if key != "SSH_AUTH_SOCK" {
							t.Errorf("unexpected environment lookup: %q", key)
						}
						return sock
					}, func(source string) bool {
						checked = append(checked, source)
						return present
					})
					var wantMount *Mount
					var wantEnv, wantChecked []string
					if sock != "" && goos == "linux" {
						wantChecked = []string{sock}
						if present {
							wantMount = &Mount{Source: sock, Target: target}
						}
					}
					if sock != "" && goos == "darwin" {
						wantMount = &Mount{Source: target, Target: target}
					}
					if wantMount != nil {
						wantEnv = []string{"SSH_AUTH_SOCK=" + target}
					}
					if !reflect.DeepEqual(mount, wantMount) || !reflect.DeepEqual(env, wantEnv) {
						t.Errorf("SSHAgent() = (%#v, %q), want (%#v, %q)", mount, env, wantMount, wantEnv)
					}
					if !reflect.DeepEqual(checked, wantChecked) {
						t.Errorf("existence checks = %q, want %q", checked, wantChecked)
					}
				})
			}
		}
	}
}

func TestFilterEnvExcludedNames(t *testing.T) {
	names := strings.Fields(`
PATH HOME SHELL USER LOGNAME PWD OLDPWD SHLVL HOSTNAME TMPDIR TEMP TMP _ DISPLAY
XDG_RUNTIME_DIR XDG_CONFIG_HOME XDG_CACHE_HOME XDG_DATA_HOME XDG_STATE_HOME
SSH_AUTH_SOCK TERM_PROGRAM TERM_PROGRAM_VERSION TERM_SESSION_ID
GOROOT GOPATH GOBIN GOCACHE GOMODCACHE GOENV CARGO_HOME RUSTUP_HOME RUSTUP_TOOLCHAIN
NVM_DIR NVM_BIN NODE_PATH PYENV_ROOT VIRTUAL_ENV PYTHONHOME PYTHONPATH CONDA_PREFIX JAVA_HOME
DOCKER_HOST DOCKER_CONTEXT DOCKER_CERT_PATH DOCKER_TLS_VERIFY DOCKER_CONFIG
LD_PRELOAD LD_LIBRARY_PATH DYLD_LIBRARY_PATH DX_IMAGE __CF_USER_TEXT_ENCODING XPC_SERVICE_NAME CONDA_DEFAULT_ENV
LD_ DYLD_ DX_ __CF XPC_ CONDA_
npm_config_prefix NPM_CONFIG_PREFIX PNPM_HOME VOLTA_HOME BUN_INSTALL ASDF_DIR ASDF_DATA_DIR
RBENV_ROOT GEM_HOME GEM_PATH SDKMAN_DIR PIPX_HOME PIPX_BIN_DIR
UV_PYTHON_INSTALL_DIR UV_TOOL_DIR UV_TOOL_BIN_DIR PKG_CONFIG_PATH LIBRARY_PATH CPATH
C_INCLUDE_PATH CPLUS_INCLUDE_PATH MANPATH INFOPATH TERMINFO TERMINFO_DIRS
XDG_DATA_DIRS XDG_CONFIG_DIRS GIT_EXEC_PATH
HOMEBREW_PREFIX HOMEBREW_CELLAR MISE_DATA_DIR MISE_CONFIG_DIR HOMEBREW_ MISE_
`)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, name := range names {
			t.Run(goos+"/"+name, func(t *testing.T) {
				input := []string{name + "=value"}
				if goos == "windows" {
					input = append(input, strings.ToLower(name)+"=value")
				}
				if got := FilterEnv(input, goos); len(got) != 0 {
					t.Errorf("FilterEnv(%q, %q) = %q, want no entries", input, goos, got)
				}
			})
		}
	}
}

func TestFilterEnvInstallLocationsCaseAndLookalikes(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			kept := []string{
				"MY_HOMEBREW_THING=keep", "MY_MISE_THING=keep", "HOMEBREW=keep", "MISE=keep",
				"GOFLAGS=-mod=readonly", "NODE_OPTIONS=--max-old-space-size=4096",
				"NPM_CONFIG_PREFIX_EXTRA=keep", "MY_PNPM_HOME=keep",
			}
			mixedCase := []string{
				"Npm_Config_Prefix=case-sensitive", "Pnpm_Home=case-sensitive",
				"Homebrew_Prefix=case-sensitive", "Mise_Data_Dir=case-sensitive",
			}
			input := append(append([]string(nil), kept...), mixedCase...)
			want := input
			if goos == "windows" {
				want = kept
			}
			if got := FilterEnv(input, goos); !reflect.DeepEqual(got, want) {
				t.Errorf("FilterEnv(%q, %q) = %q, want %q", input, goos, got, want)
			}
		})
	}
}

func TestFilterEnvWindowsNames(t *testing.T) {
	names := strings.Fields(`
SYSTEMROOT SYSTEMDRIVE WINDIR COMSPEC PATHEXT PROGRAMFILES PROGRAMFILES(X86)
PROGRAMDATA APPDATA LOCALAPPDATA USERPROFILE HOMEDRIVE HOMEPATH OS PROCESSOR_ARCHITECTURE PSMODULEPATH
`)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, name := range names {
			t.Run(goos+"/"+name, func(t *testing.T) {
				input := []string{name + "=value", strings.ToLower(name) + "=value"}
				want := input
				if goos == "windows" {
					want = nil
				}
				if got := FilterEnv(input, goos); !reflect.DeepEqual(got, want) {
					t.Errorf("FilterEnv(%q, %q) = %q, want %q", input, goos, got, want)
				}
			})
		}
	}
}

func TestFilterEnvPreservation(t *testing.T) {
	tests := []struct {
		name  string
		goos  string
		input []string
		want  []string
	}{
		{"nil", "linux", nil, nil},
		{"empty", "windows", []string{}, nil},
		{"malformed", "linux", []string{"", "broken", "TERM", "PATH"}, nil},
		{
			"order and values", "linux",
			[]string{"TERM=xterm-256color", "PATH=/bin", "COLORTERM=truecolor", "LANG=en_US.UTF-8", "LC_ALL=C", "broken", "LC_TIME=C", "TOKEN=a=b=c", "EMPTY=", "TERM=second"},
			[]string{"TERM=xterm-256color", "COLORTERM=truecolor", "LANG=en_US.UTF-8", "LC_ALL=C", "LC_TIME=C", "TOKEN=a=b=c", "EMPTY=", "TERM=second"},
		},
		{
			"user and build settings", "darwin",
			[]string{"GOPRIVATE=example.com/*", "GOOS=darwin", "GOARCH=arm64", "AWS_PROFILE=work", "HTTP_PROXY=http://proxy", "NPM_TOKEN=secret", "CARGO_NET_GIT_FETCH_WITH_CLI=true", "RUSTFLAGS=-Copt-level=2"},
			[]string{"GOPRIVATE=example.com/*", "GOOS=darwin", "GOARCH=arm64", "AWS_PROFILE=work", "HTTP_PROXY=http://proxy", "NPM_TOKEN=secret", "CARGO_NET_GIT_FETCH_WITH_CLI=true", "RUSTFLAGS=-Copt-level=2"},
		},
		{
			"POSIX case sensitive", "linux",
			[]string{"Path=keep", "home=keep", "dx_image=keep", "ld_preload=keep", "__cf_encoding=keep"},
			[]string{"Path=keep", "home=keep", "dx_image=keep", "ld_preload=keep", "__cf_encoding=keep"},
		},
		{
			"windows mixed case", "windows",
			[]string{"Path=drop", "Home=drop", "GoRoot=drop", "Docker_Host=drop", "dX_Image=drop", "Ld_Preload=drop", "__cF_encoding=drop", "SystemRoot=drop", "Term=keep", "Lc_All=C", "Token=Mixed=Value"},
			[]string{"Term=keep", "Lc_All=C", "Token=Mixed=Value"},
		},
		{
			"exact boundaries", "linux",
			[]string{"PATH_SUFFIX=keep", "MY_HOME=keep", "DOCKER_HOSTNAME=keep", "LD=keep", "DYLD=keep", "DX=keep", "XPC=keep", "CONDA=keep", "CF=keep", "XDG_DATA_DIRS_SUFFIX=keep"},
			[]string{"PATH_SUFFIX=keep", "MY_HOME=keep", "DOCKER_HOSTNAME=keep", "LD=keep", "DYLD=keep", "DX=keep", "XPC=keep", "CONDA=keep", "CF=keep", "XDG_DATA_DIRS_SUFFIX=keep"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.input...)
			got := FilterEnv(tt.input, tt.goos)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterEnv(%q, %q) = %q, want %q", tt.input, tt.goos, got, tt.want)
			}
			for i := range original {
				if tt.input[i] != original[i] {
					t.Errorf("input[%d] changed from %q to %q", i, original[i], tt.input[i])
				}
			}
		})
	}
}
