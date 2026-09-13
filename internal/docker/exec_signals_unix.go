//go:build unix

package docker

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const warmForwardScript = `f=$1
sig=$2
for i in 1 2 3 4 5 6 7 8 9 10; do
    if [ -s "$f" ]; then
        pids=$(cat "$f") || exit 1
        frontier=$pids
        if command -v pgrep >/dev/null 2>&1; then
            while [ -n "$frontier" ]; do
                frontier=$(for parent in $frontier; do pgrep -P "$parent" 2>/dev/null || :; done)
                pids="$pids $frontier"
            done
        fi
        kill -"$sig" $pids 2>/dev/null || :
        exit 0
    fi
    sleep 0.1
done
exit 1`

func interceptWarmSignals(_ context.Context, r *Runner, container, pidFile string, terminalTTY bool) func() {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return forwardWarmSignals(r, container, pidFile, terminalTTY, signals)
}

func forwardWarmSignals(r *Runner, container, pidFile string, terminalTTY bool, signals chan os.Signal) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sig := range signals {
			if sig == syscall.SIGINT && terminalTTY {
				continue
			}
			name := map[os.Signal]string{syscall.SIGINT: "INT", syscall.SIGTERM: "TERM", syscall.SIGHUP: "HUP"}[sig]
			// The local CLI can exit before the remote kill finishes or the pid file exists.
			forwardCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = r.Output(forwardCtx, "exec", container, "/bin/sh", "-c", warmForwardScript, "dx-exec-signal", pidFile, name)
			cancel()
		}
	}()
	return func() {
		// Stop guarantees no more sends, so closing drains queued signals before joining.
		signal.Stop(signals)
		close(signals)
		<-done
	}
}
