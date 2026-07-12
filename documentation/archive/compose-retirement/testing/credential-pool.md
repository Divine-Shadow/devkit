Credential Pool — Verification Plan (Do Not Run Automatically)

Objective
- Manually verify that pool mode seeds agent `$HOME/.codex` from a mounted pool using the selected strategy, while preserving default behavior when disabled.

Prerequisites
- Docker available; DevKit CLI built: `cd devkit/cli/devctl && make build` (produces `devkit/kit/bin/devctl`).
- A host pool directory with at least two slots:
  - `/tmp/codex-pool/slot1/.codex/{auth.json,...}`
  - `/tmp/codex-pool/slot2/.codex/{auth.json,...}`
  - Use safe, non‑production tokens for testing.

Environment
- `export DEVKIT_CODEX_CRED_MODE=pool`
- `export DEVKIT_CODEX_POOL_DIR=/tmp/codex-pool`
- Optional: `export DEVKIT_CODEX_POOL_STRATEGY=shuffle DEVKIT_CODEX_POOL_SEED=42`

Step 1 — Preflight (no containers modified)
- Run: `devkit/kit/bin/devctl preflight`
- Expect:
  - Docker OK, tmux status, `~/.codex` info (informative only).
  - Pool: `OK (N slots in /tmp/codex-pool). Reminder: include --profile pool to mount.`

Step 2 — Dry‑run fresh‑open (no containers modified)
- Run: `devkit/kit/bin/devctl --dry-run -p codex fresh-open 3`
- Inspect stderr/stdout:
  - Compose args include `-f kit/compose.pool.yml`.
  - For each agent i=1..3, a sequence of `docker compose ... exec --index i dev-agent` commands:
    - `rm -rf /workspace/.devhome-agent<i>/.codex`
    - `mkdir -p .../.codex/rollouts .../.cache .../.config .../.local`
    - `cp -a /var/codex-pool/<slot>/. /workspace/.devhome-agent<i>/.codex`
    - `bash -lc if [ -f '/workspace/.../.codex/auth.json' ] ... chmod 600 ...`
  - A log line: `[seed] Agent i -> slot <name>` for each agent.

Step 3 — Live run (controlled)
- Warning: this starts containers. Ensure no important sessions are running.
- Bring up 2 agents: `devkit/kit/bin/devctl -p codex fresh-open 2`
- Verify mapping (from logs) matches expected strategy:
  - by_index: A1→slot1, A2→slot2
  - shuffle(seed=42): stable mapping per run.
- Exec into agent 1: `devkit/kit/bin/devctl -p codex exec 1 bash -lc 'ls -la $HOME/.codex; test -f $HOME/.codex/auth.json && stat -c %a $HOME/.codex/auth.json'`
  - Expect contents from its slot; `600` permissions on `auth.json` if present.
- Repeat for agent 2.

Step 4 — Duplicates tolerance
- Create a pool with 1 slot only. Re‑run `fresh-open 2`.
- Expect both agents map to the same slot; both get valid `.codex` contents; no errors.

Step 5 — Fallback behavior
- Unset pool or point to empty dir:
  - `unset DEVKIT_CODEX_CRED_MODE` or `export DEVKIT_CODEX_POOL_DIR=/tmp/empty`
- Re‑run `fresh-open 1`; expect seeding from host `~/.codex` (original behavior) and no pool log lines.

Step 6 — Strategy checks
- With 3 agents and 2 slots:
  - by_index: mapping should wrap (1→slot1, 2→slot2, 3→slot1).
  - shuffle with fixed seed: recompute expected permutation offline and compare logs.

Cleanup
- `devkit/kit/bin/devctl -p codex down` (or `make -C devkit codex-down`).

Notes
- For commands other than `fresh-open`/`reset`, add `--profile pool` if you need the pool mount present during interactive `exec`.
- Never store real production tokens in `/tmp`; use dedicated test credentials.

