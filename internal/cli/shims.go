package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/route"
	"github.com/joshualawson/dx/internal/shim"
	"github.com/joshualawson/dx/internal/trust"
)

func localReason(e Env, result *config.Result, tool string) string {
	switch strings.ToLower(e.Getenv("DX_LOCAL")) {
	case "1", "true", "yes":
		return "DX_LOCAL=1"
	}
	if result.Config.RunsLocal(tool) {
		return "local in " + result.Origins["local"]
	}
	return ""
}

func findLocal(e Env, tool string) (string, error) {
	if e.ShimErr != nil {
		return "", e.ShimErr
	}
	return e.FindLocal(tool, e.PathEnv, e.PathExt, e.GOOS, e.ShimDir, e.Exe)
}

func runLocal(e Env, tool, reason string) (int, error) {
	filename, err := findLocal(e, tool)
	if errors.Is(err, shim.ErrNotFound) {
		return 0, fmt.Errorf("%s is set to run locally (%s) but no %s was found on PATH outside %s", tool, reason, tool, e.ShimDir)
	}
	if err != nil {
		return 0, err
	}
	return e.ExecLocal(filename, e.Args, e.Environ)
}

func shimsCommand(e Env, store *trust.Store, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dx shims install|uninstall [tool...] | dx shims list")
	}
	switch args[0] {
	case "install", "uninstall":
		for _, tool := range args[1:] {
			if strings.HasPrefix(tool, "-") {
				return fmt.Errorf("usage: dx shims %s [tool...]", args[0])
			}
		}
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: dx shims list")
		}
	default:
		return fmt.Errorf("usage: dx shims install|uninstall [tool...] | dx shims list")
	}
	if e.ShimErr != nil {
		return e.ShimErr
	}
	switch args[0] {
	case "install":
		tools := args[1:]
		if len(tools) == 0 {
			for tool := range route.Builtin {
				tools = append(tools, tool)
			}
			sort.Strings(tools)
		}
		installed, err := e.Shims.Install(tools)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "installed %d shims in %s\n", len(installed), e.ShimDir)
		if !e.OnPath(e.ShimDir, e.PathEnv, e.GOOS) {
			if e.GOOS == "windows" {
				fmt.Fprintf(e.Stdout, "add %s to the start of your user PATH (System Properties → Environment Variables)\n", e.ShimDir)
			} else {
				fmt.Fprintf(e.Stdout, "add to your shell profile: export PATH=\"%s:$PATH\"\n", e.ShimDir)
			}
		}
		return nil
	case "uninstall":
		removed, err := e.Shims.Uninstall(args[1:])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(e.Stdout, "removed %d shims\n", len(removed))
		return err
	}
	result, err := load(e, store)
	if err != nil {
		return err
	}
	entries, err := e.Shims.List()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(e.Stdout, "no shims installed")
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Tool < entries[j].Tool })
	table := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "TOOL\tRUNS\tPATH")
	for _, entry := range entries {
		runs := "dx"
		if reason := localReason(e, result, entry.Tool); reason != "" {
			runs = "local (" + reason + ")"
		}
		if entry.Stale {
			runs += " stale"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\n", entry.Tool, runs, entry.Path)
	}
	return table.Flush()
}

func whichCommand(e Env, store *trust.Store, args []string) error {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: dx which <tool>")
	}
	if e.ShimErr != nil {
		return e.ShimErr
	}
	result, err := load(e, store)
	if err != nil {
		return err
	}
	tool := args[0]
	entries, err := e.Shims.List()
	if err != nil {
		return err
	}
	missing := false
	if reason := localReason(e, result, tool); reason != "" {
		filename, err := findLocal(e, tool)
		if errors.Is(err, shim.ErrNotFound) {
			fmt.Fprintf(e.Stdout, "%s: local not found (%s)\n", tool, reason)
			missing = true
		} else if err != nil {
			return err
		} else {
			fmt.Fprintf(e.Stdout, "%s: local %s (%s)\n", tool, filename, reason)
		}
	} else {
		root, err := mountRoot(e, "")
		if err != nil {
			return err
		}
		markers, err := e.PresentMarkers(root)
		if err != nil {
			return err
		}
		image := resolve(result.Config, tool, "", markers)
		lifecycle := "cold"
		if useWarm(invocation{Command: []string{tool}}, result.Config, image) {
			lifecycle = "warm"
		}
		fmt.Fprintf(e.Stdout, "%s: dx %s (%s), %s\n", tool, image.Image, image.Reason, lifecycle)
	}
	status := "not installed"
	for _, entry := range entries {
		if entry.Tool == tool {
			status = "installed"
			if entry.Stale {
				status = "stale"
			}
			break
		}
	}
	fmt.Fprintf(e.Stdout, "shim: %s in %s; shims dir on PATH: %s\n", status, e.ShimDir, yesNo(e.OnPath(e.ShimDir, e.PathEnv, e.GOOS)))
	if missing {
		return alreadyReported{}
	}
	return nil
}

func doctorShims(e Env) error {
	if e.ShimErr != nil {
		return e.ShimErr
	}
	entries, err := e.Shims.List()
	if err != nil {
		return err
	}
	stale := 0
	for _, entry := range entries {
		if entry.Stale {
			stale++
		}
	}
	_, err = fmt.Fprintf(e.Stdout, "shims: %s (on PATH: %s, %d installed, %d stale)\n", e.ShimDir, yesNo(e.OnPath(e.ShimDir, e.PathEnv, e.GOOS)), len(entries), stale)
	return err
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
