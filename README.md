# terraform-provider-homelab

A custom Terraform provider that bundles homelab-specific resource types
behind one provider. Built with
[terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework).

| Thing            | Value                                              |
|------------------|----------------------------------------------------|
| Provider source  | `pvginkel/homelab`                                 |
| Binary           | `terraform-provider-homelab`                       |
| Go module        | `github.com/pvginkel/HomelabTerraformProvider`     |
| Resource prefix  | `homelab_`                                         |

The provider is consumed via a baked-in **filesystem mirror**, not a registry.
The Jenkins build stamps each build as `0.1.<build-number>`, archives the
binary, and the `iac` / `modern-app-dev` images install it into the mirror at
`/usr/local/share/terraform/plugins/...`. `TF_CLI_CONFIG_FILE=/etc/terraform.rc`
in those images points Terraform there, so `terraform init` resolves
`pvginkel/homelab` with no per-machine setup. Because every build is a new
version, the CI job also rewrites the consumer's `.terraform.lock.hcl` (in the
Ansible repo) to match — no manual `terraform init -upgrade`.

## Install

Two scripts populate the mirror layout on a box:

```sh
# build from local source (provider development) — installs version.txt's version
./scripts/install-local.sh

# fetch the CI-built (released) binary from Jenkins — installs 0.1.<build>
JENKINS_URL=https://jenkins.home ./scripts/fetch-install.sh
```

Both default to `PLUGIN_ROOT=/usr/local/share/terraform/plugins`. A local
source build is a one-off version, so it won't match the CI-maintained lock —
run `terraform init -upgrade` in the consumer dir when iterating locally.

The mirror config Terraform reads (already baked into the images at
`/etc/terraform.rc`):

```hcl
provider_installation {
  filesystem_mirror {
    path    = "/usr/local/share/terraform/plugins"
    include = ["registry.terraform.io/pvginkel/*"]
  }
  direct {
    exclude = ["registry.terraform.io/pvginkel/*"]
  }
}
```

Consuming modules then declare:

```hcl
terraform {
  required_providers {
    homelab = {
      source = "pvginkel/homelab"
    }
  }
}

provider "homelab" {}
```

## Provider configuration

The provider talks to several independent backends, each configured by a
group of attributes with environment-variable fallbacks. A whole group may
be left unset — only the resources that use it will fail. Setting a proper
subset of a group (e.g. a URL without its token, or three of the four Ceph
attributes) is rejected as a misconfiguration.

| Attribute               | Env var                          | Used by                                          |
|-------------------------|----------------------------------|--------------------------------------------------|
| `dns_reservation_url`   | `HOMELAB_DNS_RESERVATION_URL`    | `homelab_dns_reservation`                        |
| `dns_reservation_token` | `HOMELAB_DNS_RESERVATION_TOKEN`  | `homelab_dns_reservation`                        |
| `backup_server_url`     | `HOMELAB_BACKUP_SERVER_URL`      | `homelab_backup_credential`                      |
| `backup_server_token`   | `HOMELAB_BACKUP_SERVER_TOKEN`    | `homelab_backup_credential`                      |
| `ceph_mon_host`         | `HOMELAB_CEPH_MON_HOST`          | `homelab_rbd_image`, `homelab_cephfs_subvolume`  |
| `ceph_user`             | `HOMELAB_CEPH_USER`              | `homelab_rbd_image`, `homelab_cephfs_subvolume`  |
| `ceph_key`              | `HOMELAB_CEPH_KEY`               | `homelab_rbd_image`, `homelab_cephfs_subvolume`  |
| `ceph_pool`             | `HOMELAB_CEPH_POOL`              | `homelab_rbd_image`, `homelab_cephfs_subvolume`  |
| `s3_endpoint`           | `HOMELAB_S3_ENDPOINT`            | `homelab_s3_storage`                             |
| `s3_admin_access_key`   | `HOMELAB_S3_ADMIN_ACCESS_KEY`    | `homelab_s3_storage`                             |
| `s3_admin_secret_key`   | `HOMELAB_S3_ADMIN_SECRET_KEY`    | `homelab_s3_storage`                             |

All token / key / secret attributes are marked sensitive. The typical
pattern is to leave the provider block empty and supply every value via
environment.

```hcl
provider "homelab" {}
```

Configuration notes for the Ceph-backed resources:

- `ceph_user` is the cephx user **without** the `client.` prefix; `ceph_key`
  is the inline base64 cephx key.
- `ceph_pool` serves as **both** the RBD pool and the CephFS subvolume group
  (they are always equal per environment, e.g. `csi-dev` / `csi-prd`). The
  CephFS filesystem name is the hardcoded cluster constant `cephfs`, and the
  subvolume group is assumed to already exist.
- The `homelab_rbd_image` / `homelab_cephfs_subvolume` resources link against
  **`librados2`** and **`librbd1`** at runtime (the provider is cgo). Every
  host that runs `terraform apply` with these resources must have those
  packages installed. (The build host additionally needs the `-dev` headers;
  see [Development](#development).)
- `homelab_s3_storage` authenticates to the RGW Admin Ops API as an admin user
  that must exist with caps `users=*;buckets=*`:

  ```sh
  radosgw-admin user create --uid=tf-provider --display-name="TF provider admin" \
    --caps="users=*;buckets=*"
  ```

  Supply its `access_key` / `secret_key` as `s3_admin_access_key` /
  `s3_admin_secret_key`.

## Resources

### `homelab_dns_reservation`

A DHCP + DNS reservation managed by the homelab dnsmasq sidecar. The
sidecar picks the IPv4 address and binds it to the hostname for the
reservation's lifetime; subsequent updates that change the MAC keep the
same IPv4.

```hcl
resource "homelab_dns_reservation" "k8s_leader" {
  hostname = "srvk8sl1"
  mac      = "02:A7:F3:03:84:00"
}
```

| Attribute  | Type   | Required | Description                                                                                          |
|------------|--------|----------|------------------------------------------------------------------------------------------------------|
| `hostname` | string | yes      | Reservation hostname (lowercase, no FQDN). **Changing forces destroy + recreate.**                   |
| `mac`      | string | yes      | MAC address bound to the hostname. Must match `AA:BB:CC:DD:EE:FF` with **uppercase** hex bytes.      |
| `ipv4`     | string | computed | IPv4 address allocated by the sidecar. Stable for the lifetime of the hostname.                      |
| `id`       | string | computed | Equals `hostname`.                                                                                   |

**Import:** `terraform import homelab_dns_reservation.example <hostname>`.

### `homelab_backup_credential`

An upload credential managed by the homelab backup server. The server
mints the bearer token on create; the token is stable for the lifetime of
the scope. To rotate, taint the resource — Terraform will destroy and
recreate, producing a new token. Objects already uploaded under the scope
are **not** removed when the credential is deleted.

```hcl
resource "homelab_backup_credential" "electronics_inventory" {
  scope     = "electronics-inventory"
  retention = 10
}

output "upload_token" {
  value     = homelab_backup_credential.electronics_inventory.token
  sensitive = true
}
```

| Attribute   | Type   | Required | Description                                                                                                         |
|-------------|--------|----------|---------------------------------------------------------------------------------------------------------------------|
| `scope`     | string | yes      | Scope key. URL segment and folder name under the rclone destination. **Changing forces destroy + recreate.**        |
| `retention` | number | yes      | Number of stored objects to retain in the scope. Integer in `[1, 100]`.                                             |
| `token`     | string | computed | Server-minted bearer token authorized to upload to this scope. Sensitive; stable for the lifetime of the scope.     |
| `id`        | string | computed | Equals `scope`.                                                                                                     |

**Import:** `terraform import homelab_backup_credential.example <scope>`.
The token is repopulated from the server on the next read.

### `homelab_rbd_image`

A raw (unformatted) RBD block image (RWO) in the provider's `ceph_pool`. The
image is created raw — no map / mkfs; the ceph-csi rbd driver formats it on
first mount via `fsType`. `size` is a Kubernetes quantity string. Resize is
**grow-only and in place**; shrinking is rejected, and renaming forces
destroy + recreate. **Destroy deletes the image and its data.**

```hcl
resource "homelab_rbd_image" "registry" {
  name = "registry-data"
  size = "20Gi"
}
```

| Attribute | Type   | Required | Description                                                                          |
|-----------|--------|----------|--------------------------------------------------------------------------------------|
| `name`    | string | yes      | RBD image name. **Changing forces destroy + recreate.**                              |
| `size`    | string | yes      | Capacity as a quantity string (e.g. `"20Gi"`). Grow-only; shrinking is rejected.     |
| `id`      | string | computed | Equals `name`.                                                                       |

**Import:** `terraform import homelab_rbd_image.example <name>`.

### `homelab_cephfs_subvolume`

A CephFS subvolume (RWX file storage) in the `cephfs` filesystem, under
`ceph_pool` as the subvolume group. `size` sets the quota (grow in place;
the mgr refuses to set a quota below current usage). Renaming forces destroy
+ recreate. **Destroy deletes the subvolume and its data.**

```hcl
resource "homelab_cephfs_subvolume" "media" {
  name = "media"
  size = "100Gi"
}

output "media_root_path" {
  value = homelab_cephfs_subvolume.media.path
}
```

| Attribute | Type   | Required | Description                                                                                      |
|-----------|--------|----------|--------------------------------------------------------------------------------------------------|
| `name`    | string | yes      | Subvolume name. **Changing forces destroy + recreate.**                                          |
| `size`    | string | yes      | Quota as a quantity string (e.g. `"100Gi"`). Resized in place.                                   |
| `path`    | string | computed | Absolute subvolume path (e.g. `/volumes/<group>/<name>/<uuid>`). Use as the chart's `rootPath`.  |
| `id`      | string | computed | Equals `name`.                                                                                   |

**Import:** `terraform import homelab_cephfs_subvolume.example <name>`.

### `homelab_s3_storage`

A set of S3 buckets **plus** a dedicated RGW user that owns exactly those
buckets and is created with `max_buckets = -1`, so it can never create new
ones. The access key is a **computed byproduct** of declaring buckets: the
provider creates each bucket with the admin credential, then re-links its
ownership to the per-release user. Adding a bucket to the set creates+links
it; removing one **deletes that bucket and all its objects**. Changing
`key_rotation` regenerates the access key on the same user, leaving buckets
and objects intact. **Destroy purges every bucket (objects included) and
removes the user.**

```hcl
resource "homelab_s3_storage" "app" {
  name    = "release-app"
  buckets = ["release-app-data", "release-app-cache"]

  # Bump to rotate the access key without touching buckets.
  key_rotation = "1"
}

output "s3_access_key_id" {
  value = homelab_s3_storage.app.access_key_id
}

output "s3_secret_access_key" {
  value     = homelab_s3_storage.app.secret_access_key
  sensitive = true
}
```

| Attribute           | Type        | Required | Description                                                                                         |
|---------------------|-------------|----------|-----------------------------------------------------------------------------------------------------|
| `name`              | string      | yes      | RGW user id and logical name. **Changing forces destroy + recreate.**                               |
| `buckets`           | set(string) | yes      | Buckets owned by this credential. Removing a bucket deletes it **and all its objects**.             |
| `key_rotation`      | string      | no       | Opaque rotation trigger. Changing it regenerates the access key; buckets and objects are untouched. |
| `access_key_id`     | string      | computed | Minted S3 access key id.                                                                            |
| `secret_access_key` | string      | computed | Minted S3 secret access key. Sensitive.                                                             |
| `id`                | string      | computed | Equals `name`.                                                                                      |

**Import:** `terraform import homelab_s3_storage.example <name>`. The key is
re-read from RGW; the bucket set is reconciled on the next plan.

## Development

The rbd / cephfs / cephconn / provider packages are **cgo** against
`librados` / `librbd`, so building needs the Ceph client **dev** libraries and
`CGO_ENABLED=1`:

```sh
# Debian/Ubuntu build deps (headers land at /usr/include/{rados,rbd}/...)
sudo apt-get install -y librados-dev librbd-dev pkg-config build-essential

CGO_ENABLED=1 go build -o terraform-provider-homelab .   # build
CGO_ENABLED=1 go test ./...                              # unit tests (no network)
```

(The S3 and quantity packages are pure Go and build/test without the Ceph
libs; only the cgo packages need them.)

Acceptance tests hit live backends. They are guarded by `TF_ACC=1` and
require the same env vars the provider itself consumes:

```sh
TF_ACC=1 \
  HOMELAB_DNS_RESERVATION_URL=... HOMELAB_DNS_RESERVATION_TOKEN=... \
  go test -v ./internal/dnsreservation/

TF_ACC=1 \
  HOMELAB_BACKUP_SERVER_URL=... HOMELAB_BACKUP_SERVER_TOKEN=... \
  go test -v ./internal/backupcredential/

# rbd + cephfs share the Ceph config group; needs a reachable cluster and an
# existing pool / subvolume group.
TF_ACC=1 \
  HOMELAB_CEPH_MON_HOST=... HOMELAB_CEPH_USER=... HOMELAB_CEPH_KEY=... HOMELAB_CEPH_POOL=... \
  CGO_ENABLED=1 go test -v ./internal/rbdimage/ ./internal/cephfssubvolume/

TF_ACC=1 \
  HOMELAB_S3_ENDPOINT=... HOMELAB_S3_ADMIN_ACCESS_KEY=... HOMELAB_S3_ADMIN_SECRET_KEY=... \
  go test -v ./internal/s3storage/
```

To see redacted HTTP request/response logs from the provider:

```sh
TF_LOG_PROVIDER_HOMELAB=DEBUG terraform apply
```

Bearer tokens in the logged messages and fields are masked automatically
(see `internal/httplog/redact.go`).
