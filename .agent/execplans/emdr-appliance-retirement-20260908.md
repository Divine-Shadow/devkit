# Retire the EMDR appliance dependency

## Purpose and acceptance

The operator commissioned a persistent EMDR workspace and retired the disposable
job appliance on 2026-09-08. Ordinary Management controllers must start using
their existing source-derived profile without any EMDR preparation manifest,
socket, identity, or environment handle. Preserve all generic source, mount,
operation, auth, history, and deployment validation.

## Progress

- [x] Start from clean current origin/master f6d00ca5857b68fb9feeb23914d4b8220ac2f430.
- [x] Remove EMDR-only capability loaders, binds, launch validation and fixtures.
- [x] Remove the old active preparation plan and clarify protected user-workspace data.
- [x] Focused controller plan/launch tests passed; all 12 native-system flake checks passed (exit 0).
- [ ] Publish current-source candidate; WSL consumes its exact revision.

## Decisions

The prior source is recoverable from Git history; it grants no forward authority.
No live EMDR data, credentials, state or processes are touched by this change.
Existing general controller tests now exercise a controller with no EMDR fixture,
which verifies the removed prerequisite rather than constructing a substitute.
Persistent operator workspaces must not inherit disposable Product lease policy.

## Surprises and discoveries

The old local Devkit checkout was stale. Current origin/master has an EMDR
capability requirement in both planning and launch validation. Both consumers
must change before WSL removes the capability.

## Outcomes and retrospective

The ordinary controller plan and launch tests pass without an EMDR fixture.
The complete 12-check native-system flake gate passed. Publication and WSL
consumption are separate remaining effects; no live cleanup is claimed here.
