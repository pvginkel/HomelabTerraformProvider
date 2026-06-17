#!/usr/bin/env bash
# Append a built terraform-provider-homelab to a Provider Network Mirror
# tree (the TerraformRegistry repo's dist/) and prune old versions.
#
# Produces, under <dist>/registry.terraform.io/pvginkel/homelab/ :
#   terraform-provider-homelab_<version>_<os>_<arch>.zip   (the binary, zipped)
#   <version>.json                                         (archives + hashes)
#   index.json                                             (merged version list)
#
# Hashes: h1 is computed by `terraform providers lock` against a packed
# filesystem mirror of the freshly built zip (so it equals the ziphash a
# consumer recomputes from the downloaded archive); zh is the zip's sha256.
# This is the network-served twin of the unpacked filesystem mirror the
# provider used to bake into the iac image, but multi-version.
#
# Args / env:
#   BIN        path to the built provider binary           (required)
#   VERSION    <series>.<build>, e.g. 0.1.27               (required)
#   DIST       path to the registry checkout's dist/ root  (required)
#   KEEP       how many newest versions to retain          (default 10)
#   PLATFORMS  space-separated <os>_<arch> list            (default linux_amd64)
#
# Needs: terraform, python3.

set -euo pipefail

BIN="${BIN:?set BIN to the built provider binary}"
VERSION="${VERSION:?set VERSION (e.g. 0.1.27)}"
DIST="${DIST:?set DIST to the registry dist/ root}"
KEEP="${KEEP:-10}"
PLATFORMS="${PLATFORMS:-linux_amd64}"

HOST="registry.terraform.io"
NS="pvginkel"
NAME="homelab"
BINNAME="terraform-provider-${NAME}"

PDIR="${DIST}/${HOST}/${NS}/${NAME}"
mkdir -p "$PDIR"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# (platform, zip filename, h1, zh) rows for the version.json assembler.
rows="${tmp}/rows.tsv"
: > "$rows"

for plat in $PLATFORMS; do
    inner="${BINNAME}_v${VERSION}"
    zipname="${BINNAME}_${VERSION}_${plat}.zip"
    zippath="${tmp}/${zipname}"

    # Zip the binary under the canonical inner name so the ziphash (h1)
    # matches the unpacked-mirror dirhash terraform expects.
    python3 - "$BIN" "$zippath" "$inner" <<'PY'
import sys, zipfile
src, dst, arcname = sys.argv[1:4]
zi = zipfile.ZipInfo(arcname)
zi.external_attr = (0o755 & 0xFFFF) << 16
zi.compress_type = zipfile.ZIP_DEFLATED
with zipfile.ZipFile(dst, "w") as zf, open(src, "rb") as f:
    zf.writestr(zi, f.read())
PY

    # h1 via terraform, off a throwaway packed filesystem mirror.
    fs="${tmp}/fs-${plat}"
    mkdir -p "${fs}/${HOST}/${NS}/${NAME}"
    cp "$zippath" "${fs}/${HOST}/${NS}/${NAME}/"
    cfg="${tmp}/cfg-${plat}"
    mkdir -p "$cfg"
    cat > "${cfg}/main.tf" <<EOF
terraform {
  required_providers {
    ${NAME} = { source = "${NS}/${NAME}", version = "${VERSION}" }
  }
}
EOF
    ( cd "$cfg" && terraform providers lock \
        -fs-mirror="$fs" -platform="$plat" \
        "${HOST}/${NS}/${NAME}" >/dev/null )
    h1="$(grep -oE 'h1:[A-Za-z0-9+/=]+' "${cfg}/.terraform.lock.hcl" | head -1)"
    [ -n "$h1" ] || { echo "error: no h1 hash produced for ${plat}" >&2; exit 1; }
    zh="zh:$(sha256sum "$zippath" | cut -d' ' -f1)"

    cp "$zippath" "${PDIR}/${zipname}"
    printf '%s\t%s\t%s\t%s\n' "$plat" "$zipname" "$h1" "$zh" >> "$rows"
    echo "==> staged ${zipname} (${h1}, ${zh})"
done

# Assemble <version>.json, merge index.json, and prune to KEEP newest.
VERSION="$VERSION" KEEP="$KEEP" PDIR="$PDIR" ROWS="$rows" python3 - <<'PY'
import json, os, re

version = os.environ["VERSION"]
keep = int(os.environ["KEEP"])
pdir = os.environ["PDIR"]
rows = os.environ["ROWS"]

archives = {}
with open(rows) as f:
    for line in f:
        plat, zipname, h1, zh = line.rstrip("\n").split("\t")
        archives[plat] = {"url": zipname, "hashes": [h1, zh]}

with open(os.path.join(pdir, f"{version}.json"), "w") as f:
    json.dump({"archives": archives}, f, indent=2, sort_keys=True)
    f.write("\n")

index_path = os.path.join(pdir, "index.json")
try:
    with open(index_path) as f:
        index = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    index = {}
versions = index.get("versions") or {}
versions[version] = {}

def key(v):
    return [int(p) if p.isdigit() else p for p in v.split(".")]

ordered = sorted(versions, key=key)
pruned = ordered[:-keep] if keep > 0 and len(ordered) > keep else []
for v in pruned:
    del versions[v]
    for fn in (f"{v}.json",):
        try:
            os.remove(os.path.join(pdir, fn))
        except FileNotFoundError:
            pass
    for fn in os.listdir(pdir):
        if re.match(rf"terraform-provider-homelab_{re.escape(v)}_.+\.zip$", fn):
            os.remove(os.path.join(pdir, fn))
    print(f"==> pruned {v}")

with open(index_path, "w") as f:
    json.dump({"versions": versions}, f, indent=2, sort_keys=True)
    f.write("\n")

print(f"==> index now carries: {', '.join(sorted(versions, key=key))}")
PY
