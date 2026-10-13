# Native broker VM acceleration portability

Framework: [Tradeoff Decision Framework](../../../../ouroboros-ide/documentation/engineering/architecture/tradeoff_decision_framework.md).
[Agent instructions](../../../AGENTS.md) · [Repair plan](../../../.agent/execplans/missing-pid-station-reconciliation-20261013.md)

Decision type: tooling remediation. Risk: medium. Owner: repair task
01a132cd-cef1-7143-aeda-5bb581aed22a under Management's explicit portability
commission. No host, daemon, runtime, security or publication authority changes.

## problem

The full native flake gate for frozen source tree a6f436b0b665f2a5f772631da5c01b218ce2cf49
refused the real broker VM before execution: required {kvm, nixos-test}, supplied
{benchmark, big-parallel, nixos-test}. The declared builder list is empty;
/dev/kvm is absent in the task. Actual broker acquisition, own-unit startup and
stop recovery must still execute against real Linux/systemd/process/socket state.

## options

1. Use an already declared KVM-capable builder. None is declared here.
2. Let this source-owned fixture use the pinned driver's existing QEMU software
   acceleration support, retaining the identical guest and complete script.
3. Advertise unproved KVM, change host settings, skip the VM or replace it with
   mocks. These violate the commission and are rejected.

## selection_rationale

Choose 2. Pinned nixpkgs at /nix/store/3p306srz83h9z9v0ma9xcxb8y8cdxkxj-source
generates x86_64 Linux QEMU with `-machine accel=kvm:tcg -cpu max` in
nixos/lib/qemu-common.nix. Its nixos/lib/testing/run.nix unconditionally adds
the KVM scheduler feature on Linux; this is not a fixture assertion. The exact
qemu-host-cpu-only-for-vm-tests-9.2.4 package lists both tcg and kvm through its
supported `-accel help` interface. This fixture tests real lifecycle identities,
systemd, Linux pidfds/cgroups, Unix peer/readiness and custody, never KVM itself.

Select this fixture's public `test` package option with the same pinned
noninteractive driver invocation and normal output/log setup. The pinned
`runNixOSTest` result is a lazy package and does not expose overrideAttrs; the
initial evaluation refusal is retained. Remove only the acceleration scheduling
prerequisite from this source-derived test derivation. Keep `nixos-test` and the
pinned non-Linux `apple-virt` requirement where applicable,
the pinned guest/runner, all assertions, type/lint checks and bounded deadline.
Correctness and verifiability are preserved by running the same real guest;
operational safety keeps isolation and resource bounds; evidence still records
the original failure and actual resulting execution. Speed ranks last: software
acceleration can be slower and may fail the normal deadlines.

Actual v3 TCG execution booted the guest and started a functionally ready broker,
then failed the six-way cold acquisition check. v4 preserves its six callers,
12-second subprocess limits and zero-exit assertions while exposing captured
errors: one succeeded, five returned context deadline exceeded. The pinned VM
default is one vCPU; the native build's --cores 2 does not allocate guest CPUs.
Select two guest CPUs, matching the task's existing two-CPU bound, and keep
memory at 1024 MiB. This source-owned fixture resource correction preserves
every caller, admission rule and deadline. It does not change host/capacity
settings, increase the task bound, or make a slow result acceptable. The same
full real-unit gate must pass; original failures remain retained.

v5 with two vCPUs produced four successful admissions and two lifecycle-lock
deadline errors, then normal VM/VLAN cleanup. The broker became ready at guest
83.611s; observer launches at 84.642, 86.893, 87.747 and 88.555s show real
serialized admission work, not a binding or readiness refusal. inspectLocked
holds the shared exclusive guard across managedProcess's fixed systemd-run
observation and listener ping. The fixture omitted Config.StartTimeout and so
gave each queued caller the production five-second lock-wait default.

Calibrate only the test executable's requested Config.StartTimeout to 40s:
six serialized observation slots, each bounded by the unchanged five-second
observer, one-second reap delay and two 100ms socket operations, sum to 37.2s.
Its caller context is 75s for that queue plus the unchanged 15s real-unit start
limit and final observation, with a bounded scheduling margin. The Python
wrapper is 80s so it exposes the helper's own result rather than terminating it
first. The same helper configuration applies to cold and retained-owner tests;
there is no warming, retry or selected successful caller. Config's shared field
also bounds that fixture's fixed job/query at 45s; the real unit remains at 15s.
The production default, packaged unit, observer and listener timeouts are
unchanged, as are the two-second stop-race refusal and twelve-second stop
completion checks. This is fixture configuration calibration, not acceptance
of the original five-second concurrent-caller envelope under TCG.

## safety_checks

Inspect the evaluated successor's required features and generated QEMU command.
Do not inject a kvm feature, change daemon config, select an undeclared builder,
disable type/lint checking or weaken admission assertions. Keep production and
stop-race deadlines intact; retain all prior fixture failures. Obtain independent
integrated review of the changed tree, then run the full native flake gate with
one job and two cores. A timeout or real-unit failure remains a failure to correct
or return, never an excuse to report PASS. Complete source-checkout gates and
normal publication separately; the existing dependency-egress refusal is not
resolved by this scheduling correction.

## rollback_plan

Revert the fixture-only scheduling diff if review finds a hardware-acceleration
dependency or the pinned runner cannot execute the full contract. Return the
original KVM requirement and exact supplier prerequisite. No installed rollback
is needed; the task performs no runtime selection or activation.

## decision_scope

One existing broker VM fixture and its linked repair plan/decision record only.
Original tree, review and native failure stay retained as v1 evidence. A new
integrated source tree needs its own review and complete native CI. No changes to
Go broker semantics, vendor, credentials, builder/security configuration, Source
history, original WSL gate or fleet effects are included.
