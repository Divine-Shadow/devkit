# Retired station broker with a missing PID file

[Agent instructions](../../AGENTS.md) · [Plan index](../PLANS.md) ·
[Native gates](../../kit/docs/native-operator-runbook.md)

## Objective and authority

Repair the existing fixed station broker lifecycle after process loss leaves
complete broker.json and a matching nonaccepting socket but no broker.pid.
Task 01a132cd-cef1-7143-aeda-5bb581aed22a owns source repair, independent
Sol/Ultra review and mandatory native CI/publication. Its explicitly commissioned
recursive bootstrap exception permits direct implementation here. Management
retains capacity, installation, runtime activation and original Source continuation.
No security, credential, configuration or live station mutation is authorized.

## Source custody

Normal supplied Git transport acquired a fresh isolated clone at
/workspaces/dev/devkit, on codex/devkit-broker-repair-01a132cd. Remote default
master is the accepted 539cc768d4405f75228737d514e9c2a72d66ef7d;
tree ac4c52bd55d5ec001a0291f9fd35b5c9dd9d47ee,
parent 241c2c5e0d5bafd7c6d2306a2e84cb49a042adeb. The existing host checkouts
reported by the caller were absent from this native task namespace and were not
edited. No .agents/skills exists in the fetched repository. Ordinary commands
and the repository's native-reset-test-config Nix build execute successfully.
The native task identifies devkit/workspace-egress/v4; security was unchanged.

## Decision

Keep Inspect strict. Reconcile at the existing Start/EnsureReady/Stop boundary
under its shared descriptor lock, only for a complete current fixed station
owner, absent PID pathname, no stop intent, proven retired recorded lifetime,
matching retained socket and ECONNREFUSED. Retain descriptors across the fixed
unit query and revalidate state digest, PID absence, stop intent, process and
socket before cleanup. Never synthesize PID metadata or signal a process.
An inactive/failed fixed unit must have MainPID zero. Its own forking ExecStart
may be activating only with the existing invocation/cgroup proof; an external
consumer cannot admit that phase. Existing interrupted typed stops keep their
exact persisted witness branch and deliberate-stop inhibition.

Alternatives rejected: treating all PID mismatch as stale; adopting a live
process from JSON alone; hard-coded incident witnesses; force flags; live file
cleanup; a new capability or runtime. These weaken existing custody or broaden
authority. This is maintenance of the owning Go lifecycle, with no new surface.
Rollback: revert this bounded source diff if review/native gates reject it;
no installed-runtime rollback is needed because activation is outside scope.

## Milestones and progress

- [x] Acquire accepted baseline; read owner, lifecycle, process, socket, stop and
  test boundaries; verify actual ordinary native Nix fixture build.
- [x] Implement guarded missing-PID reconciliation and focused fixtures.
- [x] Focused recovery/adverse/mutation cases pass after two retained fixture
  failures (syntax, then missing absolute test tool paths).
- [x] Complete focused broker regressions and baseline regression comparison.
- [ ] Freeze integrated inventory/diff and obtain independent Sol/Ultra review;
  correct findings and refresh exact-subject review when necessary.
- [ ] Complete native repository gates, immutable package and NixOS lifecycle
  check serially with bounded resources; preserve every failure.
- [ ] Publish through the supported native gate, read back exact commit/tree,
  and deliver original logs, review and derivation evidence.

## Verification and evidence

The new Go fixtures use a real owned child PID/start tuple and socket inode,
then kill only that test child and remove only its fixture PID file. They prove
inactive/failed recovery and effect-free dry runs; refuse active/reused/unknown
processes, owner/unit/policy mismatches, malformed/unsafe state, accepting or
foreign sockets, deliberate/in-progress stops and changes during unit query.
The existing NixOS shared-lifetime check now exercises the packaged lifecycle
and real fixed systemd unit for acquisition, the unit's own start, and typed
stop after private VM process loss. Existing listener-peer/readiness, six-way
reuse, sibling isolation, retained descriptor, legacy stop and retry regressions
remain required.

Original command logs are retained under .git/repair-evidence in the isolated
source lease; they are evidence, not product source or permission to activate.
Only this task produces source changes/build requests. The initial native
process snapshot showed no competing build producer; the existing Java process
was the supplied governance service. Subsequent heavy gates must remain serial.

## Outcomes and current frontier

The v1 seven-file tree a6f436b0b665f2a5f772631da5c01b218ce2cf49 passed
focused/full broker tests, vet and the baseline regression comparison. Both
independent reviews found no blocking source defect. Native validation-only
submit exec-submit-01a132e4-4bd passed its Git/sensitive/applicability scope,
without Go/Nix commands, commits or pushes. Native make ci-cheap failed on
Forbidden dependency egress; full nix flake check failed before its VM because
the builder lacks KVM. Candidate Devctl and portable Go-test outputs were
realized, but real missing-PID unit acquisition/start remains untested.

Management explicitly commissioned the cheapest supported real-VM portability
assessment and narrow source correction. The pinned driver supports TCG;
the [portability decision](../../docs/decision-framework/governance_decisions/20261013_native_broker_vm_acceleration_portability.md)
removes only this fixture's hardware-acceleration scheduling prerequisite,
retaining the full real-unit script, guest, type/lint checks and other features.
Original v1 tree/diff/reviews/failures remain in .git/repair-evidence. Successor
8092ed2757f06c3c8125d7b558ee00efb1b3aebb received two static integrated PASS
reviews, consumed by Management. Its real native VM booted under TCG and the
fixed broker unit started functionally, but the gate failed in the unchanged
six-way acquisition fixture before the recovery loop. The fixture captured and
hid the child's stderr. The next candidate preserves all six callers and the
12-second subprocess deadline, and asserts their zero exits explicitly so
completed nonzero returns include captured stdout/stderr. No deadline or
lifecycle assertion is weakened. Diagnostic successor
1e8807fe1c844e1c3984d2c494f0032531468878 found one success and five context
deadline failures before the recovery loop, with normal VM/VLAN cleanup.
The next fixture allocates two guest CPUs within the existing task bound,
retaining 1024 MiB and all deadlines/assertions; native build --cores 2 alone
does not configure the guest. The two-vCPU successor
d74e1c9ef078b2a53d502d34c5983e9a8f1b0770 failed with four successful cold
callers and two lifecycle-lock deadline errors; VM/VLAN cleanup completed.
Source diagnosis identifies serialized real observer work under the exclusive
guard and the fixture's omitted Config.StartTimeout selecting the five-second
default. Calibrate only the test helper to 40s lock/config, 75s caller and 80s
subprocess bounds, derived from six unchanged observer slots plus real unit
work as recorded in the linked decision. Keep all six cold callers, zero-exit
and identity assertions, production defaults and stop-race budgets unchanged.
Next: freeze/review this timing successor and rerun native CI serially.
Publication still requires passing full gates and
Management's review consumption. No installed result or Source WSL PASS is claimed.

v6 tree 4a36eaf4c00cfabb681f6c78ca6b8e68860221f6 received both static
integrated PASS reviews, consumed by Management, and passed all 14 x86_64
native flake checks. Its real TCG VM completed the full script in 170.58s and
cleaned up normally. That result uses the calibrated fixture envelope; the
production five-second concurrent envelope under TCG remains unproved. Caller
75s/query45s/wrapper80s are intended budgets, not universal helper-first or
end-to-end guarantees; queries have a separate background context.

Source CI uses the existing Go-test closure, immutable hash-pinned module
supply and offline Go. Retained failures exposed inherited GOROOT, trimpath,
long temporary-path and ambient SSH test interference; command-local fixture
normalization changes no persistent environment or security policy. Actual
integration then found the baseline test builder missing its existing third
SSH linker input. An untouched accepted archive reproduced that precise
failure. The [source CI fixture decision](../../docs/decision-framework/governance_decisions/20261013_native_broker_source_ci_fixture_binding.md)
supplies only the existing source policy to that test builder, preserving all
production SSH authority and negative tests. Next: focused consequences, new
integrated review, complete source/flake gates and ordinary native publication.

v7 a10cf5197ff2aa7a22e92632ab68c8de9f0360b0 received both integrated static
PASS reviews, consumed by Management. Its source gate still failed six legacy
CLI fixtures at missing fixed-owner admission and reset's broker-deletion
expectation. The production admission/teardown blobs match the accepted
baseline. Exact pidfd cleanup reaped its two recorded private fixture children;
live broker and Agent1 proxy were untouched. No native effect is inferred from
a test command merely returning terminal status.

The [native fixture migration](../../docs/decision-framework/governance_decisions/20261013_native_cli_station_fixture_migration.md)
maps all six existing external CLI consequences into a required variant of
the real NixOS fixture. It preserves relative target paths, wrong-host-key,
delayed-pack, exit/stdout, reciprocal Git and reset/history assertions, while
proving positive shared-owner preservation and a functioning sibling. Only
those explicit checkout invocations require the isolated fixture; the mapped
native gate requires every named PASS without SKIP. The original lifetime VM
remains intact. Next: freeze, review, execute meaningful native consequences and
complete whole checkout/native publication gates for the new subject.

v8 002bb114a57acbfbaab01cc2792f6b46def8e68d passed additional independent
static review. Its focused native check passed the first three consequences,
then rejected selected-lane's SKIP: system-profile Git was outside the test's
store-only namespace. The guest command completed within its 900s bound and
the VM/VLAN cleaned up. The successor supplies immutable package bin paths
without changing the restricted bind, emits scenario start/terminal/original
output, and makes the mapped Make gate fail immediately on upstream errors.
No original failure or missing consequence becomes PASS by that correction.
The v9 source-only evaluation succeeded, but targeted scope inspection found
shlex imported in the other helper instead of the consequence script. Corrected
before any v9 VM dispatch; no native execution or PASS is attributed to v9.
