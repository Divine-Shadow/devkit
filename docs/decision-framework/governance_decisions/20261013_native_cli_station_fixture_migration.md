# Native CLI consequence fixture migration

Framework: [Tradeoff Decision Framework](../../../../ouroboros-ide/documentation/engineering/architecture/tradeoff_decision_framework.md).
[Agent instructions](../../../AGENTS.md) · [Repair plan](../../../.agent/execplans/missing-pid-station-reconciliation-20261013.md) ·
[SSH fixture binding](20261013_native_broker_source_ci_fixture_binding.md) ·
[VM acceleration and timing](20261013_native_broker_vm_acceleration_portability.md)

Decision type: tooling remediation. Risk: medium. Owner: supplier repair task
01a132cd-cef1-7143-aeda-5bb581aed22a under Management's explicit migration
direction. The source repair bootstrap exception applies; native gates remain
mandatory. Source installation and original WSL continuation remain elsewhere.

## problem

v7's real checkout gate advanced past the inherited SSH input defect, then
failed six legacy CLI consequences without the immutable station-owner supply.
The accepted baseline and v7 have identical endpoint admission and target
teardown code. Temporary copied broker executables cannot supply the fixed
owner's immutable geometry, observer and real user-manager cgroup. Reset's
broker-deletion assertion also contradicts existing target-only teardown.
Two private failed-test children survived old cleanup; exact pidfd lifetime
cleanup completed with recorded PID/start/root/UID/binary identities, without
touching the live station or Agent1 proxy. Original failures remain evidence.

## options

1. Bypass missing-owner admission, fabricate ownership, or skip all consequences.
2. Run the consequences against the existing real owner composition in a new
   private-VM check, preserving all external CLI and negative assertions.
3. Require the failed live supplier or mutate its state to supply checkout tests.

## selection_rationale

Choose 2. Extend the existing source-owned broker NixOS fixture with a separate
CLI-consequence variant. It supplies the same fixed service, packaged broker,
immutable owner/observer inputs and real isolated user manager. The original
shared-lifetime fixture and its six cold callers, recovery and adverse checks
retain their existing configuration and script. The consequence variant uses
an alias-free station declaration outside target temporary roots; target paths
and relative metadata remain unchanged. Source-owned project fixture builders
select its complete broker policy rather than manufacturing owner state.

The Nix-built test runner executes these existing external CLI consequences:

| Existing test | Required native consequence |
| --- | --- |
| TestNativeTopLevelExecAndAttachPreserveSandboxExitCode | exec and attach retain sandbox exit 7 |
| TestNativeTopLevelExecProjectsStdoutAndCleansProxyOnEveryExit | stdout plus proxy cleanup for exits 0 and 7 |
| TestNativeTopLevelPrepareAndExecUseIsolatedRelativeMetadata | preparation/exec retain isolated relative target metadata |
| TestNativeTopLevelSelectedLaneGitReciprocity | real namespace selected-lane reciprocal Git custody |
| TestDevAllResetReconstructsThreeSlotsThroughPackageSSHAuthority | failed/successful reset, target/history custody and negative cases |
| TestNativePrepareCarriesDelayedPackThroughActualOpenSSHProxyCommand | actual OpenSSH/ProxyCommand delayed pack and wrong host key refusal |

Only those six source-only invocations report the explicit missing isolated
namespace; the required native check must execute every named test, return zero,
emit its exact PASS and contain no SKIP. make native-reset-custody-gate consumes
that check's original consequence log and still requires the exact reset PASS.
Full flake check includes both VM variants. No gate can pass by removing the
consumer assertion or by claiming an absent supplier is ready.

Actual v8 execution passed the first three consequences, then rejected a SKIP
in selected-lane reciprocity: the guest's system-profile Git path was absent
inside the deliberately store-only bind. Normal VM/VLAN cleanup completed.
Supply the declared tool packages' immutable bin paths before the convenience
system profile, preserving the existing restricted bind. Keep each scenario's
start/terminal marker and original output in the native build log, so buffered
source compilation is distinguishable from a completed or skipped assertion.
Make's mapped gate explicitly stops if evaluation/build fails.

The reset test keeps target-owned cleanup assertions and replaces global
broker-deletion expectations with positive same-owner metadata/socket and
functional admission checks. Every consequence checks the same PID/start
generation through retained state/PID bytes, socket inode and real observer/
listener admission. A real sibling endpoint remains ready after every test.

Correctness and verifiability outweigh speed. The extra real fixture can be
slower, but avoids fake ownership or live effects. Both VMs retain two vCPUs
and 1024 MiB, with native builds serialized at one job/two cores. No production
owner guard, process identity, permissions, credentials, host keys or API/image
policy is changed. The new capability is test supply composed in the existing
flake, not a production Go runtime selector or a second live broker.

## safety_checks

Freeze the integrated source; obtain fresh independent review of the mapping,
test supplier and unchanged production boundaries. Validate the native runner's
type/lint checks, execute all six with real inputs and no skips, then complete
the whole checkout and fifteen-check native gate. Preserve all original logs
and failures. Prove VM and target fixture cleanup before another producer.
This fixture does not establish installed consumer namespace or Source recovery.

## rollback_plan

Revert the fixture migration if any consequence loses its promised behavior
or its owner cannot be supplied honestly. Return the exact missing immutable
owner-plus-isolated-user-manager prerequisite and failed supported result.
Do not weaken admission or migrate effects to a live station.

## decision_scope

Existing CLI test fixture builders, one required Nix check variant, Make's
mapped reset gate and linked documentation only. No production lifecycle,
auth/security, vendor, SDK, package selection, Source history or runtime
activation changes. Final subject requires fresh review and complete gates.
