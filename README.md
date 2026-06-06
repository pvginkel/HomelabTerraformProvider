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

The provider is consumed via a Terraform `dev_overrides` block; nothing is
published to a registry. `dev_overrides` bypasses the registry, the version
constraint, and the consumer's `.terraform.lock.hcl` — a rebuilt binary is
picked up with no `terraform init` and no lock refresh.

## Install

`scripts/install-local.sh` builds the binary and installs it into the
dev-override directory (`~/.local/lib/terraform-providers` by default):

```sh
./scripts/install-local.sh
```

The Terraform CLI config must point a dev override at that directory. The
`modern-app-dev` image already ships this at `/etc/terraform.rc`
(`TF_CLI_CONFIG_FILE`); for a bare workstation, put it in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "pvginkel/homelab" = "/home/youruser/.local/lib/terraform-providers"
  }
  direct {}
}
```

The path must be absolute — Terraform does not expand `~` here.

In CI the same binary is built by the Jenkins pipeline, archived, and baked
into the `modern-app-dev` image at the same dev-override directory.

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
