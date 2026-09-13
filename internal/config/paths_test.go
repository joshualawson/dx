package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGlobalPath(t *testing.T) {
	for _, test := range []struct {
		name, goos, home string
		env              map[string]string
		want             string
	}{
		{"linux default", "linux", "/home/user", nil, filepath.FromSlash("/home/user/.config/dx/config.yaml")},
		{"darwin default", "darwin", "/Users/user", nil, filepath.FromSlash("/Users/user/.config/dx/config.yaml")},
		{"linux xdg", "linux", "/home/user", map[string]string{"XDG_CONFIG_HOME": "/custom", "APPDATA": "/ignored"}, filepath.FromSlash("/custom/dx/config.yaml")},
		{"darwin xdg", "darwin", "/Users/user", map[string]string{"XDG_CONFIG_HOME": "/custom"}, filepath.FromSlash("/custom/dx/config.yaml")},
		{"windows appdata", "windows", `C:\Users\user`, map[string]string{"APPDATA": `C:\Users\user\AppData\Roaming`, "XDG_CONFIG_HOME": "/ignored"}, `C:\Users\user\AppData\Roaming\dx\config.yaml`},
		{"windows slash appdata", "windows", "C:/Users/user", map[string]string{"APPDATA": "C:/Users/user/AppData/Roaming"}, `C:\Users\user\AppData\Roaming\dx\config.yaml`},
		{"windows unc appdata", "windows", `\\server\users\user`, map[string]string{"APPDATA": `\\server\users\user\AppData\Roaming`}, `\\server\users\user\AppData\Roaming\dx\config.yaml`},
		{"windows missing appdata", "windows", `C:\Users\user`, nil, filepath.Join("dx", "config.yaml")},
		{"linux override", "linux", "/home/user", map[string]string{"DX_CONFIG": "/custom.yaml", "XDG_CONFIG_HOME": "/ignored"}, "/custom.yaml"},
		{"darwin override", "darwin", "/Users/user", map[string]string{"DX_CONFIG": "/custom.yaml"}, "/custom.yaml"},
		{"windows override", "windows", `C:\Users\user`, map[string]string{"DX_CONFIG": `D:\custom.yaml`, "APPDATA": `C:\ignored`}, `D:\custom.yaml`},
		{"relative override preserved", "linux", "/home/user", map[string]string{"DX_CONFIG": "relative.yaml"}, "relative.yaml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var getenv func(string) string
			if test.env != nil {
				getenv = func(key string) string { return test.env[key] }
			}
			if got := GlobalPath(test.goos, test.home, getenv); got != test.want {
				t.Errorf("GlobalPath = %q, want %q", got, test.want)
			}
		})
	}
}

func TestContainsPath(t *testing.T) {
	for _, test := range []struct {
		name, goos, home, cwd string
		want                  bool
	}{
		{"inside", "linux", "/home/user", "/home/user/repo", true},
		{"equal", "linux", "/home/user", "/home/user", true},
		{"outside", "linux", "/home/user", "/srv/repo", false},
		{"prefix", "linux", "/home/user", "/home/user-other/repo", false},
		{"case sensitive", "linux", "/home/User", "/home/user/repo", false},
		{"darwin case sensitive", "darwin", "/Users/User", "/Users/user/repo", false},
		{"root", "linux", "/", "/repo", true},
		{"clean dotdot", "linux", "/home/user", "/home/user/../outside", false},
		{"windows case insensitive", "windows", `C:\Users\USER`, `c:\users\user\repo`, true},
		{"windows prefix", "windows", `C:\Users\user`, `C:\Users\user-other\repo`, false},
		{"windows different drive", "windows", `C:\Users\user`, `D:\Users\user\repo`, false},
		{"windows slash styles", "windows", `C:\Users\User`, "c:/users/user/repo", true},
		{"windows drive root", "windows", `C:\`, `c:\repo`, true},
		{"windows clean dotdot", "windows", `C:\Users\user`, `C:\Users\user\..\outside`, false},
		{"windows unc", "windows", `\\Server\Share\User`, `\\server\share\user\repo`, true},
		{"windows unc other share", "windows", `\\server\share\user`, `\\server\share-other\user\repo`, false},
		{"windows unc root", "windows", `\\server\share`, `\\server\share\repo`, true},
		{"windows unc trailing slash", "windows", `\\server\share\`, `\\server\share`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := containsPath(test.goos, test.home, test.cwd); got != test.want {
				t.Errorf("containsPath = %t, want %t", got, test.want)
			}
		})
	}
}

func TestParentPath(t *testing.T) {
	for _, test := range []struct {
		goos, input, want string
	}{
		{"linux", filepath.FromSlash("/home/user/repo"), filepath.FromSlash("/home/user")},
		{"linux", string(filepath.Separator), string(filepath.Separator)},
		{"windows", `C:\Users\user\repo`, `C:\Users\user`},
		{"windows", `C:\repo`, `C:\`},
		{"windows", `C:\`, `C:\`},
		{"windows", "C:/repo", `C:\`},
		{"windows", `\\server\share\repo`, `\\server\share\`},
		{"windows", `\\server\share\`, `\\server\share\`},
		{"windows", `\\server\share`, `\\server\share`},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := parentPath(test.goos, test.input); got != test.want {
				t.Errorf("parentPath = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExpandTrusted(t *testing.T) {
	for _, test := range []struct {
		name, goos, home string
		input, want      []string
	}{
		{"posix", "linux", filepath.FromSlash("/home/user"), []string{"~", "~/repo", "/srv/repo", "~other/repo", `~\repo`}, []string{filepath.FromSlash("/home/user"), filepath.FromSlash("/home/user/repo"), "/srv/repo", "~other/repo", `~\repo`}},
		{"windows", "windows", `C:\Users\user`, []string{"~", "~/repo", `~\other`, `D:\work`, "~other/repo"}, []string{`C:\Users\user`, `C:\Users\user\repo`, `C:\Users\user\other`, `D:\work`, "~other/repo"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := expandTrusted(test.input, test.home, test.goos); !reflect.DeepEqual(got, test.want) {
				t.Errorf("expandTrusted = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestWindowsHomeCaseStopsSearch(t *testing.T) {
	opts, _ := fixture(t, map[string]string{"home/.dx.yaml": "unknown: [broken", "home/org/.dx.yaml": "docker: true\n"}, "")
	opts.GOOS = "windows"
	opts.Home = strings.ToUpper(opts.Home)
	result, err := Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 1 || !result.Config.Docker {
		t.Errorf("result = %#v", result)
	}
}

func TestSameWindowsUNCRoot(t *testing.T) {
	if !samePath("windows", `\\server\share`, `\\SERVER\share\`) {
		t.Error("unc root with and without trailing separator should match")
	}
}

func TestAbsoluteWindowsPaths(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`C:\Users\user\..\repo`, `C:\Users\repo`},
		{"C:/Users/repo", `C:\Users\repo`},
		{`\\server\share\repo`, `\\server\share\repo`},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := absolutePath("windows", test.input)
			if err != nil || got != test.want {
				t.Errorf("absolutePath = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}
