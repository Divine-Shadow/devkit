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
- Helper:
  - `scripts/devkit -p <project> up|down|status|exec|logs|allow|warm|maintain|check-net` (wrapper prefers Go CLI).
  - Or call the binary directly after build: `devkit/kit/bin/devctl -p <project> ...`.
  - Monorepo overlay: use `-p dev-all` to mount the entire dev root at `/workspaces/dev`.
    - Change directory inside agent: `scripts/devctl -p dev-all exec-cd 1 ouroboros-ide bash`
    - Or attach into a specific repo: `scripts/devctl -p dev-all attach-cd 1 dumb-onion-hax`
  - Isolation plan: see `isolation.md` for worktrees + per‑agent HOME design.
  - Worktrees + SSH workflow: see `worktrees_ssh.md` for end‑to‑end flows (`bootstrap`, `worktrees-*`, `open`).

## CLI Builds and Tests

- Build Go CLI: `cd devkit/cli/devctl && make build` (outputs `devkit/kit/bin/devctl`).
- Unit tests: `cd devkit/cli/devctl && go test ./...`.
- Dry‑run preview: append `--dry-run` to print `docker`/`tmux` commands without executing.
- Useful env vars:
  - `DEVKIT_ROOT`: override devkit root (used by tests).
  - `DEVKIT_NO_TMUX=1`: skip tmux integration (non‑interactive environments).
  - `DEVKIT_DEBUG=1`: echo executed commands to stderr.

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
