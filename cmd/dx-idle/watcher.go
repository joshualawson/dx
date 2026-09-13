package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Watcher struct {
	ProcRoot string
	Self     int
	Timeout  time.Duration
	Interval time.Duration
	Now      func() time.Time
	Stderr   io.Writer
}

func (w *Watcher) Busy() (bool, error) {
	entries, err := os.ReadDir(w.ProcRoot)
	if err != nil {
		return true, fmt.Errorf("read %s: %w", w.ProcRoot, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.IndexFunc(name, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			continue
		}
		pid, err := strconv.Atoi(name)
		if err != nil || pid == w.Self {
			continue
		}
		statPath := filepath.Join(w.ProcRoot, name, "stat")
		stat, err := os.ReadFile(statPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return true, fmt.Errorf("read %s: %w", statPath, err)
		}
		// Process names can contain spaces, parentheses and newlines.
		end := strings.LastIndexByte(string(stat), ')')
		if end < 0 {
			return true, fmt.Errorf("read %s: missing process name", statPath)
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) == 0 || len(fields[0]) != 1 {
			return true, fmt.Errorf("read %s: invalid process state", statPath)
		}
		if fields[0] != "Z" {
			return true, nil
		}
	}
	return false, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	logged := make(map[string]bool)
	logOnce := func(err error) {
		if w.Stderr == nil {
			return
		}
		message := err.Error()
		if !logged[message] {
			logged[message] = true
			fmt.Fprintln(w.Stderr, message)
		}
	}
	if w.Timeout < 0 {
		logOnce(fmt.Errorf("dx-idle: timeout must not be negative"))
	}
	if w.Interval <= 0 {
		logOnce(fmt.Errorf("dx-idle: interval must be positive"))
	}
	// Invalid settings must not endanger commands either; the CLI rejects them before startup.
	if w.Timeout <= 0 || w.Interval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	now := w.Now
	if now == nil {
		now = time.Now
	}
	// Startup grants the first exec a full idle window.
	lastActive := now()
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			busy, err := w.Busy()
			if err != nil {
				// A failed scan cannot prove that the container is idle.
				logOnce(err)
				busy = true
			}
			current := now()
			if busy {
				lastActive = current
			} else if current.Sub(lastActive) >= w.Timeout {
				return nil
			}
		}
	}
}
