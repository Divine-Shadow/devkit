# Nix Runtime Parity Evidence

This file records the first concrete Nix-native implementation slice for the
Compose retirement work. It follows
`kit/docs/proposals/nix-runtime-verification-contract.md`.

## Implemented Artifacts

- `flake.nix`: dev shells for the current container families and a Nix-built
  `postgres-broker` package.
- `devShells.x86_64-linux.dev-all`: replacement shell for the external
  Ouroboros Codex agent image.
- `devShells.x86_64-linux.ouroboros-terraform`: `dev-all` plus Terraform and
  Packer pins.
- `devShells.x86_64-linux.pokeemerald`: `dev-all` plus ARM embedded toolchain.
- `devShells.x86_64-linux.ouroboros-static-front-end`: static frontend shell.
- `devShells.x86_64-linux.template-agent`, `runtime-test-agent`, and
  `tinyproxy`: replacements for lightweight support images.
- `cli/devctl/internal/runtime/{agent,plan,launch}`: native agent identity,
  launch plan, and bubblewrap command construction.
- `cli/devctl/internal/runtime/readiness`: runtime-vs-repo readiness model used
  by native agent checks.
- `devctl native plan`: inspectable native plan output.
- `devctl native exec`: prepares per-agent state and runs a flake shell through
  bubblewrap.
- `devctl native readiness`: reports runtime readiness, repo readiness, and
  capacity availability separately.
- `devctl broker start|status|stop`: manages the host-side broker process used
  by native sandboxes.
- `devctl native prepare` and `devctl native capacity`: prepare dedicated
  worktrees/state and report capacity from native readiness.
- Top-level `up`, `down`, `restart`, `status`, `logs`, `scale`, `exec`,
  `attach`, and `ensure-ready` target the native runtime for `dev-all`;
  `devctl compose ...` is the explicit legacy Docker Compose path.
- Native agent plans now use `/worktrees/agentN/<repo>` for every agent,
  including agent 1, and keep HOME/Codex/XDG state under `/agent-state`.
  `native prepare` and lifecycle `up` write a JSON manifest under the native
  host state root for downstream tmux/layout/session orchestration.
- `tmux-sync`, `tmux-add-cd`, `tmux-apply-layout`, `layout-apply`,
  `tmux-shells`, `open`, and `worktrees-tmux` now route `dev-all` sessions
  through native `devkit exec` commands instead of Docker exec.
- `fresh-open`, `reset`, `run`, `bootstrap`, `worktrees-setup`,
  `worktrees-branch`, `worktrees-status`, and `worktrees-sync` now use native
  lifecycle/worktree orchestration for `dev-all`; legacy Compose behavior
  remains only for non-`dev-all` overlays pending quarantine.
- `check-net`, `check-codex`, `check-sts`, `warm`, and `maintain` route
  `dev-all` diagnostics/hooks through native `devkit exec`.
- `overlays/dev-all/devkit.yaml` defines explicit native repo readiness checks
  for git reachability, frontend warm/install, frontend typecheck, frontend
  tests, and the core SBT compile check.

## Verified Commands

All commands require explicit flakes on this host:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
nix --extra-experimental-features 'nix-command flakes' build --no-link --print-out-paths .#postgres-broker
nix --extra-experimental-features 'nix-command flakes' develop .#runtime-test-agent --command bash -lc 'git --version && ssh -V && curl --version | head -1'
nix --extra-experimental-features 'nix-command flakes' develop .#template-agent --command bash -lc 'git --version && uv --version && python3 --version'
nix --extra-experimental-features 'nix-command flakes' develop .#dev-all --command bash -lc 'spago --version && codex --version && docker --version && go version && playwright --version'
nix --extra-experimental-features 'nix-command flakes' develop .#dev-all --command bash -lc 'mgba-headless --help 2>&1 | grep -q -- --script'
nix --extra-experimental-features 'nix-command flakes' develop .#ouroboros-terraform --command bash -lc 'terraform version | head -2 && packer version'
nix --extra-experimental-features 'nix-command flakes' develop .#pokeemerald --command bash -lc 'arm-none-eabi-gcc --version | head -1 && arm-none-eabi-as --version | head -1'
nix --extra-experimental-features 'nix-command flakes' develop .#ouroboros-static-front-end --command bash -lc 'spago --version && netlify --version'
nix --extra-experimental-features 'nix-command flakes' develop .#tinyproxy --command bash -lc 'tinyproxy -h 2>&1 | head -5; uv --version'
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 go test ./...
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit go run . -p dev-all native exec --repo devkit --flake .#runtime-test-agent -- git --version
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit go run . -p dev-all native exec --repo devkit --flake .#dev-all -- bash -lc 'spago --version && codex --version && docker --version && go version && playwright --version && mgba-headless --help 2>&1 | grep -q -- --script'
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit go run . -p dev-all native exec --repo devkit --flake .#dev-all -- node -e 'const { chromium } = require("@playwright/test"); (async () => { const browser = await chromium.launch({ headless: true }); const page = await browser.newPage(); await page.setContent("<title>native-bwrap-playwright-ok</title>"); console.log(await page.title()); await browser.close(); })().catch((err) => { console.error(err); process.exit(1); });'
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit go run . -p dev-all native readiness --repo devkit --flake .#runtime-test-agent --repo-check 'exit 7' --format json
kit/scripts/devkit -p dev-all broker status --format json
kit/scripts/devkit -p dev-all native prepare --repo ouroboros-ide --count 2 --dry-run --format json
kit/scripts/devkit -p dev-all native capacity --repo devkit --count 1 --flake .#runtime-test-agent --repo-check 'exit 7' --format json
kit/scripts/devkit -p dev-all --dry-run scale 2 --repo ouroboros-ide --broker-socket /tmp/devkit-scale.sock --broker-state-root /tmp/devkit-scale-state --skip-ready --format json
kit/scripts/devkit -p dev-all --dry-run exec 1 --repo ouroboros-ide --broker-socket /tmp/devkit-scale.sock -- echo hi
kit/scripts/devkit -p dev-all --dry-run compose exec --index 1 dev-agent echo hi
kit/scripts/devkit -p dev-all native plan --repo ouroboros-ide --index 1 --format json
kit/scripts/devkit -p dev-all --dry-run native prepare --repo ouroboros-ide --count 2 --format json
kit/scripts/devkit -p dev-all --dry-run tmux-sync --count 2 --session devkit-native-smoke
kit/scripts/devkit -p dev-all --dry-run worktrees-tmux ouroboros-ide 2
bash -lc 'kit/scripts/devkit -p dev-all --dry-run layout-apply --file <(printf "%s\n" "session: native-layout" "windows:" "  - index: 1" "    name: agent-1" "    path: frontend" "  - index: 2" "    name: agent-2" "    path: /workspaces/dev/agent-worktrees/agent2/ouroboros-ide")'
DEVKIT_NO_TMUX=1 kit/scripts/devkit -p dev-all --dry-run fresh-open 2
DEVKIT_NO_TMUX=1 kit/scripts/devkit -p dev-all --dry-run reset 2
kit/scripts/devkit -p dev-all --dry-run run ouroboros-ide 2
DEVKIT_NO_TMUX=1 kit/scripts/devkit -p dev-all --dry-run bootstrap ouroboros-ide 2
kit/scripts/devkit -p dev-all --dry-run worktrees-setup ouroboros-ide 2 --base agent --branch main
kit/scripts/devkit -p dev-all --dry-run worktrees-branch ouroboros-ide 2 test-branch
kit/scripts/devkit -p dev-all --dry-run worktrees-status ouroboros-ide --index 2
kit/scripts/devkit -p dev-all --dry-run worktrees-sync ouroboros-ide --pull --index 2
kit/scripts/devkit -p dev-all --dry-run check-net
kit/scripts/devkit -p dev-all --dry-run check-codex
kit/scripts/devkit -p dev-all --dry-run check-sts tinyproxy
kit/scripts/devkit -p dev-all --dry-run warm
```

Broker smoke command, run with the broker process left open in one terminal and
the native smoke in another:

```bash
sock=$(mktemp -u /tmp/devkit-native-broker.XXXXXX.sock)
cd brokers/postgres-broker
BROKER_LISTEN="unix://$sock" BROKER_UPSTREAM="unix:///var/run/docker.sock" BROKER_ALLOWED_IMAGES="postgres:latest" BROKER_ALLOW_PULLS=true \
  nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 go run .
```

```bash
sock=/tmp/devkit-native-broker.<id>.sock

cd cli/devctl
nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit \
  go run . -p dev-all native exec --repo devkit --flake .#runtime-test-agent --broker-endpoint "$sock" -- bash -lc '
    set -euo pipefail
    echo "DOCKER_HOST=$DOCKER_HOST"
    test "${DEVKIT_NATIVE_AGENT:-}" = 1
    test "$DOCKER_HOST" = "unix://'"$sock"'"
    test -S "'"$sock"'"
    test ! -e /var/run/docker.sock
    curl --unix-socket "'"$sock"'" -fsS http://docker/_ping | grep -qx OK
    code=$(curl -sS -o /tmp/redis-create.out -w "%{http_code}" --unix-socket "'"$sock"'" -H "Content-Type: application/json" -d "{\"Image\":\"redis:7\"}" "http://docker/v1.45/containers/create?name=devkit-native-smoke-redis")
    echo "redis_create_http=$code"
    test "$code" = 403
    grep -qi forbidden /tmp/redis-create.out
    echo native-broker-smoke-ok
  '
```

Observed key versions:

- Codex CLI: `codex-cli 0.130.0`.
- Docker CLI: `Docker version 27.5.1`.
- Go: `go1.22.4`.
- Spago: `0.93.45`.
- Netlify CLI: `netlify-cli/26.0.1`.
- Playwright CLI: `Version 1.52.0`.
- Terraform: `v1.9.8`.
- Packer: `v1.11.2`.
- mGBA: pinned `mgba-headless` build from commit
  `b19b557a78930ede7ee7f5dcbc880f9ff2533ffe` with `--script` support.
- ARM toolchain smoke: `arm-none-eabi-gcc` and `arm-none-eabi-as` present.
- Native Playwright smoke output: `native-bwrap-playwright-ok`.
- Readiness split smoke: `runtime_ready: true`, `repo_ready: false`, and
  `capacity_available: true` when `--repo-check 'exit 7'` fails.
- Broker smoke output: `DOCKER_HOST=unix:///tmp/devkit-native-broker.<id>.sock`,
  `redis_create_http=403`, and `native-broker-smoke-ok`.
- Managed broker lifecycle smoke output: `running: true` after
  `broker start`, `canonical-managed-broker-ok` from a native agent using the
  managed broker socket, and `running: false` after `broker stop`.
- Native capacity smoke output: `runtime_ready: 1`, `repo_ready: 0`, and
  `capacity_available: 1` when the repo check exits non-zero.

## Completed Parity

- `spago` and `netlify-cli` now come from lockfile-backed
  `nix/npm-tools/package-lock.json`; shell hooks do not install npm packages at
  runtime.
- Playwright uses Nix-provisioned `playwright-test` plus
  `playwright-driver.browsers`. Native bubblewrap can launch Chromium without
  an ad hoc browser install.
- Native plans keep `DirectDockerSocket=false`, set `DOCKER_HOST` to the broker
  socket, and do not bind `/var/run/docker.sock`.
- Native readiness now reports runtime readiness separately from repo readiness;
  capacity availability follows runtime readiness only.
- Native broker lifecycle, worktree fanout, and capacity reporting are available
  through the canonical `kit/scripts/devkit` entrypoint.
- The default `dev-all` lifecycle and simple agent entry commands now route to
  the native runtime. Compose remains available only through the explicit
  `compose` namespace for historical workflows and unsupported overlays.
- Native tmux/layout/session helpers use the same top-level `exec` path as
  manual attachment, so tmux windows inherit the native worktree, HOME/state,
  broker, and bubblewrap plan model.
- Native run/reset/bootstrap helpers use top-level native lifecycle commands;
  worktree branch/status/sync helpers operate on the configured native host
  worktree root instead of container indexes.
- Native check and hook helpers execute inside the same bubblewrap path as
  manual `exec`, and repo readiness diagnostics are listed in overlay config
  instead of being only hardcoded in legacy Compose readiness.

## Intentionally Dropped Parity

- `claude-code` is no longer a Nix runtime parity blocker. Nix shells and the
  static frontend Dockerfile do not install or validate the Claude CLI.

## Known Parity Gaps

- npm itself reports `10.8.2`; Docker updates npm to latest at image build time.
- Broker coverage is still a smoke, not full test-container lifecycle parity.
  The current evidence proves native agent reachability, direct socket absence,
  and policy rejection of a disallowed image through the broker.

## Host Capability Requirements

- Final broker smokes require a controlled host-side Docker daemon at
  `/var/run/docker.sock`. That socket is consumed by the broker process on the
  host and is not mounted into the native agent sandbox.
- Native bubblewrap execution requires `bwrap` and host Nix flakes support.
- The default broker endpoint remains
  `unix:///run/devkit/test-container-broker.sock`; tests can override it with
  `--broker-endpoint` for temporary broker instances.
- On this host, the current user cannot create `/run/devkit`, so lifecycle
  verification used a temporary socket override. Production use of the default
  path requires a host-created `/run/devkit` directory with suitable ownership.

## Native Runtime Boundary

`native exec` uses bubblewrap with a blank root, binds `/nix/store`,
`/nix/var/nix`, the dev workspace, per-agent HOME, managed resolver config, and
the optional broker socket. It sets `DOCKER_HOST` to the broker endpoint and
does not bind `/var/run/docker.sock`.

The command is intentionally additive. Existing Compose commands remain present
for currently running agents while the native control-plane replacement grows.
