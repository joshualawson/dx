package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Runner struct {
	Bin    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (r *Runner) binary() string {
	if r.Bin != "" {
		return r.Bin
	}
	return "docker"
}

func (r *Runner) Run(ctx context.Context, args []string) (int, error) {
	cmd := exec.CommandContext(ctx, r.binary(), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Stdin, r.Stdout, r.Stderr
	if len(r.Env) != 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	stdin, _ := r.Stdin.(*os.File)
	forward, stop := interceptSignals(IsTerminal(stdin))
	defer stop()
	if err := cmd.Start(); err != nil {
		return -1, startError(r.binary(), err)
	}
	forward(cmd.Process)
	// Only startup failures are errors; the CLI owns its exit status.
	_ = cmd.Wait()
	return processExitCode(cmd.ProcessState), nil
}

func (r *Runner) Output(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.binary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return "", startError(r.binary(), err)
	}
	err := cmd.Wait()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w: %w", ctx.Err(), err)
		}
		command := strings.Join(append([]string{r.binary()}, args...), " ")
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return output, fmt.Errorf("docker command %q: %w: %s", command, err, detail)
		}
		return output, fmt.Errorf("docker command %q: %w", command, err)
	}
	return output, nil
}

func startError(bin string, err error) error {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("start docker command %q: docker must be installed and available on PATH: %w", bin, err)
	}
	return fmt.Errorf("start docker command %q: %w", bin, err)
}

func (r *Runner) ContextName(ctx context.Context) (string, error) {
	name, err := r.Output(ctx, "context", "show")
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "default"
	}
	return name, nil
}
