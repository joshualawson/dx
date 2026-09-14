package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/route"
	"github.com/joshualawson/dx/internal/trust"
	"gopkg.in/yaml.v3"
)

func Main(e Env) int {
	code, err := execute(e)
	if err != nil {
		var reported alreadyReported
		if !errors.As(err, &reported) {
			fmt.Fprintf(e.Stderr, "dx: %s\n", err)
		}
		return 125
	}
	return code
}

func execute(e Env) (int, error) {
	var in invocation
	var err error
	if e.ShimTool != "" {
		in.Command = append([]string{e.ShimTool}, e.Args...)
	} else {
		in, err = parse(e.Args)
		if err != nil {
			return 0, err
		}
	}
	switch in.Subcommand {
	case "help", "version":
		if len(in.Command) != 0 {
			return 0, fmt.Errorf("%s does not accept arguments", in.Subcommand)
		}
		if in.Subcommand == "help" {
			_, err = io.WriteString(e.Stdout, help)
		} else {
			_, err = fmt.Fprintf(e.Stdout, "dx %s\n", e.Version)
		}
		return 0, err
	}
	if e.Err != nil {
		return 0, fmt.Errorf("get current folder: %w", e.Err)
	}
	store := &trust.Store{Path: trust.DefaultPath(e.GOOS, e.Home, e.Getenv)}
	switch in.Subcommand {
	case "shims":
		return 0, shimsCommand(e, store, in.Command)
	case "which":
		return 0, whichCommand(e, store, in.Command)
	case "ps":
		return 0, listWarm(e, in.Command)
	case "stop":
		return 0, stopWarm(e, in.Command)
	case "trust":
		return 0, approveCommand(e, store, in.Command)
	case "doctor":
		return 0, doctor(e, store, in.Command)
	case "config":
		if !optionalFlag(in.Command, "--explain") {
			return 0, fmt.Errorf("usage: dx config [--explain]")
		}
	}
	result, err := load(e, store)
	if err != nil {
		return 0, err
	}
	if in.Subcommand == "config" {
		return 0, printConfig(e.Stdout, result, len(in.Command) != 0)
	}
	if e.ShimTool != "" {
		if reason := localReason(e, result, e.ShimTool); reason != "" {
			return runLocal(e, e.ShimTool, reason)
		}
	}
	root, err := mountRoot(e, in.Mount)
	if err != nil {
		return 0, err
	}
	markers, err := e.PresentMarkers(root)
	if err != nil {
		return 0, err
	}
	image := resolve(result.Config, in.Command[0], in.Image, markers)
	ctx := context.Background()
	spec, err := runSpec(ctx, e, in, result.Config, root, image)
	if err != nil {
		return 0, err
	}
	return startContainer(ctx, e, spec, useWarm(in, result.Config, image), time.Duration(result.Config.IdleTimeout))
}

func options(e Env, store *trust.Store) config.Options {
	return config.Options{Cwd: e.Cwd, Home: e.Home, GOOS: e.GOOS, Getenv: e.Getenv, Trust: store}
}

func load(e Env, store *trust.Store) (*config.Result, error) {
	input := bufio.NewReader(e.Stdin)
	for {
		result, err := e.LoadConfig(options(e, store))
		var untrusted *config.UntrustedError
		if !errors.As(err, &untrusted) {
			return result, err
		}
		if !e.StdinTTY || !e.StderrTTY {
			for _, p := range untrusted.Paths {
				fmt.Fprintf(e.Stderr, "dx: %s is not trusted; review it and run: dx trust %s\n", p, p)
			}
			return nil, alreadyReported{}
		}
		for _, p := range untrusted.Paths {
			if err := approve(e, store, input, p, false); err != nil {
				return nil, err
			}
		}
	}
}

type alreadyReported struct{}

func (alreadyReported) Error() string { return "error already reported" }

func approve(e Env, store *trust.Store, input *bufio.Reader, filename string, yes bool) error {
	content, err := e.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read config %q: %w", filename, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("parse config %q: %w", filename, err)
	}
	fmt.Fprintln(e.Stderr, filename)
	for _, line := range trust.Describe(doc) {
		fmt.Fprintf(e.Stderr, "  %s\n", line)
	}
	if !yes {
		fmt.Fprint(e.Stderr, "Trust this file? [y/N] ")
		answer, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read approval for %q: %w", filename, err)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			return fmt.Errorf("%s is not trusted", filename)
		}
	}
	return store.Approve(filename, content)
}

func approveCommand(e Env, store *trust.Store, args []string) error {
	var paths []string
	yes, ended := false, false
	for _, arg := range args {
		switch {
		case !ended && arg == "--":
			ended = true
		case !ended && arg == "--yes":
			yes = true
		case !ended && strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown trust flag %s", arg)
		default:
			p, err := absolute(e, arg)
			if err != nil {
				return err
			}
			paths = append(paths, p)
		}
	}
	if !yes && (!e.StdinTTY || !e.StderrTTY) {
		return fmt.Errorf("dx trust requires a terminal or --yes")
	}
	if len(paths) == 0 {
		_, err := e.LoadConfig(options(e, store))
		var untrusted *config.UntrustedError
		if errors.As(err, &untrusted) {
			paths = untrusted.Paths
		} else if err != nil {
			return err
		}
	}
	input := bufio.NewReader(e.Stdin)
	for _, p := range paths {
		if err := approve(e, store, input, p, yes); err != nil {
			return err
		}
	}
	return nil
}

func optionalFlag(args []string, flag string) bool {
	return len(args) == 0 || (len(args) == 1 && args[0] == flag)
}

func resolve(cfg config.Config, cmd, image string, markers []string) route.Result {
	tools := make(map[string]route.Tool, len(cfg.Tools))
	for name, t := range cfg.Tools {
		tools[name] = route.Tool{Image: t.Image, Version: t.Version}
	}
	return route.Resolve(route.Input{Cmd: cmd, ImageFlag: image, Tools: tools, Markers: markers})
}

func printConfig(out io.Writer, result *config.Result, explain bool) error {
	data, err := yaml.Marshal(result.Config)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if !explain {
		_, err = out.Write(data)
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("decode config for explanation: %w", err)
	}
	values := map[string]any{}
	flatten("", doc, values)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "KEY\tVALUE\tORIGIN")
	for _, key := range keys {
		value, err := yaml.Marshal(values[key])
		if err != nil {
			return fmt.Errorf("encode config key %q: %w", key, err)
		}
		origin := result.Origins[key]
		if origin == "" {
			origin = "default"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\n", key, strings.ReplaceAll(strings.TrimSpace(string(value)), "\n", " "), origin)
	}
	return table.Flush()
}

func flatten(prefix string, doc map[string]any, values map[string]any) {
	for key, value := range doc {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		if child, ok := value.(map[string]any); ok && len(child) != 0 {
			flatten(name, child, values)
		} else {
			values[name] = value
		}
	}
}

func doctor(e Env, store *trust.Store, args []string) error {
	if !optionalFlag(args, "--recheck") {
		return fmt.Errorf("usage: dx doctor [--recheck]")
	}
	ctx := context.Background()
	cache := hostJoin(e.GOOS, e.StateDir, "network.json")
	if len(args) != 0 {
		if err := e.ForgetHostNetwork(cache); err != nil {
			return err
		}
	}
	bin := e.DockerBin
	if bin == "" {
		bin = "not found"
	}
	fmt.Fprintf(e.Stdout, "docker binary: %s\n", bin)
	name, err := e.Docker.ContextName(ctx)
	if err != nil {
		fmt.Fprintf(e.Stdout, "docker context: %s\n", err)
	} else {
		fmt.Fprintf(e.Stdout, "docker context: %s\n", name)
	}
	works, err := e.Docker.HostNetworkWorks(ctx, cache)
	if err != nil {
		fmt.Fprintf(e.Stdout, "host networking: %s\n", err)
	} else {
		fmt.Fprintf(e.Stdout, "host networking: %t\n", works)
	}
	containers, err := e.Docker.ListWarm(ctx)
	if err != nil {
		fmt.Fprintf(e.Stdout, "warm containers: %s\n", err)
	} else {
		fmt.Fprintf(e.Stdout, "warm containers: %d\n", len(containers))
	}
	if err := doctorShims(e); err != nil {
		fmt.Fprintf(e.Stdout, "shims: %s\n", err)
	}
	root, err := mountRoot(e, "")
	if err != nil {
		return err
	}
	markers, err := e.PresentMarkers(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "home: %s\nstate dir: %s\nmount root: %s\nmarkers: %s\n", e.Home, e.StateDir, root, strings.Join(markers, ", "))
	opts := options(e, store)
	result, err := e.LoadConfig(opts)
	var untrusted *config.UntrustedError
	if errors.As(err, &untrusted) {
		for _, filename := range untrusted.Paths {
			fmt.Fprintf(e.Stdout, "untrusted: %s\n", filename)
		}
		// Inspection must not approve or execute an untrusted configuration.
		opts.Trust = nil
		result, err = e.LoadConfig(opts)
	} else if err == nil {
		fmt.Fprintln(e.Stdout, "untrusted: none")
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, "config sources:")
	for _, source := range result.Sources {
		fmt.Fprintf(e.Stdout, "  %s\n", source.Path)
	}
	image := resolve(result.Config, "go", "", markers)
	fmt.Fprintf(e.Stdout, "dx go image: %s (%s)\n", image.Image, image.Reason)
	return nil
}
