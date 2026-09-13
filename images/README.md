# Toolchain images

Build contexts are always the repository root. Every image supports `linux/amd64` and `linux/arm64`, has no entrypoint or selected user, and defaults to `bash`. The base provides the optional `dx` user (1000:1000); tools also run with numeric users absent from `/etc/passwd` and with a writable HOME mounted by dx.

## Local build and smoke tests

```sh
docker build --platform linux/amd64 -f images/base/Dockerfile -t dx-local/dx-base:dev .
images/smoke-test.sh base
for name in go node python rust infra; do
  docker build --platform linux/amd64 -f "images/$name/Dockerfile" \
    --build-arg BASE=dx-local/dx-base:dev -t "dx-local/dx-$name:dev" .
  images/smoke-test.sh "$name"
done
docker image ls --filter 'reference=dx-local/dx-*:dev'
```

Smoke tests require Docker and network access. `images/smoke-test.sh` is reusable from a clean checkout after the corresponding `dx-local/dx-<name>:dev` image has been built. It uses uid:gid 1234:1234, `HOME=/home/test`, a writable HOME tmpfs, and fresh named volumes `dx-cache-<toolchain>-test`. It refuses to reuse an existing test volume and removes only volumes it created. Tests check versions, inherited image conventions, cache permissions, and real builds/package installs for language toolchains; infra checks Terraform, kubectl, Helm, AWS CLI, gcloud, and Azure CLI versions.

## Versions

Version arguments live in each Dockerfile. Image tags identify the primary toolchain series; the commit SHA identifies the complete image recipe.

| Image | Primary version / tag | Other pins |
| --- | --- | --- |
| base | Debian bookworm / `bookworm` | Docker 28.4.0, Go builder 1.25 |
| go | Go 1.25.14 / `1.25` | gopls 0.20.0, golangci-lint 2.4.0, delve 1.25.0 |
| node | Node 22.19.0 / `22` | Corepack 0.34.0, pnpm 10.17.0, TypeScript 5.9.2, tsx 4.20.5, ESLint 9.35.0, Prettier 3.6.2 |
| python | Python 3.13 / `3.13` | uv 0.8.17, Ruff 0.12.12 |
| rust | Rust stable / `stable` | rustup 1.28.2 |
| infra | Terraform 1.13.2 / `1.13.2` | kubectl 1.34.1, Helm 3.19.0, AWS CLI 2.30.0, gcloud 539.0.0, Azure CLI 2.77.0 |

When changing `GO_VERSION`, update both `GO_SHA256_AMD64` and `GO_SHA256_ARM64` using the official Go download metadata. Node downloads are checked against the release's official SHA-256 manifest. Terraform, kubectl, Helm, rustup, and golangci-lint downloads are also checksum-verified.

`RUST_VERSION=stable` and `PYTHON_VERSION=3.13` intentionally retain the requested defaults; pass exact versions for a fixed toolchain release. Debian packages and upstream image tags are not digest-locked, so these recipes do not promise bit-for-bit reproducibility.

## Caches and credentials

Each `/cache/<toolchain>` has mode 1777, which Docker copies into a newly created cache volume. Installed executables remain outside those volumes, except user-installed tools. Node's pinned pnpm is seeded into the Corepack cache and also retained under `/usr/local/share/corepack`. Rust's registry/git directories point into its cache volume, and `CARGO_INSTALL_ROOT` keeps user-installed crates there. Cargo's home permits creation of its additional cache lock/metadata files without making the installed toolchain writable.

Images do not override HOME or XDG locations, so credentials and config directories mounted under HOME remain visible to git, gh, cloud CLIs, and language tools. Terraform's plugin cache and Helm's download cache are kept under `/cache/infra`; cloud credential and config files stay under HOME for dx to share with the host.

## CI

`images.yml` builds and exports the base as a multi-platform OCI artifact. Each toolchain matrix job consumes that exact artifact through a BuildKit named context, including on fork PRs; it never falls back to a previously published `latest` base. Only main and `v*` tag builds publish images. PRs build both architectures without registry writes. GHA caches are isolated by image name.
