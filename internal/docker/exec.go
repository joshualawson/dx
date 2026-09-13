package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type ExecSpec struct {
	Container   string
	Cmd         []string
	Workdir     string
	User        string
	Env         []string
	Interactive bool
	TTY         bool
}

func ExecArgs(spec ExecSpec) []string {
	args := []string{"exec"}
	if spec.Interactive {
		args = append(args, "-i")
	}
	if spec.TTY {
		args = append(args, "-t")
	}
	if spec.Workdir != "" {
		args = append(args, "--workdir", spec.Workdir)
	}
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}
	args = append(args, envArgs(spec.Env)...)
	if spec.Container != "" {
		args = append(args, spec.Container)
	}
	return append(args, spec.Cmd...)
}

func ExecProcessEnv(spec ExecSpec) []string {
	return ProcessEnv(RunSpec{Env: spec.Env})
}

func (r *Runner) RunWarm(ctx context.Context, spec RunSpec, command ExecSpec, idleTimeout time.Duration) (int, error) {
	runner := *r
	// Per-command credentials must not leak into later calls through a shared runner.
	runner.Env = ExecProcessEnv(command)
	for attempt := 0; ; attempt++ {
		name, err := r.EnsureWarm(ctx, spec, idleTimeout)
		if err != nil {
			return -1, err
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return -1, fmt.Errorf("create docker exec pid identifier: %w", err)
		}
		pidFile := "/tmp/dx-exec-" + hex.EncodeToString(id[:]) + ".pid"
		wrapped := command
		wrapped.Container = name
		wrapped.Cmd = append([]string{"/bin/sh", "-c", "echo $$ > " + pidFile + ` && exec "$@"`, "dx-exec"}, command.Cmd...)
		code, err := runner.runWarmExec(ctx, wrapped, pidFile)
		if err != nil || code == 0 || attempt == 1 || ctx.Err() != nil {
			return code, err
		}
		if !r.canRetryWarm(ctx, name, pidFile) {
			return code, nil
		}
	}
}

func (r *Runner) runWarmExec(ctx context.Context, spec ExecSpec, pidFile string) (int, error) {
	cmd := exec.CommandContext(ctx, r.binary(), ExecArgs(spec)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.Stdin, r.Stdout, r.Stderr
	if len(r.Env) != 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	stdin, _ := r.Stdin.(*os.File)
	stop := interceptWarmSignals(ctx, r, spec.Container, pidFile, IsTerminal(stdin) && spec.TTY)
	defer stop()
	if err := cmd.Start(); err != nil {
		return -1, startError(r.binary(), err)
	}
	_ = cmd.Wait()
	return processExitCode(cmd.ProcessState), nil
}

func (r *Runner) canRetryWarm(ctx context.Context, name, pidFile string) bool {
	exists, running, err := r.warmState(ctx, name)
	if err != nil || running {
		return false
	}
	if !exists {
		return true
	}
	_, err = exec.CommandContext(ctx, r.binary(), "exec", name, "test", "-f", pidFile).Output()
	var exitError *exec.ExitError
	// The CLI can also exit 1; only a silent test failure proves the pid file is absent.
	return errors.As(err, &exitError) && exitError.ExitCode() == 1 && len(exitError.Stderr) == 0 && ctx.Err() == nil
}
