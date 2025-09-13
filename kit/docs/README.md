Dev Kit — Base Kit Details

- Networks:
  - `dev-internal` (internal: true; 172.30.10.0/24) for agents and optional DNS.
  - `dev-egress` (bridge) for proxies and internet access.
- Services:
  - `tinyproxy` (default): allowlist‑enforced HTTP(S) egress.
  - `dev-agent`: your project’s container (overlay overrides build/image and mount).
  - Optional `envoy` and `envoy-sni` (enable with `--profile envoy`).
  - Optional `dns` (dnsmasq allowlist; enable with `--profile dns`).
- Profiles:
  - `hardened`: read‑only root, limits; combine with others.
  - `dns`: forces agent DNS via `172.30.10.3` dnsmasq allowlist.
  - `envoy`: starts Envoy HTTP proxy and SNI TCP forward proxy.
  - `pool`: mounts a read‑only Codex credential pool into the agent (opt‑in).
- Helper:
- `devkit/kit/scripts/devkit -p <project> up|down|status|exec|logs|allow|warm|maintain|check-net` (wrapper in-repo; defaults to `-p codex`).
- Or call the binary directly after build: `devkit/kit/bin/devctl -p <project> ...`.
  - Monorepo overlay: use `-p dev-all` to mount the entire dev root at `/workspaces/dev`.
    - Change directory inside agent: `scripts/devctl -p dev-all exec-cd 1 ouroboros-ide bash`
    - Or attach into a specific repo: `scripts/devctl -p dev-all attach-cd 1 dumb-onion-hax`
    - Sync tmux windows to agent count: `scripts/devctl -p dev-all tmux-sync --count 3` (optionally `--service dev-agent`)
    - Add a mixed window to same tmux: `scripts/devctl -p dev-all tmux-add-cd 2 dumb-onion-hax --session devkit:mixed --name doh-2` (optionally `--service dev-agent`)
    - Apply a YAML layout: `scripts/devctl -p dev-all tmux-apply-layout --file tmux.yaml [--session NAME]`
      - Layout example:
        session: devkit:mixed
        windows:
          - index: 1
            path: ouroboros-ide
            name: ouro-1
            service: dev-agent
          - index: 2
            path: dumb-onion-hax
            name: doh-2
            service: dev-agent
    - Orchestrate overlays + tmux from one file:
      - `scripts/devctl layout-apply --file orchestration.yaml`
      - See devkit/README.md for a full example.
  - Isolation plan: see `isolation.md` for worktrees + per‑agent HOME design.
  - Worktrees + SSH workflow: see `worktrees_ssh.md` for end‑to‑end flows (`bootstrap`, `worktrees-*`, `open`).

## CLI Builds and Tests

- Build Go CLI: `cd devkit/cli/devctl && make build` (outputs `devkit/kit/bin/devctl`).
- Unit tests: `cd devkit/cli/devctl && go test ./...`.
- Dry‑run preview: append `--dry-run` to print `docker`/`tmux` commands without executing.
  - Layout only: `devkit/kit/scripts/devkit --dry-run tmux-apply-layout --file devkit/kit/examples/tmux.yaml`
  - Orchestration: `devkit/kit/scripts/devkit --dry-run layout-apply --file devkit/kit/examples/orchestration.yaml`
  - Generate orchestration from running containers: `devkit/kit/scripts/devkit layout-generate --service dev-agent --output /tmp/orchestration.yaml`
- Useful env vars:
  - `DEVKIT_ROOT`: override devkit root (used by tests).
  - `DEVKIT_NO_TMUX=1`: skip tmux integration (non‑interactive environments).
  - `DEVKIT_DEBUG=1`: echo executed commands to stderr.
  - `DEVKIT_INTERNAL_SUBNET`: internal network CIDR (default `172.30.10.0/24`).
  - `DEVKIT_DNS_IP`: DNS service IP on internal network (default `172.30.10.3`).
    - If you see "Address already in use" on up, pick a different subnet/IP here.
  - Git identity (required; no fallback): set at least one of these or have a host‑level git config.
    - `DEVKIT_GIT_USER_NAME` and `DEVKIT_GIT_USER_EMAIL` (preferred)
    - Or host `git config --global user.name` and `user.email` must be set
    - The CLI will fail fast if both name/email cannot be determined.

Further reading
- Mixed overlays + frontend notes: overlay-front-end-notes.md
  - Credential pool (opt‑in; defaults off):
    - `DEVKIT_CODEX_CRED_MODE=host|pool` (default `host`)
    - `DEVKIT_CODEX_POOL_DIR=/abs/path/to/pool` (host path; required when `pool`)
    - `DEVKIT_CODEX_POOL_STRATEGY=by_index|shuffle` (default `by_index`)
    - `DEVKIT_CODEX_POOL_SEED=<int>` (optional seed for `shuffle`)

## Credential Pool (Opt‑In)

Purpose
- Seed each agent’s `$HOME/.codex` from a read‑only pool of prepared Codex homes, instead of host `~/.codex`. Writes (refresh tokens, sessions) remain local to each agent’s `$HOME/.codex`.

Mount and profiles
- The pool is mounted when the compose file `kit/compose.pool.yml` is included (profile `pool`).
- The CLI will auto‑include this compose file for `fresh-open` and `reset` when `DEVKIT_CODEX_CRED_MODE=pool` is set. For other commands (`up`, `exec`, etc.), add `--profile pool` if the pool mount is needed.

Env configuration
- `DEVKIT_CODEX_CRED_MODE=pool` — enable pool mode.
- `DEVKIT_CODEX_POOL_DIR=/abs/path/to/pool` — host directory containing one subdir per slot (each a full `.codex` tree).
- `DEVKIT_CODEX_POOL_STRATEGY=by_index|shuffle` — slot assignment:
  - `by_index`: agent N → `slots[(N-1) % S]` (predictable, duplicates allowed).
  - `shuffle`: per‑run shuffle of slots; assign sequentially; optional `DEVKIT_CODEX_POOL_SEED` for reproducible shuffles.

Seeding behavior
- Applies to `fresh-open` and `reset` when pool mode is on:
  - Reset `$HOME/.codex`, copy `slot/.` → `$HOME/.codex`, `chmod 600 auth.json` if present.
  - Logs: `[seed] Agent i -> slot <name>`.
- If the pool is empty/missing, falls back to host `~/.codex` seeding (unchanged behavior).

Quickstart (pool mode)
- Host prep: create `/abs/path/to/pool/slot1`, `/abs/path/to/pool/slot2`, … each containing a `.codex` tree.
- Run (dry run recommended first):
  - `export DEVKIT_CODEX_CRED_MODE=pool`
  - `export DEVKIT_CODEX_POOL_DIR=/abs/path/to/pool`
  - Optional: `export DEVKIT_CODEX_POOL_STRATEGY=shuffle DEVKIT_CODEX_POOL_SEED=123`
  - Preflight: `devkit/kit/bin/devctl preflight`
  - Dry run: `devkit/kit/bin/devctl --dry-run -p codex fresh-open 2`


### Make Targets (Codex Overlay)

Convenience commands (essentials):
- Reset and open N agents (alias of `fresh-open`): `devctl -p <proj> reset [N]`.
- Scale agents without teardown: `devctl -p <proj> scale N`.
 - Scale and sync tmux: `devctl -p <proj> scale N --tmux-sync [--session NAME] [--service NAME]`.

Worktrees workflow (dev-all overlay):
- Setup per-agent branches + worktrees that track `origin/<base>`: `devctl -p dev-all worktrees-setup <repo> <count> [--base agent] [--branch main]`.
- Bootstrap using defaults from `overlays/dev-all/devkit.yaml`: `devctl -p dev-all bootstrap`.
- Open tmux across worktrees: `devctl -p dev-all worktrees-tmux <repo> <count>`.

Convenience targets to validate the codex overlay end‑to‑end:

- Build CLI: `make -C devkit build-cli`
- Fresh open with all profiles: `make -C devkit codex-fresh-open N=1`
- Verify inside dev‑agent: `make -C devkit codex-verify`
- End‑to‑end: `make -C devkit codex-ci`
- Cleanup: `make -C devkit codex-down`

Notes:
- All targets use the Go CLI (`kit/bin/devctl`).
- `codex-fresh-open` sets `DEVKIT_NO_TMUX=1` to avoid interactive tmux during automation.
- You can disable heavyweight installs during image build by exporting: `INSTALL_CODEX=false INSTALL_CLAUDE=false` before running `codex-fresh-open`.

### Fresh‑Open Integration Test (Optional)

This verifies hardened profiles and core tools are callable inside the agent.

- Requirements: Docker, and a container image that has `git`, `codex`, and `claude` installed and callable non‑interactively.
- Run:
  - `cd devkit/cli/devctl`
  - `DEVKIT_INTEGRATION=1 DEVKIT_IT_IMAGE=<image> go test -tags=integration -run FreshOpen_Integration`
- What it does:
  - Stitches compose with all profiles (hardened,dns,envoy) and overlay.
  - Brings up the stack via `fresh-open 1`.
  - Checks: `git --version`, `timeout 10s codex --version | codex exec 'ok'`, and `timeout 10s claude --version | --help`.
  - Tears down containers and networks.

## Git Over SSH (GitHub)

- Allow + setup (per agent): `scripts/devkit ssh-setup [--index N] [--key ~/.ssh/id_ed25519]`
  - Adds `ssh.github.com` to proxy/DNS allowlists (SSH over port 443).
  - Copies your host SSH key and known_hosts into `/workspace/.devhome/.ssh`.
  - Writes SSH config to tunnel via the proxy: `ProxyCommand nc -X connect -x tinyproxy:8888 %h %p`.
  - Sets `git config --global core.sshCommand 'ssh -F /workspace/.devhome/.ssh/config'`.
- Test: `scripts/devkit ssh-test N` (expects the GitHub banner).
- Flip remote + push: `scripts/devkit repo-push-ssh <repo-path> [--index N]`.
  - For the codex overlay (single repo at `/workspace`), use `.` as `<repo-path>`.
  - For `dev-all`, pass relative path like `ouroboros-ide`.
- tmux workflow: `scripts/devkit tmux-shells N` (auto-runs ssh-setup for each instance).
- Allowlist changes:
  - `devctl -p <proj> allow example.com` edits both proxy and DNS allowlists.
  - Restart services to apply: `devctl -p <proj> restart`.


## Implementation Style (Contrib Guidance)

- Avoid heredocs and monolithic shell blocks for complex flows. Prefer small, composable Go helpers that build command strings or use `RunWithInput` for file content.
- Keep steps atomic and testable: one responsibility per script/command.
- Examples:
  - Codex seeding under `internal/seed`: tiny scripts for wait, reset/mkdir, clone, fallback copy, chmod.
  - SSH config under `internal/sshcfg`: config string builder instead of inline heredocs.
- Rationale: reduces quoting/escaping bugs, simplifies auditing and testing, and makes error handling explicit.
