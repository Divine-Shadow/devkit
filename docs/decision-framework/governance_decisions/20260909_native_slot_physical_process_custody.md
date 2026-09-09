# Native selected-slot physical process custody

Status: Implemented and independently reviewed; full gate acceptance and release pending
Date: 2026-09-09
Decision type: tooling_remediation
Pressure: high
Risk: medium
Phase: explicitly authorized centralized source repair; fresh execution acceptance remains pending
Source authority: operator-directed disposable execution recovery, delegated by controller task 01a08222-a6da-70e3-b718-1bd720e10851 under work lease golden-ci-runtime-activation-20260908.
Framework: [canonical Tradeoff Decision Framework](https://github.com/Divine-Shadow/dev/blob/main/docs/decision-framework/tradeoff_decision_framework.md).

## problem

The controller Product reset operation op-ae9104c1c09f37ce853cd3fb10d538fc refused an unowned process touching a leased slot. One bounded controller observation proved its two existing launcher/bridge processes had the exact agent marker and historical logical HOME `/workspaces/dev/ouroboros-ide/.devhome-agent1`. Through each process root, that home and its worktree were the same filesystem objects as `/home/bayesartre/dev/agent-worktrees/agent1/ouroboros-ide/.devhome-agent1` and its parent worktree (device 2096, inodes 7512988 and 7512987). The logical spelling differs from the current plan but the leased physical objects agree. Conversely, identical logical paths can name different objects in independent namespaces. Lexical matching is neither sufficient ownership proof nor reliable touch classification.

## options

1. Add bridge-specific argv exceptions or broaden lexical HOME aliases. Reject: they do not establish physical custody and risk sibling/operator acquisition.
2. Make whole-prefix reset stop live processes. Reject: it changes its explicit detection-only contract.
3. Correct the existing selected-slot process classifier to resolve the exact agent's HOME and derived worktree through its process root and compare them with selected host objects. Classify touches through physical object identity and keep unknown/conflicting actual touches closed. Select this existing disposal maintenance boundary.

## selection_rationale

Correctness: the exact agent claim plus physical home/worktree geometry establishes the same existing slot authority; no new public operation, launch behavior, Product semantics, provider, or installed-state exception is introduced. Verifiability: isolated proc fixtures cover coherent historical aliases, identical spelling with different backing, foreign actual touches, descendant conflicts, PID reuse and disposal ordering. Safety: preserve stable start-time checks and both absence checks around the mandatory history snapshot before deletion. Epistemic integrity: failed computation supplies only the causal witness, never business acceptance. Delivery speed follows these invariants; the controller retains publication, centralized deployment and fresh reconstruction.

## safety_checks

Use HOME as a lookup hint, never an ownership allowlist. Derive the candidate worktree using the relative geometry between the source-selected host home and worktree. Require both physical objects to match. A same agent marker with conflicting home/worktree/state cannot gain authority through descendant closure. A foreign process physically touching the selected leased objects remains a refusal. Missing required physical evidence fails closed when ownership or a selected touch is implicated. Whole-prefix reset remains detection-only. Run focused ownership/boundary tests, complete Devkit Go tests, vet and the canonical CLI build with outputs outside the worktree; retain repository-wide Nix gate as required by release owner. Obtain independent review before commit/publication.

## rollback_plan

Before publication discard this bounded source candidate. After centralized activation, an ownership overreach or false absence proof triggers rollback through the prior accepted immutable closure. Never repair the failed runtime into acceptance; reconstruct fresh disposable execution after an accepted source release.

## decision_scope

Only Devkit native selected-slot process observation/classification and meaningful regression fixtures, with this decision and linked ExecPlan. No launcher edits, no whole-prefix live-process acquisition, no host PID effects from the source work, and no Product VM/business mode changes.

## validation_outcome

The bounded source implementation and physical-inode regression fixtures are complete. Independent review passed after strengthening canonical HOME handling, conflicting descendants, reversed mixed geometry, and atomic directory observation. Nativecmd tests, vet and canonical CLI build passed. The complete suite ran with all assertions retained but did not pass: two unchanged integration fixtures cannot match their temporary geometry to the installed package-owned GUI projection manifest, and an unchanged timing-sensitive execx test exceeded its 120 ms idle window. Exact terminal receipt and metadata-only validation environment are recorded in the linked ExecPlan. No unchanged failure was bypassed or promoted to acceptance. Root retains gate disposition, immutable Nix validation, publication, deployment and fresh reconstruction.
