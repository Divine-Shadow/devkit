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
- `devctl native plan`: inspectable native plan output.
- `devctl native exec`: prepares per-agent state and runs a flake shell through
  bubblewrap.

## Verified Commands

All commands require explicit flakes on this host:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
nix --extra-experimental-features 'nix-command flakes' build --no-link --print-out-paths .#postgres-broker
nix --extra-experimental-features 'nix-command flakes' develop .#runtime-test-agent --command bash -lc 'git --version && ssh -V && curl --version | head -1'
nix --extra-experimental-features 'nix-command flakes' develop .#template-agent --command bash -lc 'git --version && uv --version && python3 --version'
nix --extra-experimental-features 'nix-command flakes' develop .#dev-all --command bash -lc 'codex --version && docker --version && go version'
nix --extra-experimental-features 'nix-command flakes' develop .#dev-all --command bash -lc 'mgba-headless --help 2>&1 | grep -q -- --script'
nix --extra-experimental-features 'nix-command flakes' develop .#ouroboros-terraform --command bash -lc 'terraform version | head -2 && packer version'
nix --extra-experimental-features 'nix-command flakes' develop .#pokeemerald --command bash -lc 'arm-none-eabi-gcc --version | head -1 && arm-none-eabi-as --version | head -1'
nix --extra-experimental-features 'nix-command flakes' develop .#ouroboros-static-front-end --command bash -lc 'node --version && npm --version && spago --version && netlify --version && codex --version && claude --version'
nix --extra-experimental-features 'nix-command flakes' develop .#tinyproxy --command bash -lc 'tinyproxy -h 2>&1 | head -5; uv --version'
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 go test ./...
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit go run . -p dev-all native exec --repo devkit --flake .#runtime-test-agent -- git --version
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 DEVKIT_ROOT=/home/bayesartre/dev/devkit go run . -p dev-all native exec --repo devkit --flake .#dev-all -- bash -lc 'codex --version && docker --version && go version && mgba-headless --help 2>&1 | grep -q -- --script'
```

Observed key versions:

- Codex CLI: `codex-cli 0.130.0`.
- Docker CLI: `Docker version 27.5.1`.
- Go: `go1.22.4`.
- Terraform: `v1.9.8`.
- Packer: `v1.11.2`.
- mGBA: pinned `mgba-headless` build from commit
  `b19b557a78930ede7ee7f5dcbc880f9ff2533ffe` with `--script` support.
- ARM toolchain smoke: `arm-none-eabi-gcc` and `arm-none-eabi-as` present.

## Known Parity Gaps

- `spago`: Nix shell currently provides `0.21.0`; Dockerfiles request npm
  package `spago@0.93.45`.
- `netlify-cli`: Nix shell currently provides `19.0.2`; Dockerfile requests
  `netlify-cli@26.0.1`.
- `claude-code`: Nix shell currently provides `1.0.85`; Docker installs the npm
  package without a deterministic version pin.
- npm itself reports `10.8.2`; Docker updates npm to latest at image build time.
- Browser dependency parity is package-level only so far. Playwright is present,
  but browser install/runtime checks still need a repo-specific smoke.
- Broker reachability is planned and environment-wired through
  `DOCKER_HOST=unix:///run/devkit/test-container-broker.sock`; an actual broker
  request smoke is still pending.

## Native Runtime Boundary

`native exec` uses bubblewrap with a blank root, binds `/nix/store`,
`/nix/var/nix`, the dev workspace, per-agent HOME, managed resolver config, and
the optional broker socket. It sets `DOCKER_HOST` to the broker endpoint and
does not bind `/var/run/docker.sock`.

The command is intentionally additive. Existing Compose commands remain present
for currently running agents while the native control-plane replacement grows.
