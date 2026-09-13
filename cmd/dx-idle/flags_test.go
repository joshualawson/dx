package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want options
		err  bool
		help bool
	}{
		{name: "defaults", want: options{30 * time.Minute, 5 * time.Second}},
		{name: "custom", args: []string{"--timeout", "2m", "--interval", "10ms"}, want: options{2 * time.Minute, 10 * time.Millisecond}},
		{name: "disabled", args: []string{"--timeout", "0"}, want: options{0, 5 * time.Second}},
		{name: "equals", args: []string{"--timeout=1h", "--interval=1s"}, want: options{time.Hour, time.Second}},
		{name: "unknown", args: []string{"--unknown"}, err: true},
		{name: "missing duration", args: []string{"--timeout"}, err: true},
		{name: "invalid duration", args: []string{"--timeout", "soon"}, err: true},
		{name: "duration overflow", args: []string{"--timeout", "999999999999999999h"}, err: true},
		{name: "negative timeout", args: []string{"--timeout=-1s"}, err: true},
		{name: "negative interval", args: []string{"--interval=-1s"}, err: true},
		{name: "zero interval", args: []string{"--interval=0"}, err: true},
		{name: "disabled still validates interval", args: []string{"--timeout=0", "--interval=0"}, err: true},
		{name: "positional argument", args: []string{"extra"}, err: true},
		{name: "argument after separator", args: []string{"--", "extra"}, err: true},
		{name: "help", args: []string{"--help"}, err: true, help: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := parseFlags(tt.args, &output)
			if (err != nil) != tt.err {
				t.Fatalf("parseFlags() error = %v, want error %v", err, tt.err)
			}
			if !tt.err && got != tt.want {
				t.Fatalf("parseFlags() = %+v, want %+v", got, tt.want)
			}
			if errors.Is(err, flag.ErrHelp) != tt.help {
				t.Fatalf("parseFlags() help = %v, want %v", err, tt.help)
			}
			if tt.help && output.Len() == 0 {
				t.Fatal("help did not print usage")
			}
			if err != nil && !tt.help && strings.Count(output.String(), err.Error()) != 1 {
				t.Fatalf("expected error exactly once, got %q", output.String())
			}
		})
	}
}
