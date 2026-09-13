//go:build !unix

package docker

import "context"

func interceptWarmSignals(context.Context, *Runner, string, string, bool) func() {
	return func() {}
}
