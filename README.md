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

A repository `.dx.yaml` can select images, change env, share credentials, and turn on the Docker socket, so dx asks before using untrusted files. Global config can list trusted folders. Approvals record the path and file hash, so changed files need approval again.

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
- On Linux, dx runs as your numeric uid and gid; its mounted identity files let git and SSH resolve that user.

## Development

See [AGENTS.md](AGENTS.md) for repository conventions and [DESIGN.md](DESIGN.md) for behavior decisions.

Without Go installed, run tests in Docker:

```sh
docker run --rm -v "$PWD:$PWD" -w "$PWD" golang:1.25 go test ./...
```

Releases are made by Goreleaser for `v*` tags. GitHub Actions builds and publishes images.
