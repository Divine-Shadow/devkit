# Devkit Nix Runtime Shells

This directory tracks the Nix-first replacement surface for long-lived devkit
agent containers. The root `flake.nix` exposes development shells that mirror
the current Dockerfile families closely enough for migration smoke testing.

Current shell targets:

- `template-agent`: baseline shell for new overlays.
- `ouroboros-dev-agent`: Nix shell for the external Ouroboros Codex agent image.
- `dev-all`: alias for `ouroboros-dev-agent`.
- `codex`: alias for `ouroboros-dev-agent`.
- `ouroboros-terraform`: Ouroboros agent plus Terraform and Packer.
- `ouro-integration`: alias for `ouroboros-terraform`.
- `pokeemerald`: Ouroboros agent plus ARM embedded toolchain.
- `ouroboros-static-front-end`: Node/static frontend shell.
- `runtime-test-agent`: lightweight integration-test shell.
- `tinyproxy`: host-service shell for proxy experiments.

Use explicit experimental feature flags on hosts that do not enable flakes by
default:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
nix --extra-experimental-features 'nix-command flakes' develop .#dev-all --command bash -lc 'git --version && sbt --version'
```

Every shell conversion must be verified against
`kit/docs/proposals/nix-runtime-verification-contract.md`.

Current smoke evidence and known parity gaps are tracked in
`nix/runtime-parity.md`.
