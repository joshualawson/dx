package main

import (
	"flag"
	"fmt"
	"io"
	"time"
)

type options struct {
	timeout  time.Duration
	interval time.Duration
}

func parseFlags(args []string, output io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("dx-idle", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.DurationVar(&opts.timeout, "timeout", 30*time.Minute, "idle timeout; 0 disables idle shutdown")
	flags.DurationVar(&opts.interval, "interval", 5*time.Second, "process scan interval")
	flags.Usage = func() {
		fmt.Fprintln(output, "usage: dx-idle [--timeout 30m] [--interval 5s]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	var err error
	switch {
	case flags.NArg() != 0:
		err = fmt.Errorf("dx-idle: unexpected argument %q", flags.Arg(0))
	case opts.timeout < 0:
		err = fmt.Errorf("dx-idle: timeout must not be negative")
	case opts.interval <= 0:
		err = fmt.Errorf("dx-idle: interval must be positive")
	}
	if err != nil {
		fmt.Fprintln(output, err)
	}
	return opts, err
}
