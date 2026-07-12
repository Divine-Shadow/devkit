# Native Overlay Lifecycle Classification

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose / Big Picture

The native runtime should have overlay-wide operator evidence, not only a
single canonical pair. The user-visible result is a local gate that classifies
each real overlay as `e2e-pass`, `runtime-pass`, or `not-locally-runnable`,
with concrete evidence for real `up`, `status --ready`, `exec`, safe piped
`attach`, `scale`, and `down` where the sibling repository exists locally.

## Progress

- [x] (2026-05-14T00:00:00Z) Accepted the overlay-wide lifecycle coverage goal.
- [x] (2026-05-14T00:05:00Z) Started read-only subagents for local overlay
  availability and script design.
- [x] (2026-05-14T00:08:00Z) Inspected overlay metadata and local sibling
  repositories. `dumb-onion-hax` is not checked out locally; `pokeemerald`
  exists but only has `origin/master`.
- [x] (2026-05-14T00:15:00Z) Added `kit/scripts/native-overlay-e2e-matrix` and
  `make native-overlay-e2e-matrix`, preserving the existing
  `native-e2e-lifecycle` gate.
- [x] (2026-05-14T00:16:00Z) Updated `pokeemerald` metadata to use
  `base_branch: master`, matching the local and remote repository branch.
- [x] (2026-05-14T17:03:15Z) Ran overlay-wide E2E classification and required
  verification commands.
- [x] (2026-05-14T17:03:15Z) Recorded observed evidence for the completed
  slice.

## Surprises & Discoveries

- `dumb-onion-hax` has a real overlay definition and its native runtime probe
  passes, but no local sibling checkout exists under `/home/bayesartre/dev`.
  It classifies as `runtime-pass` in this workspace because full worktree E2E
  is blocked by local checkout availability, not by the overlay runtime.
- `pokeemerald` was locally present but its overlay default base branch pointed
  at `main`; the repo only advertises `origin/master`, so real native worktree
  setup would fail until metadata matched the actual repo.
- `codex` uses the same sibling repo as `dev-all`; `dev-all` is the canonical
  `ouroboros-ide` full E2E target, so `codex` receives runtime coverage without
  duplicating the full lifecycle cycle.
- `ouro-integration` can start and execute through the native runtime, but full
  readiness is Terraform/SBT/AWS-scoped and belongs outside this lifecycle
  matrix. It classifies as `runtime-pass`.
- The Docker upstream socket was initially unavailable in the workspace. A
  Nix-provided `dockerd` later made `/var/run/docker.sock` available and the
  final lifecycle gates passed.

## Decision Log

- Decision: Add a separate overlay-wide E2E matrix target instead of changing
  `make native-e2e-lifecycle`.
  Rationale: The existing target is a stable focused gate for `dev-all` and a
  smaller front-end overlay. The new target is broader and local-workspace
  dependent, so it should be explicit.
  Date/Author: 2026-05-14 / Codex

- Decision: Keep this target outside cheap CI.
  Rationale: It requires sibling repository checkouts, Git worktree mutation,
  Nix sandbox launches, and real broker sockets.
  Date/Author: 2026-05-14 / Codex

- Decision: Classify overlays with passing runtime probes but intentionally
  skipped or workspace-blocked full E2E as `runtime-pass`.
  Rationale: This keeps the matrix honest about native runtime health while
  separating missing sibling checkouts and noncanonical full-lifecycle targets
  from actual overlay runtime failures.
  Date/Author: 2026-05-14 / Codex

## Validation and Acceptance

Acceptance requires:

- `make native-overlay-e2e-matrix` classifies `codex`, `dev-all`,
  `ouro-integration`, `ouroboros-static-front-end`, `ouroboros-terraform`,
  `pokeemerald`, and `dumb-onion-hax`.
- Locally present real overlays run real `up`, `status --ready`, `exec`, safe
  piped `attach`, `scale`, and `down`, or receive a clear non-runnable reason.
- `dumb-onion-hax` may classify as `runtime-pass` when the sibling repo is
  absent but the native runtime probe passes.
- Existing `make native-e2e-lifecycle` behavior remains intact.
- Required gates pass: `make ci-cheap`, `make native-e2e-lifecycle`,
  `make native-overlay-matrix`, `make native-overlay-e2e-matrix`, and
  `find overlays -maxdepth 2 -name flake.lock -print`.
- Overlay flakes remain lockless.
- `git diff --check` passes.

## Idempotence and Recovery

The matrix target creates temporary broker sockets, worktree roots, and agent
state roots under `/tmp/devkit-native-overlay-e2e.*`. It removes temporary Git
worktrees and unique test branches during cleanup while preserving small JSON
and text evidence files plus `report.tsv` under the evidence root.

## Artifacts and Notes

Final report:

```tsv
OVERLAY	REPO	FLAKE	CLASSIFICATION	REASON
codex	ouroboros-ide	./overlays/codex#default	runtime-pass	runtime up/status/exec/down passed; full e2e blocked: noncanonical overlay; dev-all is the canonical ouroboros-ide E2E target
dev-all	ouroboros-ide	./overlays/dev-all#default	e2e-pass	real up/status-ready/exec/attach/scale/down passed
dumb-onion-hax	dumb-onion-hax	./overlays/dumb-onion-hax#default	runtime-pass	runtime up/status/exec/down passed; full e2e blocked: missing sibling checkout at /home/bayesartre/dev/dumb-onion-hax
ouro-integration	ouroboros-ide	./overlays/ouro-integration#default	runtime-pass	runtime up/status/exec/down passed; full e2e blocked: noncanonical integration overlay; full readiness is Terraform/SBT/AWS-scoped and outside this lifecycle matrix
ouroboros-static-front-end	ouroboros-static-front-end	./overlays/ouroboros-static-front-end#default	e2e-pass	real up/status-ready/exec/attach/scale/down passed
ouroboros-terraform	ouroboros-terraform	./overlays/ouroboros-terraform#default	e2e-pass	real up/status-ready/exec/attach/scale/down passed
pokeemerald	pokeemerald	./overlays/pokeemerald#default	e2e-pass	real up/status-ready/exec/attach/scale/down passed
```

Evidence report:

- `/tmp/devkit-native-overlay-e2e.arAUlZ/report.tsv`

Verification commands:

- `nix --extra-experimental-features 'nix-command flakes' develop --command make ci-cheap`
- `nix --extra-experimental-features 'nix-command flakes' develop --command make native-overlay-matrix`
- `nix --extra-experimental-features 'nix-command flakes' develop --command make native-overlay-e2e-matrix`
- `nix --extra-experimental-features 'nix-command flakes' develop --command make native-e2e-lifecycle`
- `find overlays -maxdepth 2 -name flake.lock -print`
- `git diff --check`
- `bash -n kit/scripts/native-overlay-e2e-matrix kit/scripts/native-e2e-lifecycle kit/scripts/native-overlay-matrix`

All listed verification commands passed. The lockfile audit produced no output.
