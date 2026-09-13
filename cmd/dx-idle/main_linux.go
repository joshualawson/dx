//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, output io.Writer) int {
	opts, err := parseFlags(args, output)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	children := make(chan os.Signal, 1)
	signal.Notify(children, syscall.SIGCHLD)
	defer signal.Stop(children)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			if err := reapChildren(syscall.Wait4); err != nil {
				fmt.Fprintln(output, err)
			}
			select {
			case <-ctx.Done():
				return
			case <-children:
			}
		}
	}()

	watcher := Watcher{
		ProcRoot: "/proc",
		Self:     os.Getpid(),
		Timeout:  opts.timeout,
		Interval: opts.interval,
		Stderr:   output,
	}
	err = watcher.Run(ctx)
	stop()
	<-finished
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(output, err)
		return 1
	}
	return 0
}

func reapChildren(wait func(int, *syscall.WaitStatus, int, *syscall.Rusage) (int, error)) error {
	for {
		var status syscall.WaitStatus
		pid, err := wait(-1, &status, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.ECHILD) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("dx-idle: wait4: %w", err)
		}
		if pid == 0 {
			return nil
		}
	}
}
