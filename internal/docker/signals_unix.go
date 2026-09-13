//go:build unix

package docker

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func interceptSignals(terminal bool) (func(*os.Process), func()) {
	signals := make(chan os.Signal, 8)
	done := make(chan struct{})
	var workers sync.WaitGroup
	// Register before starting Docker so an early signal cannot terminate dx.
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	forward := func(process *os.Process) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-done:
					return
				case sig := <-signals:
					// A terminal sends SIGINT to the entire foreground process group.
					if sig != syscall.SIGINT || !terminal {
						_ = process.Signal(sig)
					}
				}
			}
		}()
	}
	stop := func() {
		signal.Stop(signals)
		close(done)
		workers.Wait()
	}
	return forward, stop
}

func processExitCode(state *os.ProcessState) int {
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return state.ExitCode()
}
