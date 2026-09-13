//go:build !unix

package docker

import "os"

func interceptSignals(bool) (func(*os.Process), func()) {
	return func(*os.Process) {}, func() {}
}

func processExitCode(state *os.ProcessState) int {
	return state.ExitCode()
}
