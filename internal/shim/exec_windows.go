//go:build windows

package shim

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

func ExecLocal(path string, args, env []string) (int, error) {
	// The child receives console interrupts directly; dx must survive to report its status.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return -1, fmt.Errorf("run local executable %q: %w", path, err)
}
