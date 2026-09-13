# dx

Personal CLI that runs development tools inside Docker containers as if they were installed locally. `DESIGN.md` is the source of truth for behaviour; update it when a decision changes.

## Building and testing

- Go 1.25, module `github.com/joshualawson/dx`.
- `go test ./...`, `go vet ./...`, `gofmt -l .`
- Without Go installed, run it from the golang image with the repo mounted at the same path: `docker run --rm -v "$PWD:$PWD" -w "$PWD" golang:1.25 go test ./...`

## Code conventions

- Standard library only, except `gopkg.in/yaml.v3` for config. Ask before adding dependencies.
- Must build and behave correctly on linux, darwin and windows.
- Pass OS-dependent inputs (GOOS, home folder, env lookup, file existence) as parameters so every OS's behaviour is unit-testable on any host; read `runtime.GOOS`, `os.Getenv` etc. only at the edges.
- Use `path/filepath` for host paths and `path` for container paths, which always use forward slashes.
- Errors are lowercase, have no trailing punctuation, wrap with `%w`, and name the file or command involved.
- Tests are table-driven, use `t.TempDir()` for filesystem work, and skip anything needing network or Docker when it's unavailable.

## Layout

| Path | Purpose |
| --- | --- |
| `cmd/dx` | CLI entrypoint and wiring |
| `cmd/dx-idle` | Idle watcher that runs as PID 1 in warm containers |
| `internal/config` | Global + upward `.dx.yaml` loading and merging |
| `internal/trust` | Approval of `.dx.yaml` files |
| `internal/project` | Project root detection |
| `internal/route` | Command → image resolution |
| `internal/hostenv` | Credential mounts, env filtering, host → container paths |
| `internal/docker` | Building and running `docker` commands |
| `images/<name>` | Toolchain Dockerfiles |
