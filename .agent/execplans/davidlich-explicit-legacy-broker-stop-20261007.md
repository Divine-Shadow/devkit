# Explicit retirement of the retained Davidlich broker

## Purpose
Restore the existing supported broker stop/start transition for the exact legacy
producer retained by Davidlich. The current owner service is inactive; old state
lacks its process and socket tuple. Ordinary readiness must continue to refuse
that producer and may not automatically retire it.

## Progress
- Actual configured-SSH read identified the retained PID and socket peer, old
  immutable executable and unchanged legacy state/PID/socket witnesses.
- Implemented a source-selected compatibility branch only in explicit Stop.
- Owned real pidfd/live-listener, interrupted retry, replacement refusal and graceful closed-listener retirement tests passed; broker vet passed. Full immutable x86 source/package gates and final sealed independent review pending.
- Runtime effects remain held for Parent/Runtime shared-consumer coordination.

## Decisions
The composition's existing fixed legacy witness admits only exact metadata,
policy and socket. The transition captures and revalidates the live lifetime
under existing lock and descriptor custody, signals only one retained pidfd, and
removes exact retained metadata/socket only after kernel-confirmed retirement.
It preserves the deliberate stop intent. An explicit supported broker start must
follow successful stop; ordinary EnsureReady cannot clear the inhibition.
No new CLI process/path/policy/credential authority or automatic startup behavior.
The optional refusal-diagnostic candidate is separate and not inherited.

## Validation
Exercise a private owned live listener and real pidfd termination; retain owned
child cleanup. Refuse changed metadata, policy, executable, socket witness/peer
and missing witness without disturbing their producers. Verify dry run and
ordinary readiness have no effects, preserve explicit stop intent, then run all
applicable x86 source checks and the selected immutable package.

## Remaining boundary
The owning installed Fleet wrapper must invoke only the existing exact selected
broker stop/start argv for Davidlich under its lifecycle lease and return terminal
receipts. Parent coordinates every broker-dependent consumer before stop.
This source work does not establish live convergence, authenticated completion,
actual catalog availability or the retained lineage's actual CI submission.

Focused terminal receipt: /tmp/fleet-refresh-task19-davidlich-explicit-legacy-stop-20261007-focused-v5.receipt.json, SHA256 2a3f27b7392ce136b6e6a348d3678c8c179d4aea97735506be8d72b6f677ad1b.
