# Workspace Root Alias Governance Pin - 2026-07-01

## Purpose

Refresh the devkit `dev-all` governance runtime pin so fleet deployment selects
the Ouroboros governance jar that canonicalizes equivalent
`/home/bayesartre/dev/...` runtime worktree roots to `/workspaces/dev/...`.

## Acceptance

- `flake.nix` pins `governanceJarVersion` to published Ouroboros commit
  `b3f081db035cfab4a3ee699a353d5f0e35e8ff62`, which contains
  `6585311faf37a767c1e6c4e01c288daac72746e3` and builds the `governance-jar`
  package.
- `nix build .#pinned-governance-jar` succeeds from this devkit worktree.
- The built jar's `Main$` bytecode shows `canonicalWorkspaceRootString` maps
  through the fleet alias canonicalization helper rather than only normalizing
  the input path.
- `devkit native governance-env` can regenerate the shared Ouro governance env
  and repo-config without invoking full native prepare or mutating per-agent
  homes, `.codex`, shell snapshots, or live app-server processes.
- Deployment remains separate: live app-server/governance restart requires the
  promoted fleet deploy lane and operator authority for disruptive restart.

## Progress

- 2026-07-01: Created clean devkit worktree
  `/workspaces/dev/agent-worktrees/devkit-workspace-root-alias-pin-20260701/devkit`
  on `codex/workspace-root-alias-governance-pin-20260701`.
- 2026-07-01: Updated `governanceJarVersion` to the published Ouroboros
  canonicalization commit.
- 2026-07-01: Nix build of the alias-only commit failed because the selected
  source did not yet include the SBT `devopsArtifactTrustServices` build edge.
  A follow-up Ouroboros repair, `b3f081db035cfab4a3ee699a353d5f0e35e8ff62`,
  includes the alias commit and repairs the Nix `governance-jar` source filter,
  so the pin was advanced there.
- 2026-07-01: `nix build .#pinned-governance-jar --no-warn-dirty` passed and
  produced `/nix/store/qpy95b39qm44vwbj24rs1znwviwn2za8-subagent-governance-dev`.
- 2026-07-01: Added `native governance-env --repo ouroboros-ide` as an
  env-only regeneration command. It calls the existing Ouro governance env
  generator directly and avoids the broader `native prepare` path that also
  writes per-agent home and Codex state.

## Decision Log

- Use a clean devkit worktree because the canonical `/workspaces/dev/devkit`
  checkout already contains unrelated Codex-version changes. Mixing this pin
  refresh into that dirty checkout would obscure custody and weaken convergence
  evidence.
- Expose a source-owned env-only command rather than telling operators or
  agents to run full `native prepare` for a governance jar/env refresh. That
  preserves the fleet invariant while avoiding unrelated `.codex` and
  per-agent-home mutations.

## Verification

- `nix --extra-experimental-features 'nix-command flakes' build .#pinned-governance-jar --no-warn-dirty --print-out-paths`
  passed from the clean devkit worktree.
- The built jar hash is
  `a434749108d8a4e04c9e7922d5a05d9a32d5252e592401c31d0d4676bf78ade0`.
- `javap` on the built `Main$` class shows `canonicalWorkspaceRootString`
  invoking `canonicalFleetDevRootAlias`, with constants `/home/bayesartre/dev`
  and `/workspaces/dev`.
- `go test ./internal/commands/nativecmd ./internal/runtime/launch` passed from
  `cli/devctl`.
- `make build` passed from `cli/devctl` and produced the updated devctl binary.
- `DEVKIT_ROOT=/workspaces/dev/devkit .../kit/bin/devctl -p dev-all native governance-env --repo ouroboros-ide --dry-run --format json`
  reported planned writes only to `/workspaces/dev/.devkit/ouro8-governance-env.sh`
  and `/workspaces/dev/.devkit/ouro8-governance-repo-env.json`.

## Outcomes & Retrospective

- Source pin and env-only regeneration tooling are ready for integration into
  the canonical devkit checkout and later fleet deployment. Live app-server
  convergence remains a separate, operator-authorized action through
  `bin/fleet governance deploy-jar`.
