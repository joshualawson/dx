package cli

import (
	"fmt"
	"strings"
)

type invocation struct {
	Image, Mount string
	Ports        []string
	Docker, Cold bool
	Command      []string
	Subcommand   string
}

func parse(args []string) (invocation, error) {
	var in invocation
	flags := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			in.Command = args[i+1:]
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if !flags && isSubcommand(arg) {
				in.Subcommand = arg
				in.Command = args[i+1:]
			} else {
				in.Command = args[i:]
			}
			return in, nil
		}
		if !flags && (arg == "--version" || arg == "--help" || arg == "-h") {
			in.Subcommand = "help"
			if arg == "--version" {
				in.Subcommand = "version"
			}
			in.Command = args[i+1:]
			return in, nil
		}
		flags = true
		name, value, assigned := strings.Cut(arg, "=")
		switch name {
		case "--docker", "--cold":
			if assigned {
				return in, fmt.Errorf("flag %s does not take a value", name)
			}
			if name == "--docker" {
				in.Docker = true
			} else {
				in.Cold = true
			}
		case "--image", "--mount", "--port":
			if !assigned {
				if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
					return in, fmt.Errorf("flag %s requires a value", name)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return in, fmt.Errorf("flag %s requires a value", name)
			}
			switch name {
			case "--image":
				in.Image = value
			case "--mount":
				in.Mount = value
			case "--port":
				in.Ports = append(in.Ports, value)
			}
		default:
			return in, fmt.Errorf("unknown flag %s", name)
		}
	}
	if len(in.Command) == 0 {
		if len(args) == 0 {
			in.Subcommand = "help"
		} else {
			return in, fmt.Errorf("a command is required")
		}
	}
	return in, nil
}

func isSubcommand(s string) bool {
	switch s {
	case "config", "trust", "doctor", "version", "help", "ps", "stop":
		return true
	}
	return false
}

const help = `Usage: dx [dx flags] <cmd> [args...]
       dx -- <cmd> [args...]
       dx config [--explain]
       dx trust [--yes] [path...]
       dx doctor [--recheck]
       dx ps
       dx stop [--all]
       dx version | dx --version
       dx help | dx -h | dx --help

Flags (before the command only; --flag=value is also accepted):
  --image <ref>   Override the image
  --mount <path>  Override the mounted project folder
  --port <spec>   Publish a port using Docker -p syntax (repeatable)
  --docker       Mount the Docker socket
  --cold         Force a fresh container

Subcommands are special only without preceding dx flags.
Use dx -- config to run a tool named config; dx --image foo config also runs it.
All tool arguments pass through unchanged.
dx images for go, node, python and rust reuse a warm container per project
until it has been idle for idle_timeout (default 30m).
Tool exit codes pass through; dx errors exit 125.
`
