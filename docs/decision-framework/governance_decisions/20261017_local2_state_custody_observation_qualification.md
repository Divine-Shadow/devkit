# Selected Local2 state-custody observation qualification

Status: qualified diagnostic repair boundary; implementation and installed observation pending.
Source authority: finite follow-on under Management 01a11f23-001e-7639-97f1-07f2cca9a030 and Source02 01a12d56-6e2a-7991-b33e-faab6f5580df. Runtime 01a12755-a379-7cb0-9d1f-b95c79cd21a1 retains every live effect and retry.
Decision type: tooling_remediation. Risk: bounded metadata disclosure and mistaken ownership inference. Phase: normal source repair, no exception or operator decision requested.

## problem

Actual operation `op-a29cb3270109075fa445c960dbe853f5` failed with the retained Devkit predicate: PID 2033854 home matches selected slot but state has conflicting physical custody. The exact branch in selected Devkit `27621160b217ae0bffad150427f141762f2821c1`, `cli/devctl/internal/commands/nativecmd/native_slot_process_custody.go`, `nativeProcHomeMatchesSlot`, proves that home and derived worktree both matched selected host objects and were directories. Selected host state existed; a successful stat of one alias in ordered `[sandboxState, stateRoot]` differed from host state via `os.SameFile`. This diagnostic does not identify which alias or objects, and does not establish that the process is foreign or the composition is correct.

The exact ten-member public terminal directory is `/nix/store/5slasxlvhiwi72rnza4lfmnz42z4c03j-PUBLIC_ACTUAL_TERMINAL_ORIGINALS`; all nine payload hashes were verified after strict member admission. The root-manifest SHA256 is `07b7dbdadfbb309d442d42d1658c4f22a17141a00378ac4514f998a03c61ec29`. Four relevant selected immutable source leaves equal the retained Devkit checkout.

Completed Product owner request `op-6c5e6c9ef61474bb64f9f47a831fb282` supplied PID/start/boot metadata, not the state comparison. Product observed start 20100371, boot `8582cb5f-ab03-4be1-84ca-55a65be679d7`, UID1000/GID100, PPID1; namespace/exe/cwd reads returned EACCES. This is observed visibility, not a new institutional prohibition or a reason to bypass a denial.

The actual normal published invocation was:

```text
/run/current-system/sw/bin/fleet app-rpc topology-read --target shadow-throne-local-2 --timeout 10
```

Operation `op-d0e083f8133967b9563e16412d723773` succeeded0. It reports selected app-server metadata, not arbitrary PID2033854 or state aliases. Its app-server identity was unstable; that is not evidence the different target PID is foreign or gone. Exact capability-gap original: `/nix/store/rviq1smjdvrrw0daq9kd1mjc32135izg-EXACT_PUBLISHED_OWNER_METADATA_CAPABILITY_GAP_20261017.json`, 4903 bytes, SHA256 `d5065c0c74031327108e2688de09aabd6363f2e728efcdf030c88a001efb7116`.

Source tracing confirms: `app_rpc_topology_observe.go` uses app-server-specific kernel inspection; `topology_process_identity.go` observes app-server identity/listeners, not selected Devkit state; `native plan` emits source-selected geometry without calling the custody predicate; history-only `native reset --preflight-only` returns before the custody scan; `controllerOperationPublicExecCommand` admits exact existing shapes, not an arbitrary stat/proc helper. No existing inspected callable supplies the missing comparison. A dry-run reset is not a read-only substitute because it may create managed egress transport.

## options

1. Infer a correct foreign-process refusal or repeat reset to capture more evidence: reject; neither resolves this source/composition question safely.
2. Broaden host access, raw IPC, caller-selected proc paths or an ad hoc helper: reject; use the normal owning observer.
3. Retain the selected predicate and project its bounded physical comparison through existing selected-target read-only metadata: select. This is narrow observation maintenance of the existing guard/metadata path, with no arbitrary-PID query, new lifecycle command or ownership authority. If implementation would instead add a durable new fleet capability, route that adapter through a governed Scala artifact column and flake app; do not expand Go authority.

## selection_rationale

Correctness: observe the actual comparison without changing its acceptance. Verifiability: share its facts/decision rather than implement a second classifier. Safety: read-only source-selected geometry, no reset/proxy/lock/history action. Epistemic integrity: same-name paths do not prove same objects, a guard refusal does not prove semantic correctness, and current observation cannot recover old lost evidence. Delivery speed follows these constraints; there is no outstanding owner request to wait on.

## finite necessary source delta

Devkit owns the facts and decision in `native_slot_process_custody.go` and its use in `native_slot_reset.go`. Factor a bounded typed observation from the existing home/worktree/state comparison, keeping the reset's exact predicate, refusal, process acquisition and destructive ordering unchanged. Capture the selected and process-view objects from that same observation; do not reconstruct a diagnostic later from reread path spellings. Reuse those facts for a read-only selected-slot observation under existing `native plan` in `native.go`; the proposed internal `--process-custody-observation` option selects this receipt, with exact `--gui-target-id`, index and immutable source-derived geometry. This flag does not exist in installed a17. The exact proposed internal argv is `-p dev-all native plan --repo ouroboros-ide --index 2 --gui-target-id shadow-throne-local-2 --process-custody-observation --format json`; the package executable is resolved only from the selected immutable manifest. It must return before launcher/proxy/source acquisition, reset coordination, history capture or mutation. No caller-selected PID, root, home, state, flake or executable may expand the observer's boundary.

Management's existing `app-rpc topology-read` for the inventory-selected local Product target supplies the public route. `app_rpc_topology_observe.go` and its existing command/receipt validation invoke the exact package-selected Devctl read-only plan observation for that target, and return its typed custody witness beside existing app-server facts. Local2 is selected from manifest/inventory, not an arbitrary socket/path override. The public invocation above remains the intended shape after publication; the added field is NOT supplied by the currently installed a17 implementation. Preserve existing target/transport resolution and incomplete-observation behavior. No new Go command or general PID/host-reader admission is justified.

The receipt is bounded to the first relevant custody refusal (or explicit complete/absent/unstable/unavailable/truncated outcome), not a host process dump. It must bind captured PID, parent, start ticks, boot identity, observer/target PID and mount namespace identities, and relevant UID metadata; identify alias kind `sandboxState` or `stateRoot`; and include source-selected expected state and observed alias object device/inode/type/UID metadata, plus home/worktree agreement. Public path fields may contain only the existing source-declared geometry. Never output argv, environment values, descriptor names outside the selected boundary, private history bodies or secrets. Existing internal agent/HOME classification reads gain no general export authority.

Pin process/root/namespace/object observations as appropriate and revalidate captured identity and mappings before marking stable. A changed/reused/exited/unreadable process or object returns explicit uncertainty, never absence or ownership. The Runtime consumer must compare the actual witness with PID2033854/start20100371/boot8582cb5f-ab03-4be1-84ca-55a65be679d7; if that identity no longer exists, the old mismatch remains unknown. Observation grants no signal, stop, reset or release authority. It cannot authorize a new reset attempt by itself.

## safety_checks

Meaningful fixtures must cover coherent selected mappings, both distinct mismatching aliases, absent aliases, a foreign same-spelling backing, home/worktree disagreement, PID reuse/parent/start/boot drift, namespace/root/object changes, unreadable state, bounded/truncated metadata and secret non-disclosure. Verify predicate outcomes remain identical and no read-only path reaches process stop, egress creation, source acquisition, reset lock/mutation or history capture. Keep real PID/pidfd/UID/sibling/release/race/cold-history safeguards and all genuine ownership decisions intact.

Actual prerequisites are attributable clean source writer(s), normal independent review, complete declared Devkit Go/vet/Nix package gates and Management Fleet checks for the adapter, then canonical source publication/readback. Pin only changed producers in WSL; run actual required flake/package gates and real direct/declared-Colmena build-only closure equality, supply full public originals, and let Runtime perform its separately owned canonical selection/adoption/metadata observation. No fleet-wide rollout, blanket image gate, SDK recertification or protected-station restart is prerequisite. A new Scala semantic capability, if actually necessary, requires its normal governed writer/artifact registration/review/full Product CI and publication; a documentation packet cannot substitute for that implementation.

## rollback_plan

Before publication retain or discard the isolated diagnostic candidate normally. After publication any metadata leak or semantic change requires reverting the diagnostic source through the same publication/supply route. Runtime owns any actual rollback. Never repair the failed consumer in place or transplant state to make the predicate pass.

## decision_scope

This qualifies the concrete missing observation and owning source/review/CI route; it does not claim the diagnostic extension is implemented, tested, published or installed. No heavy build or live process action was performed for this qualification. Accepted a17 diagnostic supply/receiving remains settled. Actual a29cb cause awaits the bounded witness; old `op-11b9a5c6f87fde8dc685fecf2c880166` cause/progress remains independently unknown. Reconstruction, SDK/R4 and Writer acceptance stay with their owners.
