
Devkit CLI Migration and Codex Integration — Retrospective

Context
- Goal: Replace ad‑hoc bash scripts with a typed CLI that “just works” for devkit, including Codex‑based development, without extra flags.
- Constraints: Preserve compatibility for users; no surprises; keep wrappers simple; avoid network and port collisions.

Key Changes (Chronological Highlights)
1) CLI scaffold (Go)
   - Added devkit/cli/devctl with subcommands (up/down/…); wrappers prefer Go binary.
   - Removed bash devctl and updated docs/wrappers to point at Go.

2) Profiles and Compose arg builder
   - Implemented robust compose file resolution with profiles and overlay detection.
   - Dropped obsolete `version:` keys from Compose files to silence v2 warnings.

3) Network stability
   - Parameterized internal subnet/DNS IP via `DEVKIT_INTERNAL_SUBNET` and `DEVKIT_DNS_IP`.
   - Auto‑picked a non‑overlapping /24 and DNS .3 at runtime (preflight) to avoid “Address already in use”.

4) Codex env + seeding
   - Ensured per‑agent env for shells/exec: `HOME`, `CODEX_HOME=$HOME/.codex`, `CODEX_ROLLOUT_DIR`, `XDG_CACHE_HOME`, `XDG_CONFIG_HOME`.
   - Implemented fresh‑open seeding: wait for mounts, create dirs, then copy tokens/config.
   - Finalized behavior: fresh‑open now always refreshes each agent’s `$HOME/.codex` from host `~/.codex` (clone entire dir). If host dir is missing, fallback to `/var/auth.json`.
   - Tightened auth.json perms (600) and created rollouts/cache/config dirs consistently.

5) Tooling & testing
   - Added `codex-debug` to introspect container env + token paths.
   - Added `codex-test` that asserts a real “ok” from `codex exec 'reply with: ok'`.
   - Integration tests (tagged) for `fresh-open` and `codex-test` (when image available).
   - Makefile targets for build/verify and E2E workflows; CI hooks for codex E2E.

6) UX polish
   - Unified in‑repo wrapper `kit/scripts/devkit`; outer wrappers became one‑liners forwarding to it.
   - Docs: quickstart, CI, integration tests, network overrides, and troubleshooting.

Incidents & Lessons Learned
- Host token contamination: While debugging, a dummy auth was created; subsequent copies propagated a bad token. Fix: fresh‑open now always overwrites agent `$HOME/.codex` from host `~/.codex` to match the source of truth. Never write to host; only read from it.
- Seeding correctness over conditionals: Relying on “only copy if missing” hid changes when host tokens changed. Fix: always refresh per‑agent dir on fresh‑open; fallback only when host dir truly absent.
- Shell script syntax issues: Joining multiline `if/then` blocks via `&&` caused hard‑to‑spot parser errors. Fix: single embedded bash script with explicit newlines and guards; added `set -euo pipefail` and small waits for mounts.
- Codex expectations: tokens are more than a single file (sessions/config may be needed). Fix: clone the entire `~/.codex` for fresh‑open instead of cherry‑picking; set `CODEX_ROLLOUT_DIR` and `XDG_*` to writable per‑agent dirs.
- Port conflicts: Compose internal subnet clashed with host routes. Fix: auto‑select a non‑overlapping CIDR and export to Compose; document overrides.

What “Just Works” Means Now
- `fresh-open N`:
  - Tears down cleanly; auto‑selects subnet/DNS; brings up all profiles.
  - For each agent, clones host `~/.codex` into `$HOME/.codex`; creates needed dirs; sets permissions.
  - Shells/exec inherit correct `HOME`, `CODEX_HOME`, `CODEX_ROLLOUT_DIR`, `XDG_*`.
- Verifications:
  - `codex-debug N` shows correct env + files.
  - `codex-test N` succeeds only when Codex replies exactly “ok”.

Action Items
- Add a one‑line “seed summary” during fresh‑open listing copied files per agent for transparency.
- Consider opt‑in “preserve agent state” flag (skip refresh) for long‑lived sessions.
- Extend tests to cover seeding of edge layouts (older Codex versions), and env exports across more commands.
- Add a preflight that validates that `~/.codex` looks structurally complete before seeding and prints a helpful hint if not.

Takeaways
- Make “source of truth” explicit: host `~/.codex` must be considered the canonical token store; agents must be refreshed from it predictably.
- Favor idempotent, declarative steps for setup: fewer conditionals, more “ensure”/“clone then fix perms”.
- Provide deterministic, machine‑verifiable checks (`codex-test`) to avoid human ambiguity.
