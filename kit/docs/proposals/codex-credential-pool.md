Codex Credential Pool — Proposal (Opt‑In, Stateless)

Overview
- Add an optional, read‑only “pool” of pre‑prepared Codex homes that can be assigned to agents at startup. This augments (does not replace) today’s default of seeding from host `~/.codex`.
- Keeps writes local: refresh tokens and session files continue to write only to each agent’s `$HOME/.codex` inside the container.

Goals
- Stateless assignment; no persistent coordinator or database.
- Allow duplicates from the pool while distributing evenly.
- Do not change defaults; pool is strictly opt‑in and safe to roll back.
- Never write back into the pool; mount it read‑only.

Non‑Goals
- No persistent round‑robin index/state across runs.
- No automatic refresh or management of pool contents.

Host Layout (Pool)
- `DEVKIT_CODEX_POOL_DIR=/abs/path/to/pool`
- Each slot is a directory that contains a complete Codex home tree:
  - Example: `/abs/path/to/pool/slot1/{auth.json, sessions/, rollouts/, ...}`
  - Prepare slots out‑of‑band (e.g., log in once per slot on a helper machine and copy the resulting `~/.codex`).

Container Mount (Profile)
- New compose profile `pool` (not enabled by default):
  - Mount the pool read‑only as `/var/codex-pool` in `dev-agent`:
    - `- ${DEVKIT_CODEX_POOL_DIR}:/var/codex-pool:ro`

Configuration (Opt‑In)
- `DEVKIT_CODEX_CRED_MODE` — `host|pool` (default: `host`).
- `DEVKIT_CODEX_POOL_DIR` — absolute host path to the pool.
- `DEVKIT_CODEX_POOL_STRATEGY` — `by_index|shuffle` (default: `by_index`).
- Optional: `DEVKIT_CODEX_POOL_SEED` — integer; when set and strategy is `shuffle`, produces a reproducible per‑run shuffle.

Assignment Strategies (Stateless)
- by_index (simple, predictable): agent N → `slots[(N-1) % S]`.
- shuffle (even per run, duplicates allowed): once per run, shuffle slot order, then assign sequentially to agents.

Seeding Behavior (Pool Mode Only)
- Applies to `fresh-open` and `reset` flows.
- For each agent N:
  1) Ensure per‑agent HOME and dirs.
  2) Reset `$HOME/.codex`.
  3) Copy from the chosen slot: `cp -a /var/codex-pool/<slot>/. "$HOME/.codex"`.
  4) `chmod 600 "$HOME/.codex/auth.json"` if present.
- Fallbacks remain intact:
  - If pool missing/empty/invalid, fall back to host `~/.codex` or `/var/auth.json` (current behavior).
- Writes remain local to `$HOME/.codex`; pool is never mutated.

CLI/UX Changes
- New compose file `kit/compose.pool.yml` and profile `pool` (opt‑in).
- `preflight` (when pool mode is on):
  - Verify `DEVKIT_CODEX_POOL_DIR` exists and is mounted.
  - Count slots; warn if `agents > slots` (expected duplicates).
  - During seeding, log: `Agent i -> slot <name>`.

Wrapper (`codexw`) Behavior
- No change required. Optionally, it may seed from the pool only if `$HOME/.codex` is empty and the pool is mounted. Primary seeding happens during `fresh-open`/`reset`.

Migration & Rollback
- Migration: introduce the profile and env variables; implement seeding path behind `DEVKIT_CODEX_CRED_MODE=pool`. Defaults unchanged.
- Rollback: disable the `pool` profile or unset `DEVKIT_CODEX_CRED_MODE`; behavior returns to host `~/.codex` seeding.

Security Considerations
- Mount pool read‑only; never write to pool contents from containers.
- Continue redacting tokens in logs; only print slot names, not paths or contents.

Edge Cases & Notes
- Duplicates are acceptable and expected when `agents > slots`.
- Tokens in pool slots can expire; refresh happens locally inside each agent’s `$HOME/.codex` as today. Refresh does not propagate back to the pool.

