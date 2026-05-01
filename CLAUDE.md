# CLAUDE.md

Context for Claude Code working in this repo.

## What this repo is

Custom Terraform provider that bundles all homelab-specific resource types behind one provider. Built with [terraform-plugin-framework](https://github.com/hashicorp/terraform-plugin-framework).

The first (and currently only) resource will be `homelab_dns_reservation`, talking to the dnsmasq sidecar API. Future resources for other homelab APIs land in this same provider.

## Naming (load-bearing — don't drift)

| Thing | Value |
|---|---|
| Go module path | `github.com/pvginkel/HomelabTerraformProvider` |
| Binary | `terraform-provider-homelab` |
| Provider source | `pvginkel/homelab` |
| Resource prefix | `homelab_` |

## Specs live in the sibling Ansible repo

The contract for the first resource:

- `/work/Ansible/docs/specs/dns-reservation-api.md` — sidecar HTTP API surface
- `/work/Ansible/docs/specs/dns-reservation-terraform.md` — Terraform resource shape

Read both before implementing the resource.

## Dev workflow

Build the binary in place:

```
go build -o terraform-provider-homelab
```

Point Terraform at the local build via `~/.terraformrc`:

```
provider_installation {
  dev_overrides {
    "pvginkel/homelab" = "/work/HomelabTerraformProvider"
  }
  direct {}
}
```

Consuming modules then declare:

```
terraform {
  required_providers {
    homelab = {
      source = "pvginkel/homelab"
    }
  }
}
```

`~/.terraformrc` is a per-machine concern — don't commit it.

## Operator runs Terraform — not Claude

The operator runs all `terraform apply` / `terraform destroy` invocations against real infrastructure (the managed VMs live in `/work/Ansible/terraform/`). Claude prepares the change, proposes the exact command, and waits for full output. Same convention as `/work/Ansible/CLAUDE.md`.

Read-only operations (inspecting files, running `go build`, `go test`) are fine for Claude to run directly.

## Conventions

- Small, focused commits with clear messages. Don't batch unrelated work.
- Commit messages: short imperative subject, body explains why. Always include the `Co-Authored-By` trailer for Claude.
- Don't ship dormant config "for later" — implement and exercise it now, or drop it.
- Strip scaffolding/walkthrough comments once a file is built. Keep only comments that carry a non-obvious *why*.
