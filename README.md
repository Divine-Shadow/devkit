Dev Kit — Reusable Containerized Dev Environment

This dev kit extracts the dual‑network, allowlisted egress development setup into a reusable package you can apply to any project in your `dev/` or `projects/` folder via small per‑project overlays.

Quick start (with the built‑in `codex` overlay):
- Bring up: `devkit/kit/scripts/devctl -p codex up`
- Exec shell: `devkit/kit/scripts/devctl -p codex exec 0 bash`
- Add allowlist domain: `devkit/kit/scripts/devctl -p codex allow example.com`
- Hardened + DNS profiles: `devkit/kit/scripts/devctl -p codex up --profile hardened,dns`
- Tear down: `devkit/kit/scripts/devctl -p codex down`

SSH (GitHub) quickstart:
- One-time per agent: `scripts/devkit ssh-setup --index 1` then `scripts/devkit ssh-test 1`
- Flip origin to SSH and push: `scripts/devkit repo-push-ssh .`

Layout:
- `kit/`: base Compose, proxy, DNS, scripts, and docs.
- `overlays/<project>/`: per‑project overrides (`compose.override.yml`, `devkit.yaml`).

Key design:
- Dual networks: `dev-internal` (internal: true) for agents; `dev-egress` for internet‑facing proxy.
- Proxy (Tinyproxy by default) is dual‑homed; agents only join `dev-internal` and must egress via proxy.
- Optional DNS allowlist (dnsmasq) and hardened profile (read‑only root, resource limits).

See `kit/docs/README.md` for more details.

Proposal: Bash → Go CLI Migration
- Rationale, scope, and plan to migrate `kit/scripts/devctl` to a typed CLI while keeping shell shims.
- See: `kit/docs/proposals/devkit-cli-migration.md`.
