#!/usr/bin/env bash
set -euo pipefail

# Usage: bash precompile/scripts/build.sh /d/path/to/avalanchego
upstream_source="${1:?Provide the local AvalancheGo checkout path}"

if [[ ! -d "$upstream_source/.git" ]]; then
  echo "Expected an AvalancheGo Git checkout: $upstream_source" >&2
  exit 1
fi

upstream_source="$(cd "$upstream_source" && pwd)"

# Docker Desktop expects a Windows path when invoked from Git Bash.
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    upstream_source="$(cygpath -m "$upstream_source")"
    export MSYS_NO_PATHCONV=1
    ;;
esac

build_image="golang:1.26.10@sha256:cfa27959837144bf4649802ba38e59daffd0dd2a7994aa83daaf113285b0f551"

docker run --rm \
  --name dev2-native-build \
  --platform linux/amd64 \
  --cpus=4 \
  --memory=8g \
  -e GOTOOLCHAIN=local \
  -e GOMAXPROCS=4 \
  -e GOFLAGS=-p=2 \
  --mount "type=bind,source=$upstream_source,target=/upstream,readonly" \
  --mount type=volume,source=dev2-native-source,target=/work \
  --mount type=volume,source=dev2-native-gomod,target=/go/pkg/mod \
  --mount type=volume,source=dev2-native-gocache,target=/root/.cache/go-build \
  "$build_image" \
  bash -c '
    set -euo pipefail

    git config --global --add safe.directory /upstream

    if [ ! -d /work/avalanchego ]; then
      git -c core.autocrlf=false clone \
        --no-local /upstream /work/avalanchego
    fi

    cd /work/avalanchego

    expected_commit=4bb5c1dc13e1b6ff4eaf49fe8674666fbb342a17
    actual_commit="$(git rev-parse HEAD)"

    if [ "$actual_commit" != "$expected_commit" ]; then
      echo "Unexpected source commit: $actual_commit" >&2
      exit 1
    fi

    git diff --exit-code HEAD --

    if [ -n "$(git status --porcelain)" ]; then
      echo "Baseline build requires a clean source checkout." >&2
      exit 1
    fi

    go version
    git rev-parse HEAD

    mkdir -p build
    bash graft/subnet-evm/scripts/build.sh \
      /work/avalanchego/build/subnet-evm

    test -s build/subnet-evm
    sha256sum build/subnet-evm
  '