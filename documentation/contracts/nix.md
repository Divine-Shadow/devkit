# Nix Runtime Autonomy Contract

Status: Draft
Owner: BayeSartre

## Purpose

This contract authorizes autonomous implementation work for the next Nix-native
runtime slice. The goal is to close the remaining tool/runtime gaps identified
in `nix/runtime-parity.md` while preserving the direction established by
`kit/docs/proposals/nix-sandbox-agent-runtime-plan.md`: native agents are
Nix-provisioned sandboxes, and OCI access stays behind a broker.

This is an implementation contract, not a new architecture proposal. When this
document conflicts with the verification rules in
`kit/docs/proposals/nix-runtime-verification-contract.md`, the verification
contract wins.

## Authorized Outcomes

### 1. Claude Is Out Of Parity Scope

Claude is not a blocker for Nix runtime parity.

- Remove Claude from must-match retired image parity and smoke requirements.
- Keep shells functional without requiring `claude-code` from nixpkgs.
- Do not spend implementation time pinning, packaging, wrapping, or validating
  Claude unless explicitly reauthorized.

Acceptance evidence:

- `nix/runtime-parity.md` no longer lists Claude as an unresolved parity gap.
- Smoke commands do not rely on `claude --version`.

### 2. Spago And Netlify Match Retired Image Pins

The static frontend shell must provide deterministic versions matching the
retired image intent:

- `spago@0.93.45`
- `netlify-cli@26.0.1`

Implementation constraints:

- Prefer fixed-output or lockfile-backed Nix packaging.
- No `npm install`, `npx`, curl installer, or registry fetch in shell hooks.
- Do not downgrade Node below the Node 20 line inherited from the retired image
  contract.

Acceptance evidence:

```bash
nix --extra-experimental-features 'nix-command flakes' develop .#ouroboros-static-front-end --command bash -lc 'spago --version && netlify --version'
```

The output must report Spago `0.93.45` and Netlify CLI `26.0.1`.

### 3. Brokered OCI Access Is Required

Native agents must access OCI test dependencies only through a broker endpoint.

Implementation constraints:

- Standard native agents must never bind `/var/run/docker.sock`.
- `DOCKER_HOST` must point to the broker socket, not the host daemon socket.
- It is acceptable to extend or rename the current Postgres broker toward a
  test-container broker if that is the smallest honest path.
- Broker tests may use a controlled host-side container daemon, but that
  daemon access must stay outside the native agent sandbox.

Acceptance evidence:

- A broker smoke proves a native agent can reach the broker endpoint.
- A negative check proves `/var/run/docker.sock` is absent from the native
  sandbox.
- `nix/runtime-parity.md` records the broker command, output, and remaining
  broker limitations.

### 4. Playwright Works In The Native Runtime

The Nix/native runtime must support browser automation without ad hoc browser
installation during agent startup.

Implementation constraints:

- Browser and driver dependencies must be provisioned by Nix or a deterministic
  checked-in artifact.
- Do not broaden into application-specific E2E coverage unless a generic smoke
  cannot prove browser runtime capability.
- Keep the smoke independent of `ouroboros-ide` business behavior.

Acceptance evidence:

- A native bubblewrap shell runs a minimal Chromium Playwright script.
- The command and output are recorded in `nix/runtime-parity.md`.

### 5. Readiness Is Split Into Runtime And Repo Readiness

Readiness must stop treating repository warm-up as the same thing as agent
capacity.

Runtime readiness means:

- The sandbox launches.
- Per-agent HOME and state directories exist.
- DNS/proxy environment is present.
- Broker socket policy is correct.
- A shell command can execute in the target worktree.
- `/var/run/docker.sock` is not exposed.

Repo readiness means:

- Warm hooks.
- Package installs.
- Compiles and typechecks.
- Playwright app tests.
- SBT, npm, or project-specific checks.

Implementation constraints:

- Capacity restoration may depend on runtime readiness only.
- Repo readiness failures must be visible and retryable.
- Repo readiness failures must not mark an otherwise launchable native agent as
  unavailable.

Acceptance evidence:

- Native readiness tests or command output distinguish runtime-ready from
  repo-ready.
- At least one test or smoke proves a repo-readiness failure does not hide
  runtime readiness.

## Allowed Write Scope

Autonomous edits may touch:

- `flake.nix`, `flake.lock`, and files under `nix/`
- `documentation/contracts/nix.md`
- `kit/docs/proposals/*` when keeping Nix runtime docs aligned
- `cli/devctl/internal/runtime/*`
- narrowly scoped files under `cli/devctl/internal/commands/*`
- broker code, docs, and tests under `brokers/*`
- focused runtime test fixtures required by this contract

## Changes Requiring Reconfirmation

Do not do these without explicit user approval:

- Remove retired runtime command namespaces.
- Rewrite tmux/layout wholesale before native readiness is proven.
- Grant native agents direct container daemon socket access.
- Introduce shell-hook network installs.
- Rework unrelated overlays.
- Revert or rewrite user-created commits or unrelated files.

## Required Verification Before Claiming Done

Run and record all applicable evidence:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 go test ./...
nix --extra-experimental-features 'nix-command flakes' develop .#ouroboros-static-front-end --command bash -lc 'spago --version && netlify --version'
```

Also record:

- Native `dev-all` bubblewrap smoke.
- Playwright Chromium smoke inside native runtime.
- Broker access smoke.
- Negative evidence that `/var/run/docker.sock` is not exposed.
- Updated `nix/runtime-parity.md` with remaining gaps.

Passing `nix flake check` alone is not enough. Each acceptance gate above needs
direct evidence.

## Parallel Work Boundaries

Use independent workers only with non-overlapping write scopes:

- Worker A: Spago and Netlify packaging only.
- Worker B: broker smoke and no-socket enforcement only.
- Worker C: Playwright native runtime smoke only.
- Worker D: readiness model, tests, and command surface only.
- Main integrator: reconcile outputs, run final verification, update evidence,
  and commit.

Workers must not edit the same files concurrently unless the main integrator
assigns a replacement write scope.

## Completion Rule

This contract is complete only when every authorized outcome has concrete
evidence in the repository and `nix/runtime-parity.md` accurately distinguishes:

- completed parity,
- intentionally dropped parity,
- remaining runtime gaps,
- and any host capability requirements.
