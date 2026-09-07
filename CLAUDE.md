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
  - `/work/DockerImages/dnsmasq-management-api/dns-reservation-api.md` —
    sidecar HTTP API surface, kept beside the implementation
  - `/work/AnsibleSpecs/slices/completed/dns-reservation-provider/dns-reservation-terraform.md`
    — Terraform resource shape. Frozen with the slice that shipped it, so
    the provider's own README is the as-built reference where the two
    disagree.
- `homelab_backup_credential`
  - `/work/DockerImages/backup-server/api.md` — backup server HTTP API (the
    `/credentials/*` group is the Terraform-facing surface)

## Operator runs Terraform — not Claude

The operator runs all `terraform apply` / `terraform destroy` invocations
against real infrastructure (the managed VMs live in
`/work/Ansible/terraform/`). Claude prepares the change, proposes the
exact command, and waits for full output. Same convention as
`/work/Ansible/CLAUDE.md`.

Read-only operations are fine for Claude to run directly — inspecting
files, and the build and the non-acceptance tests. Those two run as
`kc project build` and `kc project test` from this repo's root; for a
one-off, `cexec go go test ./internal/<pkg>/`. The Go toolchain is a
sidecar, not the container Claude runs in, so a bare `go build` here finds
no `go` at all. The acceptance tests stay the operator's keystroke:
`kc project test` never sets `TF_ACC`, so every `TestAcc*` skips and the
suite touches no live backend.

## Conventions

- Commit directly to `main` as you go — no topic branches, no PRs. Make
  small, focused commits with clear messages. Don't batch unrelated work.
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
- Provider config groups are trigger-gated, not all-or-nothing. Each group
  has one trigger attribute (e.g. `ceph_mon_host`, `s3_endpoint`,
  `zfs_pools`): empty trigger disables the whole group and its other members
  (and their env-var fallbacks) are ignored; a set trigger makes the rest
  mandatory. This keeps the provider usable in a shared environment where
  unrelated `HOMELAB_*` vars are set for it. See `validateGroup` in
  `internal/provider/provider.go`.
