# Management Controller Profile v8

## Purpose

Keep Devkit on the same strict controller capability family as the installed
Fleet client. A fresh Management consumer must decode and validate the v8
manifest and v8 live identity directly; no v7 projection or unknown-field
tolerance is accepted.

## Progress

- [x] Fast-forwarded the canonical Devkit root to current `origin/master` at
  `4affd8dd1b2ac2853cc9f0d67a643d15aa907be0`.
- [x] Added strict v8 manifest and identity shapes for the typed Product
  station-reset capability.
- [x] Passed focused plan/launch tests, the complete Go suite, `go vet`, and all
  12 Devkit flake checks.
- [ ] Publish Devkit and pin its immutable revision into WSL/Nix.
- [ ] Prove the source-derived WSL/Nix consumer and controller convergence
  checks against the unchanged v8 Management contract.

## Decisions

- The profile manifest and live identity advance together to v8. Maintaining a
  v7 compatibility projection would add a second contract and contradict the
  single-route convergence objective.
- Devkit and Fleet consume the same v8 document at
  `/etc/fleet/controller-operation-authority.json`; the retired profile-path
  alias is removed rather than maintained.
- Devkit validates only the declared fixed reset socket, immutable store
  executable, typed operation and schemas, service identity, and exact
  ownership/mode fields. It receives no new effect or caller-selected command
  surface.

## Verification

Run focused tests for `internal/runtime/plan` and `internal/runtime/launch`, then
`nix flake check --no-write-lock-file -L`. The downstream WSL/Nix check must
build both `management-controller-devkit-profile-consumer` and
`management-controller-convergence` from one pinned source family.

## Outcomes

Devkit now consumes the v8 profile and live identity directly and strictly
validates the isolated Product station-reset capability. Publication and
downstream WSL/Nix proof remain.
