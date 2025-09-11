Dev Kit — Reusable Containerized Dev Environment

This dev kit extracts the dual‑network, allowlisted egress development setup into a reusable package you can apply to any project in your `dev/` or `projects/` folder via small per‑project overlays.

Quick start (with the built‑in `codex` overlay):
- Build once: `cd devkit/cli/devctl && make build` (outputs `devkit/kit/bin/devctl`).
- Bring up: `devkit/kit/scripts/devkit up` (defaults to `-p codex`)
- Exec shell: `devkit/kit/scripts/devkit exec 0 bash`
- Add allowlist domain: `devkit/kit/scripts/devkit allow example.com`
- Hardened + DNS profiles: `devkit/kit/scripts/devkit up --profile hardened,dns`
- Tear down: `devkit/kit/scripts/devkit down`

Credential pool (proposal, opt‑in):
- For teams needing multiple Codex identities, see `kit/docs/proposals/codex-credential-pool.md`.
- Summary: mount a read‑only pool of prepared Codex homes and seed agents from slots by index or per‑run shuffle. Defaults remain unchanged.

Essentials (batteries-included paths):
- Hard reset + open N agents (alias): `devkit/kit/scripts/devkit reset 3` (same as `fresh-open 3`).
- Scale agents without teardown: `devkit/kit/scripts/devkit scale 4`.
- Per-agent SSH over 443: `devkit/kit/scripts/devkit ssh-setup --index 1` then `ssh-test 1`.

Worktrees (isolated branches per agent, dev-all overlay):
- Defaults live in `overlays/dev-all/devkit.yaml` (repo, agents, base_branch, branch_prefix).
- Bootstrap end-to-end: `devkit/kit/scripts/devkit -p dev-all bootstrap` (uses defaults) or `bootstrap ouroboros-ide 3`.
- Create/verify manually:
  - Setup: `devkit/kit/scripts/devkit -p dev-all worktrees-setup ouroboros-ide 3`
  - Open windows: `devkit/kit/scripts/devkit -p dev-all worktrees-tmux ouroboros-ide 3`

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


Retrospective: Journey & Lessons
- Summary of the migration, networking fixes, Codex seeding/env work, tests, and next steps.
- See: `kit/docs/journey-retrospective.md`.


Proposal: Bash → Go CLI Migration
- Rationale, scope, and plan to migrate `kit/scripts/devctl` to a typed CLI while keeping shell shims.
- See: `kit/docs/proposals/devkit-cli-migration.md`.
