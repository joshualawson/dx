package shim

import (
	"path/filepath"
	"testing"
)

func TestDir(t *testing.T) {
	tests := []struct {
		name, goos, home, key, value, want string
	}{
		{"linux default", "linux", "/home/user", "", "", filepath.Join("/home/user", ".local", "share", "dx", "bin")},
		{"linux xdg", "linux", "/home/user", "XDG_DATA_HOME", "/data", filepath.Join("/data", "dx", "bin")},
		{"darwin", "darwin", "/Users/user", "", "", filepath.Join("/Users/user", ".local", "share", "dx", "bin")},
		{"freebsd", "freebsd", "/home/user", "XDG_DATA_HOME", "/data/", filepath.Join("/data", "dx", "bin")},
		{"windows", "windows", `C:\Users\user`, "LOCALAPPDATA", `C:\Users\user\AppData\Local`, `C:\Users\user\AppData\Local\dx\bin`},
		{"windows slashes", "windows", "unused", "LOCALAPPDATA", "C:/data/", `C:\data\dx\bin`},
		{"windows unc", "windows", "unused", "LOCALAPPDATA", `\\server\share\data`, `\\server\share\data\dx\bin`},
		{"windows drive relative", "windows", "unused", "LOCALAPPDATA", "C:", `C:dx\bin`},
		{"windows drive root", "windows", "unused", "LOCALAPPDATA", `C:\`, `C:\dx\bin`},
		{"windows empty", "windows", `C:\Users\user`, "", "", `C:\Users\user\AppData\Local\dx\bin`},
		{"windows fallback slashes", "windows", "C:/Users/user/", "", "", `C:\Users\user\AppData\Local\dx\bin`},
		{"windows fallback unc", "windows", `\\server\share\user`, "", "", `\\server\share\user\AppData\Local\dx\bin`},
		{"windows ignores xdg", "windows", `C:\Users\user`, "XDG_DATA_HOME", "/data", `C:\Users\user\AppData\Local\dx\bin`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == tt.key {
					return tt.value
				}
				return ""
			}
			if got := Dir(tt.goos, tt.home, getenv); got != tt.want {
				t.Fatalf("Dir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolName(t *testing.T) {
	tests := []struct{ argv0, goos, want string }{
		{"", "linux", ""},
		{"dx", "linux", ""},
		{"/usr/local/bin/dx", "linux", ""},
		{"go", "linux", "go"},
		{"/usr/local/bin/go", "darwin", "go"},
		{`C:\bin\go`, "linux", "go"},
		{`C:\bin/go.EXE`, "windows", "go"},
		{`C:\bin\DX.ExE`, "windows", ""},
		{"DX", "windows", ""},
		{"DX", "linux", "DX"},
		{"dx.exe", "linux", "dx.exe"},
		{"go.cmd", "windows", "go.cmd"},
		{"go.exe.exe", "windows", "go.exe"},
		{"/", "linux", ""},
		{`C:\`, "windows", ""},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.argv0, func(t *testing.T) {
			if got := ToolName(tt.argv0, tt.goos); got != tt.want {
				t.Fatalf("ToolName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOnPath(t *testing.T) {
	tests := []struct {
		name, dir, path, goos string
		want                  bool
	}{
		{"unix entry", "/dx/bin", "/usr/bin:/dx/bin:/bin", "linux", true},
		{"unix cleaned", "/dx/bin/", "/bin:/dx/./tools/../bin//", "linux", true},
		{"unix case", "/dx/bin", "/DX/bin", "linux", false},
		{"unix prefix", "/dx/bin", "/dx/binary", "linux", false},
		{"unix empty", ".", "::", "linux", false},
		{"unix root", "/", "/:/usr/bin", "linux", true},
		{"windows case", `C:\DX\bin`, `C:\Windows; c:\other;c:\dx\BIN\`, "windows", true},
		{"windows slashes", `C:\dx\bin\`, `C:/dx/./tools/../bin//;C:/other`, "windows", true},
		{"windows separator", `C:\dx\bin`, `C:\dx\bin:C:\other`, "windows", false},
		{"windows empty", ".", ";;", "windows", false},
		{"windows unc", `\\server\share\bin`, `\\SERVER\SHARE\bin\`, "windows", true},
		{"windows unc root", `\\server\share`, `\\SERVER\SHARE\`, "windows", true},
		{"windows drive root", `C:\`, `c:\..\`, "windows", true},
		{"windows relative drive", `C:bin`, `C:\bin`, "windows", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OnPath(tt.dir, tt.path, tt.goos); got != tt.want {
				t.Fatalf("OnPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
