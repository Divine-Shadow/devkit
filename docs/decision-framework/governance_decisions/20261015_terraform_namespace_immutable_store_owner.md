# Terraform immutable ownership in the checker namespace

Decision type: bounded correctness/tooling remediation. Phase: normal Source repair; no bootstrap or security-policy exception.

## problem

The actual published915 checker in Runtime operation `op-d8d25629f74dfccc86292df964313b8a` refused its immutable source. In that namespace EUID1000/EGID100 maps to outer root, while both the source and read-only `/nix/store` appear UID/GID65534. The regular source is0444; the mutable projection is UID1000/GID100 and0600. These are actual checker observations, not host stat. Later digest/profile checks were not reached. Full original Runtime evidence remains separately supplied; the preserved generic failed operation and capture error are not retrospective successes.

## options

1. Keep literal root UID0: rejects legitimate source under this namespace.
2. Add a literal65534 exception: conflates an overflow representation with authority and would admit foreign ownership without proving its anchor.
3. Compare foreign immutable-source ownership to the fixed read-only store directory as observed through admitted descriptors; retain all existing independent digest/type/no-follow/byte/profile and mutable-owner checks.

## selection_rationale

Select option3. Correctness comes from the existing exact source selection plus a namespace-relative immutable authority anchor, rather than a guessed host UID. Verifiability comes from executing the real parsed overlay predicate/batch/report with disclosed descriptor-metadata controls and mutation/counterfactual cases; those controls are not genuine kernel namespace proof. Operational safety retains nonzero refusals, provider/Auth/config boundaries and the normal immutable publication/adoption route. Epistemic integrity keeps source tests, actual captured namespace and later installed acceptance distinct. Speed follows those obligations. Existing EUID-owned source admission is unchanged; only the old literal-root alternative is replaced. Read-only flags are read from the opened source/store descriptors; no ownership or mounts are changed.

## safety_checks

The fixed `/nix/store` anchor must open as a directory with no-follow, have read-only filesystem flags, and match the foreign source UID; the source filesystem must also be read-only. Regular/no-follow source admission, canonical fixed source/digest, exact source/projected bytes, declared Terraform profile, required support/Auth and projected EUID/0600 checks stay in place. Test mapped65534 positive, a different legitimate nonzero anchor positive, foreign source UID including literal0 against a different anchor, writable source/store, wrong anchor type/symlink and foreign mutable owner (including65534). Run actual production predicate through the existing whole batch/report; prove the old numeric-root predicate fails the mapped positive and corrupting the new anchor loses negative controls. Preserve other existing positive/adverse source checks. Independent source AND test-meaning review precedes full native15; WSL Devkit-only update, native53, actual declared producers, closure equality and final review/publication precede Runtime's own installed observation. No Product artifact repin or Fleet retention change follows.

## rollback_plan

If source controls, declared gates or actual receiving evidence contradict the contract, retain the candidate/evidence and stop promotion. Before publication remain at accepted915/a75. After publication Runtime owns safe typed selection/rollback to accepted a75; Source never activates or repairs a station locally. Do not introduce a numeric overflow allowance or erase failed evidence to obtain a pass.

## decision_scope

Only the existing Terraform profile predicate, its source-owned tests and linked plan/docs change. No new capability seam, external provider, credentials, profile/mount/security policy, command/API or data/persistence model is introduced. Root's full supplier commission and actual checker evidence authorize this normal Source correction; R4 approval bootstrap remains separately denied/held.

Framework: [Management Tradeoff Decision Framework](https://github.com/Divine-Shadow/dev/blob/3f32a6e087461efc77f0d23d67138e58a0ea745d/docs/decision-framework/tradeoff_decision_framework.md).
