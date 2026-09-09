# Selected-slot physical process custody

## Purpose and outcome

Restore the existing native Devkit selected-slot disposal path when a process's historical logical HOME names the exact selected leased home, while preventing identical logical paths in other namespaces from acquiring or blocking unrelated custody. Work is source maintenance under controller task 01a08222-a6da-70e3-b718-1bd720e10851; the controller retains publication, deployment and fresh business acceptance. See the [decision](../../docs/decision-framework/governance_decisions/20260909_native_slot_physical_process_custody.md).

## Progress

- [x] Read repository AGENTS and ExecPlan contracts and inspected current origin/master 9089bb4783e20a8df293719ce9a1d8ef2919b204.
- [x] Recorded the decision before implementation in an isolated /tmp worktree.
- [x] Implement physical home/worktree ownership and physical touch observation.
- [x] Add meaningful ownership, ambiguity, descendant and disposal regression coverage.
- [x] Focused native tests, complete nativecmd tests, vet, CLI build and independent review passed.
- [ ] Full source gate acceptance: the complete suite ran and exposed two unchanged integration fixture/manifest mismatches and one unchanged 120 ms idle-timeout test failure; controller disposition and immutable Nix gate remain pending.
- [x] Transfer the reviewed source as a bounded commit under root direction; publication/deployment remain controller-owned and await the canonical Nix gate.

## Implementation and acceptance

Change `cli/devctl/internal/commands/nativecmd/native_slot_reset.go` and focused package helpers/tests. Exact agent marker is necessary; HOME is resolved through `/proc/PID/root`, and the candidate worktree follows selected host home/worktree relative geometry. Physical object equality binds custody. Descendants inherit only from proven selected ownership and cannot override explicit foreign/conflicting identity. Touch classification must distinguish physically different namespaces without suppressing actual foreign touches. Existing stable start-time revalidation, stop/absence/snapshot/absence/apply ordering and whole-prefix detection-only behavior remain mandatory.

Run focused nativecmd tests, complete `go test -count=1 ./...`, `go vet ./...` and the canonical Makefile build using a /tmp output override. Use /tmp caches and logs. Release owner runs the required immutable Nix gate before publication/deployment. No native Devkit execution, live process changes or failed-computation salvage is part of this source task.

## Surprises & Discoveries

The existing classifier uses lexical HOME and proc-link path spelling. The bounded controller witness instead proves exact physical selected home/worktree custody under an older logical alias. No bridge-specific exception is needed if the generic physical identity correction covers that observation. Identical logical paths in unrelated namespaces must not count as a selected touch.

## Decision Log

2026-09-09: Select physical custody correction in the existing selected-slot maintenance boundary. Do not broaden whole-prefix reset or convert the explicitly synthetic Product VM harness. Preserve strict foreign refusal and the history/deletion gates.

## Outcomes & Retrospective

Implemented the existing classifier without launcher or whole-reset production changes. Canonical HOME is a lookup hint; exact agent plus SameFile home/worktree proofs seed ownership. Descendants re-prove inherited lookup geometry and cannot override contradictory physical mappings. Physical touches remain separate from ownership. Directory ancestry holds an atomically opened O_DIRECTORY descriptor, preventing descriptor churn or FIFO substitution from hanging observation. The existing PID/start and history/deletion boundaries remain intact.

Independent reviewer `/root/task_context/transient_git_test_review` returned PASS on helper SHA256 `2b5bcf9c8482a1f8ff3c5c11ea5ecf84a4cafe9adb6728e8dce116754c9fcda0`, custody tests `39229a22c31abbd6a42af394987d69c4065db7deebbfa5baae1b51d6590e029d` and reset `6c335f211c03400d01971c2ada812f40eb91b3bc277dc371c33ee011a1de9445` against base 9089bb. Review fixes cover sanitized conflicting descendants, noncanonical HOME, reversed mixed custody, and atomic directory open. No review finding remains.

Final validation receipt: `/tmp/slot-process-physical-custody-final-validation-20260909.json`, terminal exec session 10020. `go test -count=1 ./...` exited 1; nativecmd passed (33.667 s). The two integration failures are `TestDevAllFreshOpenAndResetUseNativeLifecycleDryRun` and `TestDevAllResetReconstructsThreeSlotsThroughPackageSSHAuthority`: unchanged runtime plan admission finds zero inventory targets for their temporary geometry before the modified process planner. `TestRunManagedAllowsActiveCommandBeyondIdleWindow` hit its unchanged 120 ms idle deadline while expecting output every 80 ms. These observations are not a full-suite pass or proof that the published base reproduces them. `go vet ./...` and the canonical Makefile-equivalent CLI build both exited 0. Logs are `/tmp/slot-process-physical-custody-final-{full-go,vet,build}-20260909.log`.

Validation uses declared CGO_ENABLED=0 (flake.nix84/121, CLI Makefile) and the declared Git/SSH/SQLite/tool prerequisites. GOFLAGS=-buildvcs=false affects validation build metadata only: Go 1.26 vcs.go218 recognizes only a `.git` directory, skips this linked worktree's `.git` file, and selects unrelated `/tmp/.git`; the initial ambient run was invalid. The Nix package and test derivations use flake source snapshots (flake.nix79/117) without Git metadata. No assertion, source build flag, runtime artifact identity or unrelated `/tmp/.git` was changed. The authoritative source-derived Nix/package and WSL release gates remain controller-owned and required.

No runtime or business recovery is claimed by this source candidate.

## Root gate disposition

2026-09-09: Root accepted the bounded source classification and authorized a commit only, not publication. The ambient full-suite result remains failed and unaccepted. Do not replay the three tests against a base checkout or rerun the ambient suite. Root will run the full existing Devkit flake check against this exact commit from an immutable Git snapshot. The existing `mkDevctlGoTests` definition (flake.nix112-145) covers the complete portable Go surface and explicitly separates integration/config source-checkout prerequisites; no checker definition, source flag, test assertion or fixture was changed for gate acceptance. Publication waits for the root-owned canonical Nix result.
