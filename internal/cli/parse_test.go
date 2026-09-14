package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want invocation
		err  string
	}{
		{name: "passthrough", args: []string{"go", "test", "-v", "./...", "--image", "untouched"}, want: invocation{Command: []string{"go", "test", "-v", "./...", "--image", "untouched"}}},
		{name: "separator", args: []string{"--", "config", "--explain"}, want: invocation{Command: []string{"config", "--explain"}}},
		{name: "separator flag", args: []string{"--", "--help"}, want: invocation{Command: []string{"--help"}}},
		{name: "values", args: []string{"--image=foo:1", "--mount", "../repo", "--port=8080:80", "--port", "9090", "--docker", "sh", "-c", "echo hi"}, want: invocation{Image: "foo:1", Mount: "../repo", Ports: []string{"8080:80", "9090"}, Docker: true, Command: []string{"sh", "-c", "echo hi"}}},
		{name: "image separate", args: []string{"--image", "foo", "config"}, want: invocation{Image: "foo", Command: []string{"config"}}},
		{name: "mount equals", args: []string{"--mount=/src", "go"}, want: invocation{Mount: "/src", Command: []string{"go"}}},
		{name: "subcommand", args: []string{"config", "--explain"}, want: invocation{Subcommand: "config", Command: []string{"--explain"}}},
		{name: "subcommand after boolean", args: []string{"--docker", "doctor"}, want: invocation{Docker: true, Command: []string{"doctor"}}},
		{name: "subcommand later", args: []string{"sh", "config"}, want: invocation{Command: []string{"sh", "config"}}},
		{name: "cold", args: []string{"--cold", "go", "version"}, want: invocation{Cold: true, Command: []string{"go", "version"}}},
		{name: "cold value", args: []string{"--cold=true", "go"}, err: "flag --cold does not take a value"},
		{name: "cold missing command", args: []string{"--cold"}, err: "a command is required"},
		{name: "cold passthrough", args: []string{"go", "--cold"}, want: invocation{Command: []string{"go", "--cold"}}},
		{name: "ps", args: []string{"ps"}, want: invocation{Subcommand: "ps", Command: []string{}}},
		{name: "stop", args: []string{"stop", "--all"}, want: invocation{Subcommand: "stop", Command: []string{"--all"}}},
		{name: "ps after flag", args: []string{"--cold", "ps"}, want: invocation{Cold: true, Command: []string{"ps"}}},
		{name: "stop after image", args: []string{"--image=dx-base", "stop"}, want: invocation{Image: "dx-base", Command: []string{"stop"}}},
		{name: "ps after separator", args: []string{"--", "ps"}, want: invocation{Command: []string{"ps"}}},
		{name: "stop after separator", args: []string{"--", "stop"}, want: invocation{Command: []string{"stop"}}},
		{name: "shims", args: []string{"shims", "install", "go"}, want: invocation{Subcommand: "shims", Command: []string{"install", "go"}}},
		{name: "which", args: []string{"which", "go"}, want: invocation{Subcommand: "which", Command: []string{"go"}}},
		{name: "shims after separator", args: []string{"--", "shims", "install"}, want: invocation{Command: []string{"shims", "install"}}},
		{name: "which after flag", args: []string{"--cold", "which", "go"}, want: invocation{Cold: true, Command: []string{"which", "go"}}},
		{name: "empty", want: invocation{Subcommand: "help"}},
		{name: "help", args: []string{"-h"}, want: invocation{Subcommand: "help", Command: []string{}}},
		{name: "long help", args: []string{"--help"}, want: invocation{Subcommand: "help", Command: []string{}}},
		{name: "version", args: []string{"--version"}, want: invocation{Subcommand: "version", Command: []string{}}},
		{name: "unknown", args: []string{"--wat", "go"}, err: "unknown flag --wat"},
		{name: "missing image", args: []string{"--image"}, err: "flag --image requires a value"},
		{name: "missing mount", args: []string{"--mount", "--docker", "go"}, err: "flag --mount requires a value"},
		{name: "missing port", args: []string{"--port="}, err: "flag --port requires a value"},
		{name: "boolean value", args: []string{"--docker=true", "go"}, err: "does not take a value"},
		{name: "no command", args: []string{"--image=foo"}, err: "a command is required"},
		{name: "empty separator", args: []string{"--"}, err: "a command is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.args)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
