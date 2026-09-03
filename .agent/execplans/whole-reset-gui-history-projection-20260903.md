# Whole-reset GUI history projection repair

## Objective

Make source-derived whole-prefix `dev-all reset` preserve GUI history whose
SQLite rollout reference names the isolated `/workspaces/dev` projection,
without moving whole-reset custody into a reset-owned lane. Publish the generic
Devkit repair only after the complete repository gate, then return deployment
and canary evidence to the fleet convergence owner.

Decision record:
[Whole-reset GUI history projection validation](../../docs/decision-framework/governance_decisions/20260903_whole_reset_gui_history_projection.md).

## Progress

- [x] Classified Derpinator operation `op-90da851317458d198ed7e8e90337658a`
  as a generic pre-apply projection-validation failure; no reset plan ran.
- [x] Rejected a draft that reused `WorkspaceRoot` and would have moved custody
  into the whole-reset ownership boundary.
- [x] Added independent source-derived projection geometry while retaining
  global whole-reset custody.
- [x] Added a lane-2 whole-reset regression and passed focused
  `codexhistory`/`nativecmd` tests.
- [x] Passed the complete Devkit gate and independent review; the packaged
  sqlite-equipped test executed the new regression.
- [x] Published the tested repair commit
  `27b0bc51d0ec2f6b54e58d4b7f8088d864a7b93a` by compare-and-swap from
  `7590178e8a1e5167d1a408f5ababb2f06752cc93`; the advertised remote `master`
  read back as the exact candidate.
- [ ] Hand the immutable revision to WSL-Nix for central deployment and one
  fresh Derpinator canary.

## Surprises & Discoveries

- Before this repair, `SnapshotOptions.WorkspaceRoot` controlled both
  projected-path validation and custody placement. Reusing it in whole reset
  would be unsafe: the reset planner owns each complete `agentN` directory, so
  lane-local custody would either block planning as a protected overlap or be
  deleted.
- Selected-slot reset already supplies the correct geometry. Whole-prefix and
  manifest-shrink capture had no equivalent projection-only input.
- The first packaged test run executed the new SQLite regression after ordinary
  host tests had skipped it and exposed a fixture-only missing private parent
  directory. Creating the same global state parent used by existing custody
  fixtures made the regression pass without changing production behavior.

## Decision Log

- Keep `WorkspaceRoot` as the selected-slot lane-local custody switch.
- Add a distinct projection-only root derived from the same immutable lane
  geometry. Require it to equal the host worktree parent and contain the host
  home before accepting the projected Codex path.
- Freeze all other station resets until the published repair passes on
  Derpinator with a fresh typed operation.

## Implementation and verification

The implementation is confined to `cli/devctl/internal/codexhistory` and the
centralized native history-capture wiring. The test ladder is:

1. focused `codexhistory` and `nativecmd` tests;
2. complete `go test -p 2 ./... -count=1` and focused `go vet`;
3. formatting and diff checks;
4. `nix flake check --no-build` and
   `nix --option max-jobs 2 --option cores 2 build --no-link
   .#checks.x86_64-linux.devctl-go-tests
   .#checks.x86_64-linux.dev-all-runtime-bundle
   .#checks.x86_64-linux.dev-all-runtime-tools
   .#checks.x86_64-linux.dev-all-runtime-shell`;
5. independent diff review, clean commit, compare-and-swap publication, and
   exact remote readback.

Fleet acceptance is deliberately outside this source checkout: WSL-Nix pins
the published revision, centralized deployment converges the controller and
Derpinator, and a fresh cold replacement plus fresh exact two-slot reset must
return terminal typed success before sequential lane reconstruction.

## Outcomes & Retrospective

Focused tests passed. `go test -p 2 ./... -count=1`, focused `go vet`,
`git diff --check`, and `nix flake check --no-build` exited zero. The first
packaged test derivation identified the fixture setup omission described above;
after that bounded correction, `devctl-go-tests` and all three runtime package
checks exited zero with two jobs and two cores. Independent review found no
blocking code issue after the fix.

The tested repair was published at
`27b0bc51d0ec2f6b54e58d4b7f8088d864a7b93a`, and `git ls-remote` proved
`refs/heads/master` advertised that exact commit. WSL-Nix pinning, centralized
deployment, and deployed Derpinator canary evidence remain pending.
