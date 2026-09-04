# EMDR Owner Preparation Controller Envelope

## Objective

Expose the already-defined WSL/Nix EMDR owner preparation operations to the
owning isolated Management controller without widening Management's lifecycle
broker. The repaired controller must select fictional source metadata, create
one prepared fictional job, and then return immediately to the public EMDR
lifecycle and strict Codex Desktop workflow.

## Constraints

- Preserve `management-controller-convergence/v1`,
  `devkit/workspace-egress/v4`, `NoNewPrivileges=true`, and the existing
  controller-operation broker contract.
- Add a separately named `emdr-owner-preparation/v1` capability with only
  `sources`, `jobs`, `status`, and `job-create`. Reject `promote`, `assemble`,
  arbitrary commands, caller paths, credentials, and content-bearing output.
- Mount only a source-declared socket and identity file into the Management
  controller. Do not expose the setuid wrapper or a host directory.
- Use current published Devkit and WSL/Nix source, one active writer lease, the
  complete relevant source gates, ordinary Git publication, centralized
  source-selected Colmena deployment, and fresh controller reconstruction.
- Use fictional material only. Do not record source/job/candidate contents or
  protected payloads in logs, prompts, receipts, or this plan.

## Progress

- [x] Verified the deployed public lifecycle manifest is available and the
  fixed lease is `absent` with fresh typed receipts.
- [x] Proved the active profile projects only the controller-operation socket
  and identity, while `NoNewPrivileges=true` makes direct wrapper projection
  unusable.
- [x] Proved current Management source intentionally excludes owner operations
  and that no existing governed Scala EMDR owner app exists.
- [x] Add and test the exact Devkit capability manifest, socket, identity, env,
  bind, and hostile-geometry refusals.
- [x] Repair the newly reproduced hermetic Devkit gate omission by supplying
  `setsid` from `util-linux` to the existing Go test derivation.
- [ ] Add and test the WSL/Nix preparation-only client/service, immutable
  capability manifest and identity, profile wiring, and content-free receipts.
- [ ] Publish Devkit, repin WSL/Nix, pass focused checks and the complete WSL
  flake gate, publish WSL/Nix, and deploy through the typed Fleet/Colmena path.
- [ ] Reconstruct this controller with task/goal continuity and prove the named
  capability readback.
- [ ] Use the capability to create one fictional job, summon it, complete the
  strict Desktop candidate task, finalize, and prove terminal absence.

## Decision Log

- 2026-09-04: Keep Management Go unchanged. A Go owner admission would violate
  its lifecycle-only exception; a new Scala column would make Product/CI an
  unrelated dependency. Compose a separately named WSL-owned capability into
  the existing controller envelope instead.
- 2026-09-04: Keep mount-policy identity v4 and the main controller-operation
  v8 manifest unchanged. The new capability has its own strict manifest and
  runtime identity, following the existing sidecar-capability pattern and
  avoiding false ownership by the lifecycle broker.
- 2026-09-04: The complete Devkit flake gate proved that four existing `execx`
  tests use `setsid`, while the hermetic check closure omitted `util-linux`.
  Adding that one native check input is the smallest owning-source repair and
  changes no runtime closure.

## Verification

- Devkit focused Go tests for controller plan/launch profile geometry and
  refusal cases.
- WSL/Nix focused derivation/VM checks for the preparation service and a real
  Devkit bwrap consumer.
- Complete Devkit and WSL/Nix repo-declared gates required by the touched
  source.
- Live source/closure/profile readback, typed deployment receipt, fresh
  controller capability readback, then direct workbench outcome evidence.

## Rollback

If the capability schema, socket identity, output projection, or confinement
cannot be proven, do not deploy it. After deployment, any mismatch requires the
canonical NixOS rollback to the recorded previous closure and source repair;
never expose the raw wrapper, broaden the lifecycle broker, or use a host shell.

## Outcomes & Retrospective

Pending implementation and live workbench acceptance.
