package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/joshualawson/dx/internal/config"
	"github.com/joshualawson/dx/internal/docker"
	"github.com/joshualawson/dx/internal/project"
	"github.com/joshualawson/dx/internal/shim"
	"github.com/joshualawson/dx/internal/trust"
)

type DockerClient interface {
	Run(context.Context, []string, []string) (int, error)
	RunWarm(context.Context, docker.RunSpec, docker.ExecSpec, time.Duration) (int, error)
	ListWarm(context.Context) ([]docker.WarmContainer, error)
	StopWarm(context.Context, ...string) error
	Output(context.Context, ...string) (string, error)
	ContextName(context.Context) (string, error)
	HostNetworkWorks(context.Context, string) (bool, error)
	SocketMount(context.Context, string, func(string) string) (docker.Mount, string, error)
}

type ShimInstaller interface {
	Install([]string) ([]string, error)
	Uninstall([]string) ([]string, error)
	List() ([]shim.Entry, error)
}

type Env struct {
	ShimTool, Exe, ShimDir, PathEnv, PathExt string
	Shims                                    ShimInstaller
	ShimErr                                  error
	OnPath                                   func(string, string, string) bool
	FindLocal                                func(string, string, string, string, string, string) (string, error)
	ExecLocal                                func(string, []string, []string) (int, error)
	Args                                     []string
	Cwd, Home                                string
	GOOS, GOARCH                             string
	Getenv                                   func(string) string
	Environ                                  []string
	Stdin                                    io.Reader
	Stdout, Stderr                           io.Writer
	StdinTTY, StdoutTTY, StderrTTY           bool
	UID, GID                                 int
	Username                                 string
	Exists                                   func(string) bool
	StateDir                                 string
	Docker                                   DockerClient
	DockerBin                                string
	Version                                  string
	Err                                      error
	LoadConfig                               func(config.Options) (*config.Result, error)
	FindRoot                                 func(string, string) (string, error)
	PresentMarkers                           func(string) ([]string, error)
	ReadFile                                 func(string) ([]byte, error)
	WriteFile                                func(string, []byte, os.FileMode) error
	MkdirAll                                 func(string, os.FileMode) error
	ForgetHostNetwork                        func(string) error
}

type runnerClient struct{ runner *docker.Runner }

func (c runnerClient) Run(ctx context.Context, args, env []string) (int, error) {
	r := *c.runner
	r.Env = env
	return r.Run(ctx, args)
}
func (c runnerClient) RunWarm(ctx context.Context, spec docker.RunSpec, command docker.ExecSpec, idleTimeout time.Duration) (int, error) {
	return c.runner.RunWarm(ctx, spec, command, idleTimeout)
}
func (c runnerClient) ListWarm(ctx context.Context) ([]docker.WarmContainer, error) {
	return c.runner.ListWarm(ctx)
}
func (c runnerClient) StopWarm(ctx context.Context, names ...string) error {
	return c.runner.StopWarm(ctx, names...)
}
func (c runnerClient) Output(ctx context.Context, args ...string) (string, error) {
	return c.runner.Output(ctx, args...)
}
func (c runnerClient) ContextName(ctx context.Context) (string, error) {
	return c.runner.ContextName(ctx)
}
func (c runnerClient) HostNetworkWorks(ctx context.Context, filename string) (bool, error) {
	return c.runner.HostNetworkWorks(ctx, filename)
}
func (c runnerClient) SocketMount(ctx context.Context, goos string, getenv func(string) string) (docker.Mount, string, error) {
	return c.runner.SocketMount(ctx, goos, getenv)
}

func System(version ...string) Env {
	cwd, err := os.Getwd()
	cwd = preferredCwd(cwd, os.Getenv("PWD"), func(a, b string) bool {
		x, e1 := os.Stat(a)
		y, e2 := os.Stat(b)
		return e1 == nil && e2 == nil && os.SameFile(x, y)
	})
	home, _ := os.UserHomeDir()
	e := Env{
		Args: os.Args[1:], Cwd: cwd, Home: home, Err: err,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Getenv: os.Getenv, Environ: os.Environ(),
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdinTTY: docker.IsTerminal(os.Stdin), StdoutTTY: docker.IsTerminal(os.Stdout), StderrTTY: docker.IsTerminal(os.Stderr),
		UID: os.Getuid(), GID: os.Getgid(), Username: "dx", Version: "dev",
		Exists:     func(p string) bool { _, err := os.Stat(p); return err == nil },
		LoadConfig: config.Load, FindRoot: project.FindRoot, PresentMarkers: project.PresentMarkers,
		ReadFile: os.ReadFile, WriteFile: os.WriteFile, MkdirAll: os.MkdirAll,
		ForgetHostNetwork: docker.ForgetHostNetwork,
	}
	if u, err := user.Current(); err == nil {
		e.Username = u.Username
		if id, err := strconv.Atoi(u.Uid); err == nil {
			e.UID = id
		}
		if id, err := strconv.Atoi(u.Gid); err == nil {
			e.GID = id
		}
	}
	if len(version) != 0 {
		e.Version = version[0]
	}
	e.ShimTool = shim.ToolName(os.Args[0], e.GOOS)
	e.Exe, err = os.Executable()
	if err == nil {
		e.Exe, err = filepath.EvalSymlinks(e.Exe)
	}
	if err != nil {
		e.ShimErr = fmt.Errorf("resolve dx executable: %w", err)
	}
	e.PathEnv, e.PathExt = e.Getenv("PATH"), e.Getenv("PATHEXT")
	e.ShimDir = shim.Dir(e.GOOS, e.Home, e.Getenv)
	e.Shims = &shim.Installer{Dir: e.ShimDir, Exe: e.Exe, GOOS: e.GOOS}
	e.OnPath, e.FindLocal, e.ExecLocal = shim.OnPath, shim.FindLocal, shim.ExecLocal
	e.StateDir = hostDir(trust.DefaultPath(e.GOOS, e.Home, e.Getenv), e.GOOS)
	e.DockerBin, _ = exec.LookPath("docker")
	e.Docker = runnerClient{&docker.Runner{Bin: e.DockerBin, Stdin: e.Stdin, Stdout: e.Stdout, Stderr: e.Stderr}}
	return e
}

func preferredCwd(cwd, pwd string, same func(string, string) bool) string {
	if pwd != "" && filepath.IsAbs(pwd) && same(cwd, pwd) {
		return pwd
	}
	return cwd
}

func hostJoin(goos string, parts ...string) string {
	if goos != "windows" {
		return filepath.Join(parts...)
	}
	joined := strings.ReplaceAll(strings.Join(parts, "/"), `\`, "/")
	volume, rest := windowsVolume(joined)
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rest)))
	return strings.ReplaceAll(volume+cleaned, "/", `\`)
}

func hostDir(filename, goos string) string {
	if goos != "windows" {
		return filepath.Dir(filename)
	}
	volume, rest := windowsVolume(strings.ReplaceAll(filename, `\`, "/"))
	return strings.ReplaceAll(volume+filepath.ToSlash(filepath.Dir(filepath.FromSlash(rest))), "/", `\`)
}

func windowsVolume(filename string) (string, string) {
	if len(filename) >= 2 && filename[1] == ':' {
		return filename[:2], filename[2:]
	}
	if strings.HasPrefix(filename, "//") {
		parts := strings.SplitN(filename[2:], "/", 3)
		if len(parts) >= 2 {
			rest := "/"
			if len(parts) == 3 {
				rest += parts[2]
			}
			return "//" + parts[0] + "/" + parts[1], rest
		}
	}
	return "", filename
}

func absolute(e Env, p string) (string, error) {
	if e.GOOS == "windows" {
		p = strings.ReplaceAll(p, "/", `\`)
		if strings.HasPrefix(p, `\\`) || (len(p) >= 3 && p[1:3] == `:\`) {
			return hostJoin(e.GOOS, p), nil
		}
		if strings.HasPrefix(p, `\`) && len(e.Cwd) >= 2 && e.Cwd[1] == ':' {
			return hostJoin(e.GOOS, e.Cwd[:2]+p), nil
		}
		if len(p) >= 2 && p[1] == ':' {
			return "", fmt.Errorf("mount path %q must not be drive-relative", p)
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return hostJoin(e.GOOS, e.Cwd, p), nil
}
