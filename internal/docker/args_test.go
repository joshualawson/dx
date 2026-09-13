package docker

import (
	"encoding/csv"
	"reflect"
	"strings"
	"testing"
)

func TestRunArgs(t *testing.T) {
	tests := []struct {
		name string
		spec RunSpec
		want []string
	}{
		{name: "empty", want: []string{"run"}},
		{
			name: "every field",
			spec: RunSpec{
				Image: "dx-go:1.25", Cmd: []string{"go", "test", "a b", "--flag"},
				Name: "dx-project", Workdir: "/work", User: "1000:1000",
				GroupAdd: []string{"123", "456"}, Env: []string{"Z=last", "A=has space", "EMPTY="},
				Mounts:  []Mount{{Type: Bind, Source: "/src", Target: "/work", ReadOnly: true}},
				Network: "host", Ports: []string{"8080:80", "127.0.0.1:9000:9000/udp"},
				Labels:      map[string]string{"z": "last", "a": "first", "empty": ""},
				Interactive: true, TTY: true, Init: true, Remove: true, Detach: true,
			},
			want: []string{
				"run", "-i", "-t", "--init", "--rm", "-d", "--name", "dx-project", "--workdir", "/work", "--user", "1000:1000",
				"--group-add", "123", "--group-add", "456", "-e", "Z", "-e", "A", "-e", "EMPTY",
				"--mount", "type=bind,src=/src,dst=/work,readonly", "--network", "host",
				"-p", "8080:80", "-p", "127.0.0.1:9000:9000/udp", "--label", "a=first", "--label", "empty=", "--label", "z=last",
				"dx-go:1.25", "go", "test", "a b", "--flag",
			},
		},
		{
			name: "image and command only",
			spec: RunSpec{Image: "busybox:1.37", Cmd: []string{"echo", ""}},
			want: []string{"run", "busybox:1.37", "echo", ""},
		},
		{
			name: "all mount types",
			spec: RunSpec{Mounts: []Mount{
				{Type: Bind, Source: "/a", Target: "/b"},
				{Type: Volume, Source: "dx-cache", Target: "/cache", ReadOnly: true},
				{Type: Tmpfs, Target: "/tmp"},
			}},
			want: []string{"run", "--mount", "type=bind,src=/a,dst=/b", "--mount", "type=volume,src=dx-cache,dst=/cache,readonly", "--mount", "type=tmpfs,dst=/tmp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 20 {
				if got := RunArgs(tt.spec); !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("RunArgs() = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestRunArgsEnv(t *testing.T) {
	for _, tt := range []struct {
		name    string
		env     []string
		args    []string
		process []string
	}{
		{name: "empty", args: []string{"run"}},
		{
			name:    "secrets and empty values",
			env:     []string{"GH_TOKEN=secret", "AWS_SECRET_ACCESS_KEY=a=b c", "EMPTY="},
			args:    []string{"run", "-e", "GH_TOKEN", "-e", "AWS_SECRET_ACCESS_KEY", "-e", "EMPTY"},
			process: []string{"GH_TOKEN=secret", "AWS_SECRET_ACCESS_KEY=a=b c", "EMPTY="},
		},
		{
			name: "docker cli exceptions",
			env:  []string{"HOME=/container", "PATH=/container/bin", "DOCKER_HOST=tcp://container:2375", "DOCKER_CONTEXT=container", "DOCKER_CONFIG=/config", "DOCKER_TLS_VERIFY=", "DOCKER_CUSTOM=value"},
			args: []string{"run", "-e", "HOME=/container", "-e", "PATH=/container/bin", "-e", "DOCKER_HOST=tcp://container:2375", "-e", "DOCKER_CONTEXT=container", "-e", "DOCKER_CONFIG=/config", "-e", "DOCKER_TLS_VERIFY=", "-e", "DOCKER_CUSTOM=value"},
		},
		{
			name:    "mixed keys and exact exceptions",
			env:     []string{"GH_TOKEN=secret", "HOME=/container", "HOMELESS=yes", "PATH_SUFFIX=/bin", "DOCKERISH=value", "docker_host=ordinary"},
			args:    []string{"run", "-e", "GH_TOKEN", "-e", "HOME=/container", "-e", "HOMELESS", "-e", "PATH_SUFFIX", "-e", "DOCKERISH", "-e", "docker_host"},
			process: []string{"GH_TOKEN=secret", "HOMELESS=yes", "PATH_SUFFIX=/bin", "DOCKERISH=value", "docker_host=ordinary"},
		},
		{
			name:    "last occurrence wins",
			env:     []string{"Z=old", "HOME=/old", "A=first", "DOCKER_CONTEXT=old", "Z=new", "HOME=/new", "DOCKER_CONTEXT=new"},
			args:    []string{"run", "-e", "A", "-e", "Z", "-e", "HOME=/new", "-e", "DOCKER_CONTEXT=new"},
			process: []string{"A=first", "Z=new"},
		},
		{
			name:    "last empty value wins",
			env:     []string{"GH_TOKEN=old", "PATH=/old", "GH_TOKEN=", "PATH="},
			args:    []string{"run", "-e", "GH_TOKEN", "-e", "PATH="},
			process: []string{"GH_TOKEN="},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := RunSpec{Env: append([]string(nil), tt.env...)}
			for range 2 {
				if got := RunArgs(spec); !reflect.DeepEqual(got, tt.args) {
					t.Fatalf("RunArgs() = %#v, want %#v", got, tt.args)
				}
				if got := ProcessEnv(spec); !reflect.DeepEqual(got, tt.process) {
					t.Fatalf("ProcessEnv() = %#v, want %#v", got, tt.process)
				}
			}
			if !reflect.DeepEqual(spec.Env, tt.env) {
				t.Fatal("environment helpers mutated the spec")
			}
		})
	}
}

func TestRunArgsIndependentFlags(t *testing.T) {
	for _, tt := range []struct {
		name string
		spec RunSpec
		flag string
	}{
		{"interactive", RunSpec{Interactive: true}, "-i"},
		{"tty", RunSpec{TTY: true}, "-t"},
		{"init", RunSpec{Init: true}, "--init"},
		{"remove", RunSpec{Remove: true}, "--rm"},
		{"detach", RunSpec{Detach: true}, "-d"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := []string{"run", tt.flag}
			if got := RunArgs(tt.spec); !reflect.DeepEqual(got, want) {
				t.Fatalf("RunArgs() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestMountCSV(t *testing.T) {
	tests := []struct {
		name   string
		mount  Mount
		want   string
		fields []string
	}{
		{
			name:   "windows colons",
			mount:  Mount{Type: Bind, Source: `C:\Users\Jane Doe\src`, Target: "/c/src"},
			want:   `type=bind,src=C:\Users\Jane Doe\src,dst=/c/src`,
			fields: []string{"type=bind", `src=C:\Users\Jane Doe\src`, "dst=/c/src"},
		},
		{
			name:   "comma",
			mount:  Mount{Type: Bind, Source: "/a,b", Target: "/x,y", ReadOnly: true},
			want:   `type=bind,"src=/a,b","dst=/x,y",readonly`,
			fields: []string{"type=bind", "src=/a,b", "dst=/x,y", "readonly"},
		},
		{
			name:   "quotes",
			mount:  Mount{Type: Bind, Source: `/a"b,c`, Target: `/x"y`},
			want:   `type=bind,"src=/a""b,c","dst=/x""y"`,
			fields: []string{"type=bind", `src=/a"b,c`, `dst=/x"y`},
		},
		{
			name:   "windows comma and quote",
			mount:  Mount{Type: Bind, Source: `C:\a,b\"c`, Target: "/work"},
			want:   `type=bind,"src=C:\a,b\""c",dst=/work`,
			fields: []string{"type=bind", `src=C:\a,b\"c`, "dst=/work"},
		},
		{
			name:   "tmpfs readonly",
			mount:  Mount{Type: Tmpfs, Target: "/tmp", ReadOnly: true},
			want:   "type=tmpfs,dst=/tmp,readonly",
			fields: []string{"type=tmpfs", "dst=/tmp", "readonly"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RunArgs(RunSpec{Mounts: []Mount{tt.mount}})[2]
			if got != tt.want {
				t.Fatalf("mount = %q, want %q", got, tt.want)
			}
			fields, err := csv.NewReader(strings.NewReader(got)).Read()
			if err != nil || !reflect.DeepEqual(fields, tt.fields) {
				t.Fatalf("CSV fields = %#v, %v, want %#v", fields, err, tt.fields)
			}
		})
	}
}
