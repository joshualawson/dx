package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/docker"
	"github.com/joshualawson/dx/internal/route"
)

func useWarm(in invocation, cfg config.Config, image route.Result) bool {
	if len(in.Ports) != 0 || in.Cold || !isDXImage(image.Image) {
		return false
	}
	for _, name := range []string{in.Command[0], image.Toolchain} {
		if warm := cfg.Tools[name].Warm; warm != nil {
			return *warm
		}
	}
	switch image.Toolchain {
	case "go", "node", "python", "rust":
		return true
	}
	return false
}

func startContainer(ctx context.Context, e Env, spec docker.RunSpec, warm bool, idleTimeout time.Duration) (int, error) {
	if err := prepareHome(ctx, e, spec.Image); err != nil {
		return 0, err
	}
	if warm {
		command := docker.ExecSpec{
			Cmd: spec.Cmd, Workdir: spec.Workdir, User: spec.User, Env: spec.Env,
			Interactive: true, TTY: spec.TTY,
		}
		return e.Docker.RunWarm(ctx, spec, command, idleTimeout)
	}
	return e.Docker.Run(ctx, docker.RunArgs(spec), docker.ProcessEnv(spec))
}

func listWarm(e Env, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: dx ps")
	}
	containers, err := e.Docker.ListWarm(context.Background())
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		_, err := fmt.Fprintln(e.Stdout, "no warm containers")
		return err
	}
	table := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "NAME\tTOOLCHAIN\tROOT\tIMAGE\tSTATUS")
	for _, container := range containers {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", container.Name, container.Toolchain, container.Root, container.Image, container.Status)
	}
	return table.Flush()
}

func stopWarm(e Env, args []string) error {
	if !optionalFlag(args, "--all") {
		return fmt.Errorf("usage: dx stop [--all]")
	}
	var root string
	if len(args) == 0 {
		var err error
		root, err = e.FindRoot(e.Cwd, e.Home)
		if err != nil {
			return err
		}
	}
	ctx := context.Background()
	containers, err := e.Docker.ListWarm(ctx)
	if err != nil {
		return err
	}
	var names []string
	for _, container := range containers {
		if len(args) != 0 || container.Root == root {
			names = append(names, container.Name)
		}
	}
	if len(names) == 0 {
		_, err := fmt.Fprintln(e.Stdout, "no warm containers to stop")
		return err
	}
	if err := e.Docker.StopWarm(ctx, names...); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fmt.Fprintln(e.Stdout, name); err != nil {
			return err
		}
	}
	return nil
}
