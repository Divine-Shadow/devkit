# Native exec owns its command lifetime

## Purpose and contract

Repair ordinary package-owned Devkit execution after the Source03 evidence showed
that native exec directly called cmd.Run without the existing managed lifecycle.
Source1 is the one Devkit/source/Nix writer. Runtime owns adoption and actual
consumer effects; Main owns its separate Product fixture. No change can assign a
control handle or terminal result to old Source03.

Keep the existing ordinary exec command, source-selected bubblewrap and security
boundary. Reuse execx managed process-group supervision for a prepared command
without losing directory, environment, stdin, stdout or stderr. Preserve payload
exit codes. Provide an optional native execution deadline so callers can select
an owned command budget inside their transport wait. Keep systemd-owned managed
app-server execution on its separate existing lifecycle and reject that deadline
there. Bound cleanup; distinguish cleanup failure from a completed cancellation.

A managed group is the stated resource. A transport timeout or unobserved process
death never proves remote cleanup. This repair supplies new owning executions;
it introduces no general remote job API, process selector, credential, profile
override or retroactive legacy operation binding.

## Progress

- [x] Fetched canonical master af5ee86 and prepared a clean registered exclusive
  Source1 worktree with writable Git metadata.
- [x] Consumed complete original Source03 and frozen source evidence 111; recorded
  natural outer client timeout separately in 115.
- [x] Run a causal regression against direct cmd.Run before implementation:
  redirected descendant survived the old return; offline test failed as expected.
- [x] Reuse the managed runner for prepared native commands; add owned deadline.
- [x] Focused cancellation/deadline, exit/I/O, cleanup-failure, parser, PTY and
  sibling controls passed. Nested Popen-like waiter and cooperative forwarding
  wrapper reaped their separate cold group. A TERM-resistant redirected cold
  group is a negative control: it survives outside managed ownership, and the
  test disposes its own fixture. No whole-tree cleanup claim is made.
- [x] Independent first review found shared output writers lost serialization.
  A simultaneous stdout/stderr regression failed; a shared pipe fixed it. The
  complete focused controls passed again after the correction.
- [x] Complete normal Go route passed at 13:17:27 UTC, including integration
  and all package suites. Final same-reviewer verdict APPROVED for this bounded
  group repair; no remaining actionable source findings.
- [ ] Complete normal native/Nix qualification and source publication.
- [ ] Independent review, complete normal source gates, native source publication,
  immutable package qualification and Runtime handoff.

## Implementation and verification

Owning files are cli/devctl/internal/execx/run.go and the existing
commands/nativecmd entry/parser and execution tests. Add a small prepared-command
adapter to the existing managed runner rather than reconstructing its semantics.
The native wrapper uses the actual prepared source-selected command, and its
error implements the already-supported ExitCode interface. Keep the ordinary
unbounded default and systemd-managed app-server behavior explicit.

First regression: a command leader exits after creating a redirected descendant;
ordinary execution must settle the invocation group before returning. Additional
controls cover an independently launched sibling group, exact payload exit and
streams, a native deadline and source-owned cancellation. A failed cleanup must
remain non-success. Parser controls reject invalid/duplicate deadlines and
managed-app-server/attach misuse. Tests target only fresh owned fixtures.

Keep the existing group contract precise: an escaped group with redirected I/O
can outlive a killed forwarding wrapper. Neither exit 124/130 nor closed output
pipes proves that separate group was reaped. Generic descendant tracking,
subreaping, a new PID namespace or cgroup ownership is not supplied by this
existing primitive. The negative control records this limitation explicitly.

Run focused Go tests in the admitted Devkit runtime, then applicable complete
Go/vet, source native consequences and repository Nix gates through the normal
source publication path. Preserve original failed evidence and change only a
named causal input. Source1 does not signal Main03 or run a competing Main04.

## Surprises & Discoveries

The generic execx runner already has group termination, wait and I/O cleanup,
but ordinary native exec bypasses it. Its current constructor recreates commands;
a prepared-command entry is needed to preserve actual native directory and
environment. Main's outer 15190 natural timeout is settled only for its clients.

## Decision Log

Use existing group ownership and an optional in-process deadline within the
existing command. A new daemon, control socket, PID endpoint or role issuer is
outside this bounded repair. See the accompanying six-section decision note.

## Outcomes & Retrospective

Implementation and focused controls are complete; full acceptance is pending.
Evidence is under the Source1 task-output directory
native-exec-owned-lifecycle-source-001, including original red-offline, the
shared-writer red regression and review-controls-003 terminal0. The old Source03 disposition,
originating Fleet F1 assessment admission gap, C69 failure and qualified
Writer/F1 supply retain their separate evidence and owners.
