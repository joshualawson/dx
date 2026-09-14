# dx

Run development tools in Docker as if they were installed locally.

dx runs development tools in Docker containers as if they were installed locally. A new machine needs only Docker and the `dx` binary.

```sh
dx go build .
dx npm install
dx cargo test
dx terraform plan
```

## Requirements

Docker Desktop, OrbStack, Colima, or Docker Engine on Linux, running on macOS, Linux, or Windows.

## Install

Download the archive for your OS and architecture from [GitHub Releases](https://github.com/joshualawson/dx/releases), then put `dx` on your `PATH`.

Or install with Go:

```sh
go install github.com/joshualawson/dx/cmd/dx@latest
```

## Quick start

In a Go project:

```sh
cd my-go-project
dx go build .
```

The binary is written to your host project and targets your host OS and architecture. Other common commands:

```sh
dx npm install
dx cargo test
dx terraform plan
dx doctor
```

## How it works

### Image choice

The first match wins: `--image`, `tools.<command>.image` in config, the built-in command map, project markers, then `dx-base`.

| Toolchain | Commands |
| --- | --- |
| Go | `go`, `gofmt`, `gopls`, `golangci-lint`, `dlv` |
| Node | `node`, `npm`, `npx`, `pnpm`, `yarn`, `corepack`, `tsc`, `tsx`, `eslint`, `prettier` |
| Python | `python`, `python3`, `pip`, `pip3`, `uv`, `uvx`, `ruff` |
| Rust | `cargo`, `rustc`, `rustup`, `rustfmt`, `rust-analyzer`, `cargo-clippy`, `clippy-driver` |
| Infra | `terraform`, `kubectl`, `helm`, `aws`, `gcloud`, `gsutil`, `az` |

For commands not in that table, project markers select an image: `go.mod`, `package.json`, `Cargo.toml`, or `pyproject.toml`. Otherwise dx uses `dx-base`.

### Project root mounting

The dx CLI searches upward for `.git`, `go.mod`, `package.json`, `Cargo.toml`, or `pyproject.toml`, never above your home folder. It mounts the first matching folder; without one, it mounts the current folder. Use `--mount <path>` to override this.

### Warm containers

The dx images for Go, Node, Python, and Rust reuse one container per project until `idle_timeout` expires (30 minutes by default). Infra and custom images run fresh containers. `--port` and `--cold` force a fresh container. `tools.<name>.warm` overrides the default.

```sh
dx ps
dx stop
dx stop --all
```

### Shims

A shim is a link named after a tool, such as `go` or `npm`, that runs dx; all its arguments go to the tool, not dx flags. Install every built-in command, or name individual tools:

```sh
dx shims install
dx shims install go npm
```

Put the shims folder before local tool installs on `PATH`. The install command prints this reminder when needed.

| OS | Shim folder | PATH setup |
| --- | --- | --- |
| macOS, Linux | `$XDG_DATA_HOME/dx/bin` (default `~/.local/share/dx/bin`) | `export PATH="$HOME/.local/share/dx/bin:$PATH"` |
| Windows | `%LOCALAPPDATA%\dx\bin` | Add the folder to the start of your user `PATH` in System Properties → Environment Variables. |

Shims are symlinks on macOS and Linux. On Windows they are hard links, or copies when linking is unavailable. Run a local installation for one command with `DX_LOCAL=1`, or configure `local`:

```sh
DX_LOCAL=1 go test ./...
```

```yaml
local: [go, npm]
```

Local lookup skips the shims folder and other dx links. There is no fallback: a selected local tool that is not found exits 125. Direct `dx <tool>` calls always use containers.

```sh
dx which go
dx shims list
dx shims uninstall go
dx shims uninstall
```

`dx which` reports the image, reason, and warm/cold state a shim would use from the current folder, or the selected local executable. It also reports shim and `PATH` status. `dx shims list` marks stale shims; re-run `dx shims install` after upgrading dx to refresh an old symlink or Windows copy. Uninstall only removes dx shims.

Language servers such as `gopls` and `rust-analyzer` work through shims on macOS and Linux because container paths match host paths. The first call in a project starts a warm container and may take a few seconds; later calls are fast. On Windows, `/c/...` container paths are not editor-mapped, so use shims for command-line tools rather than language servers.

### Files and ownership

On Linux, commands run as your uid and gid. A named `dx-home` volume is mounted at your home path in the container; selected credential files and folders are mounted within it.

### Go builds

`go build` defaults `GOOS` and `GOARCH` to the host so its output runs on the host. `go test`, `go run`, and other Go commands run for Linux in the container. Host environment values and config override these defaults.

## Configuration

### Files and merging

The dx CLI loads the global config at `~/.config/dx/config.yaml` (`%APPDATA%\dx\config.yaml` on Windows), or the file named by `DX_CONFIG`. It then loads `.dx.yaml` files upward from the current folder; nearer files win.

Maps merge by key. Scalars and lists replace inherited values; use `key+` to append to a list. `root: true` stops the upward search.

```yaml
# ~/.config/dx/config.yaml
# Choose image tags and warm behaviour by toolchain or command.
tools:
  go:
    image: ghcr.io/joshualawson/dx-go
    version: "1.25"
    warm: true
  terraform:
    warm: false
# Make these shims run existing local tools instead of containers.
local: [go, npm]
# Add environment values after filtered host values.
env:
  GOPRIVATE: github.com/example/*
# Allow Docker commands inside containers.
docker: false
# Disable all credential sharing, or override individual credentials.
credentials:
  all: true
  aws: false
# Trust project config below these folders without prompting.
trusted:
  - ~/Development
# Stop warm containers after this idle period.
idle_timeout: 30m
```

Inspect the merged result and value origins with:

```sh
dx config --explain
```

## Trust

A repository `.dx.yaml` can select images, change env, share credentials, and turn on the Docker socket, so dx asks before using untrusted files. `local` needs no approval because it can only run tools already on your `PATH`. Global config can list trusted folders. Approvals record the path and file hash, so changed files need approval again.

```sh
dx trust
dx trust --yes path/to/.dx.yaml
```

Only `tools.<name>.version` and `root: false` do not need approval. `root: true` needs approval because it discards parent settings.

## Credentials and environment

Credential sharing is on by default. The dx CLI mounts existing host paths writable at their usual locations:

- `~/.ssh` and the SSH agent socket
- `~/.gitconfig`, `~/.config/git`, `~/.netrc`
- `~/.aws`, `~/.config/gcloud`, `~/.azure`, `~/.kube`
- `~/.config/gh`, `~/.docker/config.json`
- `~/.npmrc`, `~/.pypirc`, `~/.cargo/credentials.toml`, `~/.terraform.d`

Set `credentials.all: false` to disable sharing, or set an individual credential such as `credentials.aws: false`. Host environment variables pass through after host installation-location variables are filtered. Values use Docker environment passing, so tokens do not appear in `ps` output.

Credentials held only in the macOS Keychain or Windows Credential Manager, including some git, `gh`, and Docker Desktop credential helpers, are not available inside the Linux container.

## Networking and Docker

The dx CLI uses host networking where Docker supports it. It probes the capability once per Docker context; recheck it with:

```sh
dx doctor --recheck
```

Where host networking is unavailable, publish ports with Docker `-p` syntax:

```sh
dx --port 8080:8080 go run .
```

Use `--docker` or `docker: true` in config to mount the Docker socket. Remote `tcp://` and `ssh://` Docker hosts are not supported for socket mounting.

## Commands and flags reference

| Command | Purpose |
| --- | --- |
| `dx config [--explain]` | Print merged configuration |
| `dx trust [--yes] [path...]` | Approve `.dx.yaml` files |
| `dx doctor [--recheck]` | Check dx and Docker setup |
| `dx ps` | List warm containers |
| `dx stop [--all]` | Stop warm containers |
| `dx shims install [tool...]` | Install tool shims |
| `dx shims uninstall [tool...]` | Remove tool shims |
| `dx shims list` | List shims and their status |
| `dx which <tool>` | Show whether a shim uses dx or a local executable |
| `dx version` | Print version information |
| `dx help` | Print help |

| Flag | Purpose |
| --- | --- |
| `--image <ref>` | Override the selected image |
| `--mount <path>` | Override the project mount |
| `--port <spec>` | Publish a port; repeatable Docker `-p` syntax |
| `--docker` | Mount the Docker socket |
| `--cold` | Force a fresh container |

Flags must come before the command; `--flag=value` also works. Use `dx -- <cmd>` for a tool whose name clashes with a subcommand. Tool exit codes pass through unchanged; dx errors exit 125.

## Images

Images are published at `ghcr.io/joshualawson/dx-<name>`.

| Image | Contents |
| --- | --- |
| `dx-base` | git, curl, bash, OpenSSH client, make, Docker CLI and Compose, `dx` user, dx-idle |
| `dx-go` | Go, gopls, golangci-lint, Delve |
| `dx-node` | Node, npm, pnpm, TypeScript, ESLint, Prettier |
| `dx-python` | Python, uv, Ruff |
| `dx-rust` | Rust, Cargo, rustfmt, Clippy, rust-analyzer |
| `dx-infra` | Terraform, kubectl, Helm, AWS CLI, Azure CLI, gcloud |

Pin an image tag with `tools.<name>.version`; `tools.go.version: "1.25"` selects the Go 1.25 image. Point `tools.<name>.image` at any local or custom image.

Build images locally from the repository root:

```sh
docker build -f images/base/Dockerfile -t dx-local/dx-base:dev .
docker build -f images/go/Dockerfile --build-arg BASE=dx-local/dx-base:dev -t dx-local/dx-go:dev .
```

Then select it in config:

```yaml
tools:
  go:
    image: dx-local/dx-go:dev
```

## Troubleshooting

- Run `dx doctor` to inspect Docker, configuration, project markers, and warm containers.
- Run `dx doctor --recheck` after changing Docker host-networking support.
- Stop warm containers with `dx stop` after updating an image under the same tag.
- Use `dx which <tool>` to see whether a shim selects dx or a local executable.
- The `shims:` line in `dx doctor` reports the shim directory, `PATH` status, installed count, and stale count.
- On Linux, dx runs as your numeric uid and gid; its mounted identity files let git and SSH resolve that user.

## Development

See [AGENTS.md](AGENTS.md) for repository conventions and [DESIGN.md](DESIGN.md) for behavior decisions.

Without Go installed, run tests in Docker:

```sh
docker run --rm -v "$PWD:$PWD" -w "$PWD" golang:1.25 go test ./...
```

Releases are made by Goreleaser for `v*` tags. GitHub Actions builds and publishes images.
