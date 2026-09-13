//go:build !unix

package docker

func fakePlatform([]string) int {
	return 93
}
