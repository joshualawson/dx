package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/hostenv"
	"github.com/joshualawson/dx/internal/route"
)

func TestGoBuildTargets(t *testing.T) {
	commands := []struct {
		args  []string
		build bool
	}{
		{args: []string{"go", "build", "-o", "hello", "."}, build: true},
		{args: []string{"go", "test", "./..."}},
		{args: []string{"go", "run", "."}},
		{args: []string{"go", "vet", "./..."}},
		{args: []string{"go", "-C", "dir", "build"}, build: true},
		{args: []string{"go", "-C=dir", "build"}, build: true},
		{args: []string{"go", "-C", "build", "test", "./..."}},
		{args: []string{"go", "-C", "dir", "run", "."}},
		{args: []string{"go", "test", "build"}},
		{args: []string{"gofmt", "build"}},
		{args: []string{"make", "build"}},
		{args: []string{"go"}},
		{args: []string{"go", "-C"}},
		{},
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, command := range commands {
			for _, source := range []string{"default", "host", "config"} {
				t.Run(goos+"/"+strings.Join(command.args, " ")+"/"+source, func(t *testing.T) {
					e, _, _, _ := testEnv(t)
					e.GOOS, e.GOARCH = goos, "arm64"
					cfg := config.Config{}
					wantOS, wantArch := goos, "arm64"
					switch source {
					case "host":
						e.Environ = []string{"GOOS=freebsd", "GOARCH=386"}
						wantOS, wantArch = "freebsd", "386"
					case "config":
						e.Environ = []string{"GOOS=freebsd", "GOARCH=386"}
						cfg.Env = map[string]string{"GOOS": "openbsd", "GOARCH": "amd64"}
						wantOS, wantArch = "openbsd", "amd64"
					}
					spec, err := runSpec(context.Background(), e, invocation{Command: command.args}, cfg, e.Cwd, route.Result{Image: "dx-go:latest", Toolchain: "go"})
					if err != nil {
						t.Fatal(err)
					}
					env := envMap(spec.Env)
					if !command.build && source == "default" {
						for _, key := range []string{"GOOS", "GOARCH"} {
							if _, ok := env[key]; ok {
								t.Fatalf("unexpected %s in %v", key, spec.Env)
							}
						}
					} else if env["GOOS"] != wantOS || env["GOARCH"] != wantArch {
						t.Fatalf("targets = %s/%s, want %s/%s", env["GOOS"], env["GOARCH"], wantOS, wantArch)
					}
				})
			}
		}
	}
}

func TestIsDXImage(t *testing.T) {
	for _, tt := range []struct {
		ref  string
		want bool
	}{
		{ref: "ghcr.io/joshualawson/dx-go:1.25", want: true},
		{ref: "dx-local/dx-go:dev", want: true},
		{ref: "localhost:5000/dx-node", want: true},
		{ref: "localhost:5000/dx-node:22", want: true},
		{ref: "dx-go", want: true},
		{ref: "dx-go@sha256:abc", want: true},
		{ref: "ghcr.io/joshualawson/dx-go:1.25@sha256:abc", want: true},
		{ref: "golang:1.25"},
		{ref: "busybox"},
		{ref: "dx-local/golang:1.25"},
		{ref: "localhost:5000/golang:1.25@sha256:abc"},
		{ref: "golang:dx-go"},
		{ref: "golang@dx-go"},
		{ref: "not-dx-go"},
		{ref: ""},
	} {
		t.Run(tt.ref, func(t *testing.T) {
			if got := isDXImage(tt.ref); got != tt.want {
				t.Fatalf("isDXImage(%q) = %t, want %t", tt.ref, got, tt.want)
			}
		})
	}
}

func TestCustomImageCache(t *testing.T) {
	for _, source := range []string{"flag", "config"} {
		for _, image := range []string{"golang:1.25", "dx-local/dx-go:dev"} {
			t.Run(source+"/"+image, func(t *testing.T) {
				e, _, _, _ := testEnv(t)
				cfg := config.Config{}
				in := invocation{Command: []string{"go", "build"}}
				if source == "flag" {
					in.Image = image
				} else {
					cfg.Tools = map[string]config.Tool{"go": {Image: image}}
				}
				resolved := resolve(cfg, "go", in.Image, nil)
				spec, err := runSpec(context.Background(), e, in, cfg, e.Cwd, resolved)
				if err != nil {
					t.Fatal(err)
				}
				caches := 0
				for _, mount := range spec.Mounts {
					if strings.HasPrefix(mount.Source, "dx-cache-") {
						caches++
					}
				}
				want := 0
				if image == "dx-local/dx-go:dev" {
					want = 1
				}
				if caches != want {
					t.Fatalf("cache mounts = %d, want %d", caches, want)
				}
			})
		}
	}
}

func TestCredentialParents(t *testing.T) {
	for _, tt := range []struct {
		name        string
		credentials []hostenv.Credential
		want        []string
	}{
		{name: "builtin", credentials: hostenv.Credentials, want: []string{"/h/.cargo", "/h/.config", "/h/.docker"}},
		{name: "nested and duplicate", credentials: []hostenv.Credential{{Name: "test", Paths: []string{".config/tool/auth/token", ".config/tool/other", ".config/git", ".ssh"}}}, want: []string{"/h/.config", "/h/.config/tool", "/h/.config/tool/auth"}},
		{name: "shell characters", credentials: []hostenv.Credential{{Name: "test", Paths: []string{"space dir/quote'$(id)/token"}}}, want: []string{"/h/space dir", "/h/space dir/quote'$(id)"}},
		{name: "no parents", credentials: []hostenv.Credential{{Name: "test", Paths: []string{".ssh", ".gitconfig"}}}, want: []string{}},
		{name: "empty", want: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := credentialParents(tt.credentials); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parents = %v, want %v", got, tt.want)
			}
		})
	}
}
