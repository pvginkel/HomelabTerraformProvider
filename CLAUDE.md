# CLAUDE.md

Context for Claude Code working in this repo. User-facing documentation
(what the provider does, how to install it, per-resource reference) lives
in [README.md](README.md) — read it once before editing resources.

## Naming (load-bearing — don't drift)

| Thing | Value |
|---|---|
| Go module path | `github.com/pvginkel/HomelabTerraformProvider` |
| Binary | `terraform-provider-homelab` |
| Provider source | `pvginkel/homelab` |
| Resource prefix | `homelab_` |

## Specs live in sibling repos

Each resource has its backend API spec'd outside this repo. Read the
relevant spec before changing the corresponding resource.

- `homelab_dns_reservation`
  - `/work/Ansible/docs/specs/dns-reservation-api.md` — sidecar HTTP API surface
  - `/work/Ansible/docs/specs/dns-reservation-terraform.md` — Terraform resource shape
- `homelab_backup_credential`
  - `/work/DockerImages/backup-server/api.md` — backup server HTTP API (the
    `/credentials/*` group is the Terraform-facing surface)

## Operator runs Terraform — not Claude

The operator runs all `terraform apply` / `terraform destroy` invocations
against real infrastructure (the managed VMs live in
`/work/Ansible/terraform/`). Claude prepares the change, proposes the
exact command, and waits for full output. Same convention as
`/work/Ansible/CLAUDE.md`.

Read-only operations (inspecting files, `go build`, `go test` without
`TF_ACC`) are fine for Claude to run directly.

## Conventions

- Small, focused commits with clear messages. Don't batch unrelated work.
- Commit messages: short imperative subject, body explains why. Always
  include the `Co-Authored-By` trailer for Claude.
- Don't ship dormant config "for later" — implement and exercise it now,
  or drop it.
- Strip scaffolding/walkthrough comments once a file is built. Keep only
  comments that carry a non-obvious *why*.
- Mirror the existing resource layout when adding a new one: each resource
  gets its own `internal/<name>/` package with `models.go`, `errors.go`,
  `client.go`, `resource.go`, and matching `client_test.go` +
  acceptance-test pair. The provider wires the per-resource client via a
  package-local `ProviderData` interface so the resource package stays
  independent of `internal/provider`.
