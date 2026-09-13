package route

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want Result
	}{
		{
			name: "flag beats config builtin markers and version",
			in: Input{Cmd: "go", ImageFlag: "custom", Tools: map[string]Tool{
				"go": {Image: "ignored", Version: "1.25"},
			}, Markers: []string{"package.json"}},
			want: Result{"custom", "go", "--image flag"},
		},
		{
			name: "flag unknown command ignores markers",
			in:   Input{Cmd: "make", ImageFlag: "custom:stable", Markers: []string{"go.mod"}},
			want: Result{"custom:stable", "", "--image flag"},
		},
		{
			name: "command config beats builtin and markers",
			in: Input{Cmd: "go", Tools: map[string]Tool{
				"go": {Image: "custom", Version: "1.25"},
			}, Markers: []string{"package.json"}},
			want: Result{"custom:1.25", "go", "tools.go.image in config"},
		},
		{
			name: "unknown command image skips markers and their versions",
			in: Input{Cmd: "make", Tools: map[string]Tool{
				"make": {Image: "custom"}, "go": {Version: "1.25"}, "base": {Version: "base-version"},
			}, Markers: []string{"go.mod"}},
			want: Result{"custom:latest", "", "tools.make.image in config"},
		},
		{
			name: "unknown command image uses command version",
			in: Input{Cmd: "make", Tools: map[string]Tool{
				"make": {Image: "custom", Version: "stable"},
			}},
			want: Result{"custom:stable", "", "tools.make.image in config"},
		},
		{
			name: "builtin beats markers",
			in:   Input{Cmd: "gofmt", Markers: []string{"package.json"}},
			want: Result{DefaultRegistry + "/dx-go:latest", "go", "built-in mapping for gofmt"},
		},
		{
			name: "gofmt inherits toolchain version",
			in:   Input{Cmd: "gofmt", Tools: map[string]Tool{"go": {Version: "1.25"}}},
			want: Result{DefaultRegistry + "/dx-go:1.25", "go", "built-in mapping for gofmt"},
		},
		{
			name: "gofmt inherits toolchain image and version",
			in:   Input{Cmd: "gofmt", Tools: map[string]Tool{"go": {Image: "custom", Version: "1.25"}}},
			want: Result{"custom:1.25", "go", "tools.go.image in config"},
		},
		{
			name: "command image and version beat toolchain",
			in: Input{Cmd: "gofmt", Tools: map[string]Tool{
				"gofmt": {Image: "command", Version: "command-version"},
				"go":    {Image: "toolchain", Version: "toolchain-version"},
			}},
			want: Result{"command:command-version", "go", "tools.gofmt.image in config"},
		},
		{
			name: "command image inherits toolchain version",
			in: Input{Cmd: "gofmt", Tools: map[string]Tool{
				"gofmt": {Image: "command"}, "go": {Image: "toolchain", Version: "1.25"},
			}},
			want: Result{"command:1.25", "go", "tools.gofmt.image in config"},
		},
		{
			name: "command version overrides toolchain image version",
			in: Input{Cmd: "gofmt", Tools: map[string]Tool{
				"gofmt": {Version: "1.24"}, "go": {Image: "custom", Version: "1.25"},
			}},
			want: Result{"custom:1.24", "go", "tools.go.image in config"},
		},
		{
			name: "command version overrides builtin image version",
			in: Input{Cmd: "gofmt", Tools: map[string]Tool{
				"gofmt": {Version: "1.24"}, "go": {Version: "1.25"},
			}},
			want: Result{DefaultRegistry + "/dx-go:1.24", "go", "built-in mapping for gofmt"},
		},
		{
			name: "go marker wins independent of input order",
			in:   Input{Cmd: "make", Markers: []string{"pyproject.toml", "Cargo.toml", "package.json", "go.mod"}},
			want: Result{DefaultRegistry + "/dx-go:latest", "go", "go.mod in project"},
		},
		{
			name: "node marker beats rust and python",
			in:   Input{Cmd: "make", Markers: []string{"pyproject.toml", "Cargo.toml", "package.json"}},
			want: Result{DefaultRegistry + "/dx-node:latest", "node", "package.json in project"},
		},
		{
			name: "rust marker beats python",
			in:   Input{Cmd: "make", Markers: []string{"pyproject.toml", "Cargo.toml"}},
			want: Result{DefaultRegistry + "/dx-rust:latest", "rust", "Cargo.toml in project"},
		},
		{
			name: "python marker",
			in:   Input{Cmd: "make", Markers: []string{"pyproject.toml"}},
			want: Result{DefaultRegistry + "/dx-python:latest", "python", "pyproject.toml in project"},
		},
		{
			name: "marker inherits toolchain image and version",
			in: Input{Cmd: "make", Markers: []string{"go.mod"}, Tools: map[string]Tool{
				"go": {Image: "custom", Version: "1.25"},
			}},
			want: Result{"custom:1.25", "go", "tools.go.image in config"},
		},
		{
			name: "marker command version beats toolchain version",
			in: Input{Cmd: "make", Markers: []string{"go.mod"}, Tools: map[string]Tool{
				"make": {Version: "1.24"}, "go": {Version: "1.25"},
			}},
			want: Result{DefaultRegistry + "/dx-go:1.24", "go", "go.mod in project"},
		},
		{
			name: "custom registry",
			in:   Input{Cmd: "go", Registry: "registry.example/team"},
			want: Result{"registry.example/team/dx-go:latest", "go", "built-in mapping for go"},
		},
		{
			name: "custom registry port",
			in:   Input{Cmd: "go", Registry: "localhost:5000"},
			want: Result{"localhost:5000/dx-go:latest", "go", "built-in mapping for go"},
		},
		{
			name: "config image is not prefixed with registry",
			in:   Input{Cmd: "go", Registry: "ignored", Tools: map[string]Tool{"go": {Image: "custom"}}},
			want: Result{"custom:latest", "go", "tools.go.image in config"},
		},
		{
			name: "unknown command defaults to base",
			in:   Input{Cmd: "make"},
			want: Result{DefaultRegistry + "/dx-base:latest", "base", "default"},
		},
		{
			name: "unrecognized markers ignored",
			in:   Input{Cmd: "bash", Markers: []string{".git", "nested/go.mod", "cargo.toml"}},
			want: Result{DefaultRegistry + "/dx-base:latest", "base", "default"},
		},
		{
			name: "base toolchain image and version",
			in:   Input{Cmd: "make", Tools: map[string]Tool{"base": {Image: "custom", Version: "stable"}}},
			want: Result{"custom:stable", "base", "tools.base.image in config"},
		},
		{
			name: "base command version overrides toolchain",
			in: Input{Cmd: "make", Tools: map[string]Tool{
				"make": {Version: "command-version"}, "base": {Version: "base-version"},
			}},
			want: Result{DefaultRegistry + "/dx-base:command-version", "base", "default"},
		},
		{
			name: "base toolchain version",
			in:   Input{Cmd: "make", Tools: map[string]Tool{"base": {Version: "stable"}}},
			want: Result{DefaultRegistry + "/dx-base:stable", "base", "default"},
		},
		{
			name: "zero input",
			in:   Input{},
			want: Result{DefaultRegistry + "/dx-base:latest", "base", "default"},
		},
		{
			name: "unknown image does not inherit empty toolchain key",
			in: Input{Cmd: "make", Tools: map[string]Tool{
				"make": {Image: "custom"}, "": {Version: "ignored"},
			}},
			want: Result{"custom:latest", "", "tools.make.image in config"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.in); got != tt.want {
				t.Fatalf("Resolve(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestImageReferences(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{"img", "img:1.25"},
		{"img:stable", "img:stable"},
		{"team/img", "team/img:1.25"},
		{"team/img:stable", "team/img:stable"},
		{"localhost:5000/dx-go", "localhost:5000/dx-go:1.25"},
		{"localhost:5000/dx-go:1.23", "localhost:5000/dx-go:1.23"},
		{"localhost:5000/team/dx-go", "localhost:5000/team/dx-go:1.25"},
		{"[::1]:5000/dx-go", "[::1]:5000/dx-go:1.25"},
		{"[::1]:5000/dx-go:stable", "[::1]:5000/dx-go:stable"},
		{"img@sha256:abcdef", "img@sha256:abcdef"},
		{"img:stable@sha256:abcdef", "img:stable@sha256:abcdef"},
		{"localhost:5000/dx-go@sha256:abcdef", "localhost:5000/dx-go@sha256:abcdef"},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			in := Input{Cmd: "go", Tools: map[string]Tool{"go": {Image: tt.image, Version: "1.25"}}}
			want := Result{tt.want, "go", "tools.go.image in config"}
			if got := Resolve(in); got != want {
				t.Fatalf("Resolve(%+v) = %+v, want %+v", in, got, want)
			}
			in.ImageFlag = tt.image
			want = Result{tt.image, "go", "--image flag"}
			if got := Resolve(in); got != want {
				t.Fatalf("Resolve(%+v) = %+v, want %+v", in, got, want)
			}
		})
	}
}

func TestBuiltin(t *testing.T) {
	groups := []struct {
		toolchain string
		commands  string
	}{
		{"go", "go gofmt gopls golangci-lint dlv"},
		{"node", "node npm npx pnpm yarn corepack tsc tsx eslint prettier"},
		{"python", "python python3 pip pip3 uv uvx ruff"},
		{"rust", "cargo rustc rustup rustfmt rust-analyzer cargo-clippy clippy-driver"},
		{"infra", "terraform kubectl helm aws gcloud gsutil az"},
	}
	wantBuiltin := make(map[string]string)
	for _, group := range groups {
		for _, cmd := range strings.Fields(group.commands) {
			wantBuiltin[cmd] = group.toolchain
			t.Run(cmd, func(t *testing.T) {
				want := Result{DefaultRegistry + "/dx-" + group.toolchain + ":latest", group.toolchain, "built-in mapping for " + cmd}
				if got := Resolve(Input{Cmd: cmd}); got != want {
					t.Fatalf("Resolve(%q) = %+v, want %+v", cmd, got, want)
				}
			})
		}
	}
	if !reflect.DeepEqual(Builtin, wantBuiltin) {
		t.Fatalf("Builtin = %v, want %v", Builtin, wantBuiltin)
	}
}

func TestToolchains(t *testing.T) {
	want := map[string]string{
		"go": "dx-go", "node": "dx-node", "python": "dx-python",
		"rust": "dx-rust", "infra": "dx-infra", "base": "dx-base",
	}
	for _, tt := range []struct {
		name   string
		mutate bool
	}{
		{"contents", false},
		{"independent results", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mutate {
				previous := Toolchains()
				previous["go"] = "changed"
				delete(previous, "base")
				previous["extra"] = "extra"
			}
			if got := Toolchains(); !reflect.DeepEqual(got, want) {
				t.Fatalf("Toolchains() = %v, want %v", got, want)
			}
		})
	}
}
