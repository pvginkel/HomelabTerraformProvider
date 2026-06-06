#!/usr/bin/env bash
# Build terraform-provider-homelab and drop the binary into the dev_overrides
# directory that modern-app-dev consumes. Mirrors the Jenkins build (same
# -ldflags); use this for local iteration when waiting on a Jenkins + image
# rebuild is too slow.
#
# The image's /etc/terraform.rc points a Terraform dev_override at
# PROVIDER_DIR, so a rebuilt binary takes effect immediately: no
# `terraform init`, no .terraform.lock.hcl refresh, no version bump.
#
# Override the destination via env var:
#   PROVIDER_DIR=/some/other/dir ./scripts/install-local.sh

set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-$(cat "${REPO_DIR}/version.txt")}"
PROVIDER_DIR="${PROVIDER_DIR:-${HOME}/.local/lib/terraform-providers}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

BIN="terraform-provider-homelab"
DEST_BIN="${PROVIDER_DIR}/${BIN}"

cd "$REPO_DIR"

echo "==> go build ${BIN} v${VERSION} (${GOOS}/${GOARCH})"
GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -o "${BIN}" -ldflags "-X main.version=${VERSION}"

echo "==> install -> ${DEST_BIN}"
install -d -m 0755 "$PROVIDER_DIR"
install -m 0755 "${REPO_DIR}/${BIN}" "${DEST_BIN}"

echo "==> done"
echo
echo "dev_override consumes this directly — no 'terraform init' or lock"
echo "refresh needed. Your Terraform CLI config must contain:"
echo
echo '    provider_installation {'
echo '      dev_overrides {'
echo "        \"pvginkel/homelab\" = \"${PROVIDER_DIR}\""
echo '      }'
echo '      direct {}'
echo '    }'
echo
echo "The modern-app-dev image already ships this at /etc/terraform.rc"
echo "(TF_CLI_CONFIG_FILE), so inside the container nothing else is needed."
