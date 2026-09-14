# dx design

dx runs development tools inside Docker containers as if they were installed locally. A new machine needs only Docker and the `dx` binary.

```
dx go build main.go
dx npm install
dx cargo test
dx terraform plan
```

Status of each section: **Decided** means agreed, **Proposed** means a suggested default that is still open to change.

## Goals

- Works on macOS, Linux and Windows.
- Commands behave as if run locally: same working directory, files land where expected, exit codes and output pass straight through.
- Small per-toolchain images instead of one bloated image.
- As little config as possible; detect what can be detected and use flags for the rare cases.

## CLI (Decided)

- Written in Go, released as a single binary per OS and architecture.
- Runs containers by calling the `docker` CLI, not the Docker SDK, so it works with Docker Desktop, OrbStack, Colima and Podman.
- Usage is `dx [dx flags] <cmd> [args...]`. Everything from `<cmd>` onwards is passed to the tool untouched.

### Flags

Flags must precede the command; `--flag=value` is accepted.

| Flag | Purpose |
| --- | --- |
| `--image <ref>` | Use this image instead of the one dx would pick |
| `--mount <path>` | Mount this folder instead of the detected project root |
| `--port <spec>` | Publish a port; repeatable, with Docker `-p` syntax |
| `--docker` | Mount the Docker socket |
| `--cold` | Force a fresh container |

### Built-in subcommands (Decided)

| Command | Purpose |
| --- | --- |
| `dx config [--explain]` | Print the merged config, optionally with the file each value came from |
| `dx trust [--yes] [path...]` | Approve `.dx.yaml` files |
| `dx doctor [--recheck]` | Check Docker capabilities; optionally clear the network cache |
| `dx version` | Print version information |
| `dx help` | Print help |
| `dx ps` | List warm containers |
| `dx stop [--all]` | Stop warm containers for this project, or all |
| `dx shims install [tool...]` | Install tool shims |
| `dx shims uninstall [tool...]` | Remove tool shims |
| `dx shims list` | List shims and their status |
| `dx which <tool>` | Show whether a shim uses dx or a local executable |

Subcommand names are special only as the first argument with no preceding dx flags. A tool whose name clashes runs with `dx -- <cmd>`.

- dx errors exit 125; tool exit codes pass through unchanged.

## Choosing an image (Decided)

First match wins:

1. `--image` flag
2. `tools.<cmd>.image` in merged config (`.dx.yaml` files, then global config)
3. Built-in command map, e.g. `go`, `gofmt`, `gopls` → `dx-go`
4. Project markers, for general commands like `make` or `bash`: `go.mod` → `dx-go`, `package.json` → `dx-node`, `Cargo.toml` → `dx-rust`, `pyproject.toml` → `dx-python`
5. `dx-base`

`tools.<cmd>.version` picks the image tag.

## Mounting (Decided)

- dx searches upward from the current folder for the first of `.git`, `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml` and mounts that folder.
- The search never goes above the home folder. If nothing is found, the current folder is mounted.
- `--mount <path>` overrides detection. There is no mount setting in config.
- On macOS and Linux the folder is mounted at the same absolute path inside the container, so paths in tool output match the host.
- On Windows, `C:\src\app` is mounted at `/c/src/app`.
- The container's working directory is the current folder's location inside the mount.

## Config (Decided)

### Files

- Global: `~/.config/dx/config.yaml` (`%APPDATA%\dx\config.yaml` on Windows).
- Project: `.dx.yaml` files found by searching upward from the current folder, stopping at the home folder or drive root.
- `root: true` in a `.dx.yaml` stops the search at that file.

### Priority, lowest first

1. Built-in defaults
2. Global config
3. `.dx.yaml` files, farthest from the current folder first
4. Flags

`DX_CONFIG` points dx at a different global config file.

### Merging

- Maps such as `tools` and `env` merge key by key.
- Single values are replaced by the nearer file.
- Lists are replaced; `key+:` appends to the inherited list instead.
- `local` is allowed in global and project config; `local+:` appends tool names that shims run locally.

### Example

```yaml
# ~/Development/joshualawson/.dx.yaml
tools:
  go:   { image: ghcr.io/joshualawson/dx-go, version: "1.25" }
  node: { version: "22" }
env:
  GOPRIVATE: github.com/joshualawson/*
```

```yaml
# ~/Development/joshualawson/project/repo/.dx.yaml
tools:
  go: { version: "1.23" }
```

## Trust (Decided)

A `.dx.yaml` from a repo you didn't write could mount secrets, forward credentials or swap in a malicious image, so dx asks before using one.

- The global config lists trusted folders, e.g. `trusted: [~/Development/joshualawson]`. `.dx.yaml` files under them are used without asking.
- Any other `.dx.yaml` is shown to the user the first time it is found, and used only after `dx trust`.
- A terminal prompt shows each untrusted file with `trust.Describe` output and asks `y/N`.
- Without a terminal, dx tells the user to run `dx trust <path>`.
- Approvals are stored as path + SHA-256 of the file. If the file changes, dx asks again.
- Only `tools.<name>.version`, `root: false`, and `local` need no approval. `root: true` needs approval because it discards parent settings.
- The global config is always trusted.

## Running containers

### Container lifecycle (Decided)

- **Warm:** only dx images containing `dx-idle` are warm; custom images always run cold. `--port` forces cold.
- **Name:** `dx-warm-<hash>` is based on settings fixed at creation: image, user, groups, mounts, network and labels. Environment, workdir and command are set by `docker exec`.
- Image updates under the same tag apply after the warm container idles out or is stopped.
- Two dx processes racing to create a container: the loser waits for the winner's container.
- **Signals:** dx records the command PID and uses a second `docker exec … kill` to deliver SIGINT, SIGTERM and SIGHUP.
- **Idle shutdown:** `dx-idle` exits after 30 minutes with no commands (configurable); `/proc` scan failures are busy. Containers use `--rm`.
- **Backup cleanup:** creating a warm container removes stale warm containers left behind by crashes or sleep.
- **Cold:** fresh `docker run --rm` per call.
- **Commands:** `dx ps` lists warm containers; `dx stop [--all]` stops them.

- **Defaults:** warm for Go, Node, Python and Rust; cold for infra, base and custom images. `tools.<name>.warm` overrides by command, then toolchain. `--cold` and `--port` force a fresh container.

### Behaviour (Decided)

- **TTY:** dx requests `-t` only when stdin and stdout are real terminals, so redirects to `/dev/null` and pipes work.
- **File ownership:** Linux commands run as the host uid:gid.
- **State:** `$XDG_STATE_HOME/dx` (default `~/.local/state/dx`), or `%LOCALAPPDATA%\dx` on Windows; holds `trust.json`, `network.json`, and `identity/passwd` and `identity/group`. Shims are in `$XDG_DATA_HOME/dx/bin` (default `~/.local/share/dx/bin`), or `%LOCALAPPDATA%\dx\bin` on Windows.
- **Home:** named volume `dx-home` is mounted at the host home path inside the container; credential mounts sit within it.
- On Linux, newly created home volumes are chowned to the host user and credential parent folders (`.config`, `.docker`, `.cargo`) are pre-created.
- **Linux identity:** minimal read-only `/etc/passwd` and `/etc/group` contain root, nobody, the user, and the Docker group when needed, so ssh and git support unknown image uids.
- **Caches:** `dx-cache-<toolchain>` volumes at `/cache/<toolchain>` mount only when the image's final path component starts `dx-`; custom images would create them without dx image permissions.
- **Go target:** `GOOS` and `GOARCH` default to the host only for `go build`; `go test`, `go run` and other Go commands run for Linux. Host environment and config override them.
- **Environment:** values use Docker's environment with `-e KEY`, keeping tokens out of `ps`. `HOME`, `PATH` and `DOCKER_*` are explicit.
- Host install-location variables, including toolchain, version-manager, Homebrew, compiler search-path and XDG variables, are dropped.
- Environment order, later wins: filtered host environment, SSH agent, HOME/USER/LOGNAME, Go target, config `env`.

## Images (Decided: toolchains; Proposed: details)

v1 images:

| Image | Contents |
| --- | --- |
| `dx-base` | git, curl, ca-certificates, bash, openssh-client, make, Docker CLI and compose plugin, `dx` user (1000:1000), dx-idle |
| `dx-go` | go, gofmt, gopls, golangci-lint, delve |
| `dx-node` | node, npm, pnpm, TypeScript, eslint, prettier |
| `dx-python` | python, uv, ruff |
| `dx-rust` | rustup, cargo, rustfmt, clippy, rust-analyzer |
| `dx-infra` | terraform, kubectl, helm, aws, az, gcloud |

- Toolchain images build on `dx-base` so shared layers are stored once.
- Built for linux/amd64 and linux/arm64 by GitHub Actions and pushed to `ghcr.io/joshualawson/dx-*`.
- Tags follow the toolchain version, e.g. `dx-go:1.23`.
- Config can point any tool at any image.

## Repository layout (Decided)

```
cmd/dx/             CLI entrypoint
cmd/dx-idle/        idle watcher
internal/cli/       command line and wiring
internal/config/    loading and merging
internal/trust/     approvals
internal/project/   root detection
internal/route/     command → image resolution
internal/shim/      shim install, detection and local lookup
internal/hostenv/   credential mounts, env filtering, container paths, identity files
internal/docker/    docker commands, warm containers
images/<name>/      Dockerfiles
.github/workflows/  CLI releases and image builds
```

## Networking (Decided)

- Containers use host networking wherever it works: Linux, OrbStack, and Docker Desktop with host networking enabled.
- A server on :8080 inside dx is reachable at `localhost:8080`, and tools inside dx can reach services running on the host.
- Once per Docker context, dx probes host networking with BusyBox using `--network host` against a local HTTP listener; the result is cached in `network.json`.
- Docker failures (exit 125, such as an offline image pull) are not cached.
- `dx doctor --recheck` clears the cache.
- `--port` disables host networking and runs cold.

## Credentials (Decided)

Everything is shared by default so tools behave exactly as they would locally.

- `HOME` inside the container is the host home path (`/c/Users/<name>` on Windows) backed by the `dx-home` volume.
- These are mounted writable at the same paths, so logins done inside dx persist on the host:
  - `~/.ssh` and the SSH agent socket
  - `~/.gitconfig`, `~/.config/git`
  - `~/.netrc`
  - `~/.aws`, `~/.config/gcloud`, `~/.azure`, `~/.kube`
  - `~/.config/gh`, `~/.docker/config.json`
  - `~/.npmrc`, `~/.pypirc`, `~/.cargo/credentials.toml`, `~/.terraform.d`
- Paths that don't exist on the host are skipped.
- Host environment variables pass through after filtering host install-location variables; `HOME`, `PATH` and `DOCKER_*` use explicit `-e KEY=VALUE` when present.
- Sharing can be turned off globally or per credential in global config. A project `.dx.yaml` that changes credential settings needs trust approval.

Known gap: credentials stored in the macOS keychain or Windows Credential Manager (git credential helpers, `gh` keyring tokens, Docker Desktop's `credsStore`) can't be read from a Linux container. See Later.

## Docker socket (Decided)

- Off by default.
- On with `--docker`, `docker: true` in `.dx.yaml` (needs trust approval), or `docker: true` in global config.
- Linux mounts the socket from `DOCKER_HOST` or the active context and adds its group.
- Docker Desktop on Linux, macOS and Windows mounts `/var/run/docker.sock` without an extra group.
- `tcp://` and `ssh://` Docker hosts are errors.
- `dx-base` includes the `docker` CLI and compose plugin.

## Shims (Decided)

- A shim is a link named for a tool that points to dx. Invoked under that name, dx passes all arguments to the tool and reads none as dx flags.
- macOS and Linux use symlinks in `$XDG_DATA_HOME/dx/bin` (default `~/.local/share/dx/bin`); Windows uses `<tool>.exe` hard links in `%LOCALAPPDATA%\dx\bin`, or copies when linking fails.
- `dx shims install [tool...]` installs named shims or every built-in command. The directory must precede local installs on `PATH`; dx prints setup instructions when it is absent.
- `dx shims uninstall [tool...]` removes named shims or all shims, and only removes dx shims.
- `dx shims list` shows TOOL, RUNS and PATH; stale Windows copies and symlinks to old dx locations are marked. Re-run install to refresh them.
- `dx which <tool>` shows the selected dx image, reason and warm/cold state, or the local executable and reason; it also shows shim and `PATH` status. A missing selected local tool exits 125.
- `DX_LOCAL=1` or `local: [go, npm]` makes shims run local tools. Lookup skips the shims folder and other dx links; missing tools fail with no fallback.
- Direct `dx <tool>` calls always use containers. `local` and `DX_LOCAL` affect only shims.
- `dx doctor` reports the shims directory, `PATH` status, installed count and stale count.
- On macOS and Linux, language servers work through shims because container paths match host paths. Windows shims suit command lines, not language servers, because `/c/...` paths are not editor-mapped.
- The first call in a project starts a warm container; later calls reuse it.

## Later

- **Credential bridge:** a helper inside the container that asks dx on the host for keychain-stored credentials (e.g. `git credential fill`, `gh auth token`).
- Windows host environment values containing Windows paths, e.g. `KUBECONFIG=C:\...`, are not translated to container paths yet.
