# Broker repair source CI fixture binding

Framework: [Tradeoff Decision Framework](../../../../ouroboros-ide/documentation/engineering/architecture/tradeoff_decision_framework.md).
[Agent instructions](../../../AGENTS.md) · [Repair plan](../../../.agent/execplans/missing-pid-station-reconciliation-20261013.md) ·
[VM fixture decision](20261013_native_broker_vm_acceleration_portability.md)

Decision type: tooling remediation. Risk: low. Owner: repair task
01a132cd-cef1-7143-aeda-5bb581aed22a. Mandatory native CI remains required
under the commissioned recursive-bootstrap implementation exception.

## problem

The real source-checkout gate reaches integration tests after using the
repository's declared Go-test environment and unchanged fixed-hash modules.
Its test binary builder supplies the SSH executable and host keys, but omits
packageManagedGitConfig required by the accepted transport publication.
An untouched archive of baseline 539cc768d4405f75228737d514e9c2a72d66ef7d
reproduced TestFlakeBackedLifecycleUsesReadinessDefaultDryRun failing before
its intended action with "package-owned managed Git SSH config must be absolute".
The test-builder file was byte-identical to the baseline. Production flake.nix
already injects all three inputs; no production authentication defect is shown.

## options

1. Waive integration or weaken Package's required managed configuration.
2. Supply the same existing source-owned input in the test binary builder.
3. Substitute an alternate runtime/binary or change live credentials/policy.

## selection_rationale

Choose 2: resolve the existing repository nix/managed-git-ssh-config as an
absolute test-build input and add its existing linker binding alongside the
fixture executable and host keys. This matches flake.nix without changing
production code, SSH policy bytes, host-key/credential authority or installed
configuration. Keep the real negative host-key, delayed-pack, selected-lane
and reset tests active. Options 1 and 3 violate the mandatory gate or authority.

Correctness and verifiability rank above speed: preserve the original baseline
failure and rerun the meaningful integration/full repository gate. Operational
safety keeps this exclusively in the existing test builder and isolated source
lease. No credentials or security setting are acquired, edited or enrolled.

## safety_checks

Verify the diff changes only the test builder and linked decision/plan. Run
the previously failing case and the complete integration/reset consequence
suite with real Nix SQLite/Git/OpenSSH inputs; preserve every failure. Obtain
fresh independent integrated review and full native CI for the successor tree.
The previous v6 static/flake PASS remains scoped to its own tree.

## rollback_plan

Revert the test-builder binding if it changes host-key outcomes or source
scope. Return the baseline failure and exact CI prerequisite, never waive it.
No installed rollback is needed; runtime activation is outside this task.

## decision_scope

One pre-existing integration binary builder plus this linked decision/plan.
No production SSH, credentials, host keys, permissions, transport policy,
SDK, package recipe, live broker, Source gate or fleet-effect changes.
