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

Domain Model & Components (No Heredocs)

Design goals
- Keep logic in typed, testable Go packages; avoid monolithic shell heredocs.
- Compose container commands from small, single‑purpose steps (strings) like current `seed` package.
- Make selection, assignment, and seeding orthogonal (clean interfaces), so UX wiring in `main.go` stays small.

Core types
- Config
  - `type CredMode string // "host"|"pool"`
  - `type Strategy string // "by_index"|"shuffle"`
  - `type PoolConfig struct { Mode CredMode; Dir string; Strategy Strategy; Seed int; }`
  - Source: env vars; parsed in a tiny `internal/config/pool.go` helper.

- Pool slots
  - `type Slot struct { Name string; Path string }` // Path in container, e.g. `/var/codex-pool/slot1`
  - Discovery: `pool.Discover(root string) ([]Slot, error)` — lists immediate subdirs under `/var/codex-pool` (no recursion), sorted by name.

- Assignment
  - `type Assigner interface { Assign(slots []Slot, agentIndex, agentCount int) Slot }`
  - Implementations:
    - `assign.ByIndex` — `(agentIndex-1) % len(slots)`
    - `assign.Shuffle` — creates a per‑run permutation seeded by `PoolConfig.Seed` (or time if 0); returns `perm[(agentIndex-1) % len(slots)]`.

- Seeding plan (imperative but small steps)
  - `type SeedStep struct { Cmd []string }` // concrete argv to run in container via `docker compose exec -T ...`
  - `type Plan struct { Steps []SeedStep }`
  - Builders (pure):
    - `seed.BuildResetPlan(home string) Plan` — mkdirs and rm -rf `$home/.codex` (split into multiple steps).
    - `seed.BuildCopyFrom(hostPath, home string) Plan` — copies `hostPath/.` → `$home/.codex` and chmods auth.json.
    - Existing `internal/seed` returns small bash strings; extend with new helpers that return argv slices (no heredocs), while leaving current functions in place to minimize churn. We can gradually migrate call sites.

Execution flow (fresh-open/reset)
1) If `PoolConfig.Mode != pool`, use existing host‑seed path unchanged.
2) Discover `slots := pool.Discover("/var/codex-pool")`; if empty, fall back to host‑seed path.
3) For each agent i in 1..N:
   - `slot := assigner.Assign(slots, i, N)`
   - Build: `steps := seed.BuildResetPlan(home) + seed.BuildCopyFrom(slot.Path, home)`
   - Execute sequentially via existing `runCompose("exec", "-T", "--index", i, ...steps[j].Cmd...)`.
   - Log: `Agent i -> slot <slot.Name>` (no sensitive contents).

CLI wiring (ergonomic, minimal)
- Add `pool` profile in compose to provide the mount only when requested.
- Parse env to `PoolConfig` in `main.go` (tiny helper).
- In `fresh-open`/`reset`, branch on `CredMode==pool` and invoke the above flow.
- `preflight`: If `CredMode==pool`, run `pool.Discover` and warn when `agents > len(slots)`.

Testing strategy
- `internal/pool`: temp dir with a few subdirs → verify `Discover` order and filtering.
- `internal/assign`: table tests for by_index and shuffle (with fixed seed) across various counts.
- `internal/seed`: assert steps contain expected argv (cp, chmod) and that paths are correctly joined.
- `main` (lightweight): with `--dry-run`, assert the composed docker commands include the correct `--index`, slot path, and home.

Error handling & fallbacks
- Empty/missing pool → log and fall back to host‑seed path.
- Missing `auth.json` in slot → proceed (some slots may be intentionally partial); chmod is conditional.
- Copy failures (permissions/read‑only) → surface error and stop for that agent; others continue (match current failure model).

Why this avoids heredocs
- Every step is either an argv slice or a small single‑line bash (consistent with existing `seed` package style), executed by `docker compose exec -T ...`.
- No multiline shell blocks are generated; all quoting stays simple and testable.

