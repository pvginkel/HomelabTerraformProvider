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

The provider talks to two independent backends. Each is configured with a
`*_url` + `*_token` pair, with environment-variable fallbacks. Either pair
may be left unset — only the resources that actually use that pair will
fail. Setting exactly one half of a pair (URL without token or vice
versa) is rejected as a misconfiguration.

| Attribute                | Env var                          | Used by                       |
|--------------------------|----------------------------------|-------------------------------|
| `dns_reservation_url`    | `HOMELAB_DNS_RESERVATION_URL`    | `homelab_dns_reservation`     |
| `dns_reservation_token`  | `HOMELAB_DNS_RESERVATION_TOKEN`  | `homelab_dns_reservation`     |
| `backup_server_url`      | `HOMELAB_BACKUP_SERVER_URL`      | `homelab_backup_credential`   |
| `backup_server_token`    | `HOMELAB_BACKUP_SERVER_TOKEN`    | `homelab_backup_credential`   |

Both token attributes are marked sensitive. The typical pattern is to
leave the provider block empty and supply all four values via environment.

```hcl
provider "homelab" {}
```

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

## Development

```sh
go build -o terraform-provider-homelab .   # build
go test ./...                              # unit tests (no network)
```

Acceptance tests hit live backends. They are guarded by `TF_ACC=1` and
require the same env vars the provider itself consumes:

```sh
TF_ACC=1 \
  HOMELAB_DNS_RESERVATION_URL=... HOMELAB_DNS_RESERVATION_TOKEN=... \
  go test -v ./internal/dnsreservation/

TF_ACC=1 \
  HOMELAB_BACKUP_SERVER_URL=... HOMELAB_BACKUP_SERVER_TOKEN=... \
  go test -v ./internal/backupcredential/
```

To see redacted HTTP request/response logs from the provider:

```sh
TF_LOG_PROVIDER_HOMELAB=DEBUG terraform apply
```

Bearer tokens in the logged messages and fields are masked automatically
(see `internal/httplog/redact.go`).
