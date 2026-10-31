# Registered sibling Source preparation

## problem

Source preparation already preserves an existing GUI linked writer or standalone
GUI child clone. A registered Source may instead be a sibling of the fixed GUI
base. Topology inspection supplies its workspace id/root but does not establish
the actual owning consumer binding. Broadening a workspace-prefix check would
discard the existing geometry and ownership guarantees.

## options

Relocate the active writer, admit arbitrary workspace roots, parse a caller-named
private registry, or consume a narrow projection from the existing registration
owner while retaining Devkit's acquisition and transaction checks.

## selection_rationale

Choose the existing owner's read-only registration projection. Product exposes a
package-owned no-argument executable through its existing CLI/artifact boundary.
External Nix composition binds it with Devctl's withCurrentSourceRegistration;
Devkit chooses no Product revision and accepts no runtime producer override.

The versioned object contains workspaceId, canonical worktree/base/home,
targetId/agentIndex, codexHome, canonical Git/common directories and a stable
registrationSha256 fingerprint. The fingerprint covers actual registration and
consumer binding, excluding mutable Git HEAD/index/tracking state. It is a
consistency fingerprint, not a signature or a new authority grant. All fields are
required; unknown/duplicate/null fields, trailing data and oversized output refuse.

Only an exact sibling of the declared GUI base gains this additional admission
form. Require actual consumer/root/Git metadata equality, retain directory identity,
and requery the owning registration while Git's existing no-deref tracking-ref
transaction holds its lock. A changed binding aborts before commit. Existing
lane-common ownership markers, actual Git worktree registration, declared origin,
transport, non-rewind, CAS, symbolic-ref and unknown-commit laws remain intact.
The registered transaction includes the existing metadata budget plus the bounded
lookup budget. Locked metadata reads and the lookup inherit that transaction's
context; a late successful response after cancellation cannot authorize commit.

## safety_checks

The projection performs no registration, allocation, construction, fetch or SDK
work. Capture is bounded to 32768 stdout bytes and 20 seconds of execution through
the existing managed process-group utility; stderr is discarded. Lookup failure,
output overflow or failed group/I/O settlement supplies no binding. No caller
root/registry file/command/source selector or environment override is introduced.

Preserve HEAD/index, candidate bytes/modes, FETCH_HEAD, other refs/worktrees,
homes, history, signatures and registered roots. No consumer restart, reset,
cleanup, Git integration or approval follows from source preparation.

## verification

Real-Git tests cover standalone and lane-linked registered siblings with existing
candidate/ref preservation assertions, unbound/foreign binding refusal,
noncanonical metadata, replaced directory identity and changed registration
under the actual ref lock. Aborted transactions release locks without manual
removal. Process tests cover exact cwd/no arguments, immutable supplier selection,
bounded output, nonzero/malformed output and cancellation. Retain the existing
acquisition adverse-case tests, independent review and complete normal Devkit CI.
An actual slow-valid lookup exceeding the old metadata-only deadline must retain
the live ref lock and commit normally; enclosing cancellation must reject success.

The owning Product producer and actual packaged Darksteel consumer must be
qualified before claiming receiving. Active WSL composition and lock publication
precede Runtime's existing deployment/preparation route and Product policy/SDK
reevaluation. Source test success alone accepts none of those later transitions.

## rollback_plan

Publish a reviewed source revert through the normal source and composition paths.
Do not rewrite already prepared consumer refs or repair them locally.

## decision_scope

Existing Source1 source-preparation assignment. Source29 owns Product Main/policy
projection, Runtime owns effects and Source17 owns its SDK leaves. The coordinator
composes the existing producers; no new allocator, service, issuer or permission
consultation is needed.
