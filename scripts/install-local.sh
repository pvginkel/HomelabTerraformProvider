#!/usr/bin/env bash
# Build terraform-provider-homelab and drop the binary into the
# filesystem-mirror layout that modern-app-dev consumes. Mirrors the
# Jenkins pipeline (same -ldflags, same install path); use this for
# local iteration when waiting on a Jenkins + image rebuild is too slow.
#
# Defaults match the baked image. Override via env vars:
#   VERSION=0.1.1 ./scripts/install-local.sh
#   PLUGIN_ROOT=/some/other/dir ./scripts/install-local.sh
#
# `go build` runs as the invoking user; the install steps go through
# sudo because the default PLUGIN_ROOT is root-owned. Sudo is skipped
# when already root or when PLUGIN_ROOT is user-writable.

set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-$(cat "${REPO_DIR}/version.txt")}"
PLUGIN_ROOT="${PLUGIN_ROOT:-/usr/local/share/terraform/plugins}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

NAMESPACE="pvginkel"
NAME="homelab"
BIN="terraform-provider-${NAME}"
DEST_DIR="${PLUGIN_ROOT}/registry.terraform.io/${NAMESPACE}/${NAME}/${VERSION}/${GOOS}_${GOARCH}"
DEST_BIN="${DEST_DIR}/${BIN}_v${VERSION}"

cd "$REPO_DIR"

echo "==> go build ${BIN} v${VERSION} (${GOOS}/${GOARCH})"
GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -o "${BIN}" -ldflags "-X main.version=${VERSION}"

SUDO=()
if [[ $EUID -ne 0 ]] && [[ ! -w "$PLUGIN_ROOT" ]]; then
    if ! command -v sudo >/dev/null 2>&1; then
        echo "error: ${PLUGIN_ROOT} is not writable and sudo is unavailable." >&2
        echo "       Install sudo, run as root, or set PLUGIN_ROOT=/path/you/own." >&2
        exit 1
    fi
    SUDO=(sudo)
fi

echo "==> install -> ${DEST_BIN}"
"${SUDO[@]}" install -d -m 0755 "$DEST_DIR"
"${SUDO[@]}" install -m 0755 "${REPO_DIR}/${BIN}" "${DEST_BIN}"

echo "==> done"
echo
echo "If a consumer's .terraform.lock.hcl already pins pvginkel/homelab"
echo "v${VERSION}, the recorded hash will not match this rebuilt binary."
echo "Run 'terraform init -upgrade' in that project (or bump VERSION) to"
echo "refresh the lock."
