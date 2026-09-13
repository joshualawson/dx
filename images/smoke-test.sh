#!/bin/sh
set -eu

name=${1:?usage: images/smoke-test.sh base|go|node|python|rust|infra}
image="dx-local/dx-${name}:dev"
case "$name" in base|go|node|python|rust|infra) ;; *) exit 2 ;; esac

config=$(docker image inspect --format '{{.Config.User}}|{{json .Config.Entrypoint}}|{{json .Config.Cmd}}' "$image")
[ "$config" = '|null|["bash"]' ]
if [ "$name" = base ]; then
    docker run --rm --user 1234:1234 -e HOME=/home/test --tmpfs /home/test:uid=1234,gid=1234 "$image" bash -euc '
        test -w "$HOME"; touch "$HOME/dx-smoke"
        git --version; curl --version; docker --version; docker compose version
        test -x /usr/local/bin/dx-idle
        test "$(id -u dx)" = 1000
        test "$(id -g dx)" = 1000
    '
    exit 0
fi

volume="dx-cache-${name}-test"
if docker volume inspect "$volume" >/dev/null 2>&1; then
    printf 'refusing to reuse or remove existing volume %s\n' "$volume" >&2
    exit 1
fi
docker volume create "$volume" >/dev/null
trap 'docker volume rm "$volume" >/dev/null' EXIT
trap 'exit 1' HUP INT TERM

docker run --rm -i --user 1234:1234 -e HOME=/home/test --tmpfs /home/test:uid=1234,gid=1234 \
    -v "$volume:/cache/$name" "$image" bash -seu -- "$name" <<'SMOKE'
name=$1
test "$(stat -c %a "/cache/$name")" = 1777
test -w "$HOME"
touch "$HOME/dx-smoke"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
case "$name" in
    go)
        go version
        gopls version
        golangci-lint version
        dlv version
        go mod init example.com/dx-smoke
        printf 'package main\nimport "fmt"\nfunc main() { fmt.Println("dx") }\n' > main.go
        go build -o hello .
        test "$(./hello)" = dx
        golangci-lint run
        gopls check main.go
        go install .
        test "$(dx-smoke)" = dx
        ;;
    node)
        node --version
        npm --version
        corepack --version
        pnpm --version
        test "$(pnpm config get store-dir)" = /cache/node/pnpm/store
        tsc --version
        tsx --version
        eslint --version
        prettier --version
        npm init -y
        npm install --save-exact is-number@7.0.0
        node -e 'if (!require("is-number")(42)) process.exit(1)'
        pnpm install --ignore-scripts
        ;;
    python)
        python --version
        python3 --version
        uv --version
        ruff --version
        uv venv --python 3.13 .venv
        uv pip install --python .venv/bin/python six==1.17.0
        .venv/bin/python -c 'import six; assert six.text_type(42) == "42"'
        ;;
    rust)
        rustup --version
        rustc --version
        cargo --version
        rustfmt --version
        cargo clippy --version
        rust-analyzer --version
        cargo init --name dx-smoke
        printf '\nitoa = "=1.0.15"\n' >> Cargo.toml
        printf 'fn main() { println!("{}", itoa::Buffer::new().format(42)); }\n' > src/main.rs
        cargo build
        test "$(./target/debug/dx-smoke)" = 42
        cargo install --path . --locked
        test "$(dx-smoke)" = 42
        ;;
    infra)
        terraform version
        kubectl version --client
        helm version
        aws --version
        gcloud version
        az version
        ;;
esac
SMOKE
