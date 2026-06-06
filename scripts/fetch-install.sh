#!/usr/bin/env bash
# Fetch the CI-built terraform-provider-homelab binary from Jenkins and install
# it into the filesystem-mirror layout that the iac / modern-app-dev images
# consume. Use this to drop the *released* provider onto a box without building
# from source — the source-build counterpart is scripts/install-local.sh.
#
# Env vars:
#   JENKINS_URL    base url, e.g. https://jenkins.home              (required)
#   JOB_PATH       job path under Jenkins
#                  (default: job/IaC/job/HomelabTerraformProvider)
#   BUILD          build selector            (default: lastSuccessfulBuild)
#   JENKINS_USER / JENKINS_TOKEN  basic-auth creds                  (optional)
#   PLUGIN_ROOT    mirror root   (default: /usr/local/share/terraform/plugins)
#
# The install steps go through sudo when PLUGIN_ROOT isn't writable, skipped
# when already root or when PLUGIN_ROOT is user-writable.

set -euo pipefail

: "${JENKINS_URL:?set JENKINS_URL (e.g. https://jenkins.home)}"
JOB_PATH="${JOB_PATH:-job/IaC/job/HomelabTerraformProvider}"
BUILD="${BUILD:-lastSuccessfulBuild}"
PLUGIN_ROOT="${PLUGIN_ROOT:-/usr/local/share/terraform/plugins}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

NAMESPACE="pvginkel"
NAME="homelab"
BIN="terraform-provider-${NAME}"

ART="${JENKINS_URL%/}/${JOB_PATH}/${BUILD}/artifact"

curl_opts=(-fsSL)
if [[ -n "${JENKINS_USER:-}" && -n "${JENKINS_TOKEN:-}" ]]; then
    curl_opts+=(-u "${JENKINS_USER}:${JENKINS_TOKEN}")
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "==> fetching metadata"
curl "${curl_opts[@]}" -o "${tmp}/metadata.json" "${ART}/${BIN}-metadata.json"
VERSION="$(jq -r .version "${tmp}/metadata.json")"
[[ -n "$VERSION" && "$VERSION" != "null" ]] || {
    echo "error: could not read version from metadata" >&2
    exit 1
}

echo "==> fetching ${BIN} v${VERSION}"
curl "${curl_opts[@]}" -o "${tmp}/${BIN}" "${ART}/${BIN}"

DEST_DIR="${PLUGIN_ROOT}/registry.terraform.io/${NAMESPACE}/${NAME}/${VERSION}/${GOOS}_${GOARCH}"
DEST_BIN="${DEST_DIR}/${BIN}_v${VERSION}"

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
"${SUDO[@]}" install -m 0755 "${tmp}/${BIN}" "${DEST_BIN}"

echo "==> done (v${VERSION})"
echo
echo "If a consumer's .terraform.lock.hcl pins a different version, run"
echo "'terraform init -upgrade' there. CI normally keeps the Ansible lock in"
echo "sync, so this is only needed for ad-hoc consumers."
