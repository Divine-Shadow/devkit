# Manifest-selected Management controller

Framework: `docs/decision-framework/tradeoff_decision_framework.md`

## problem

The immutable Management manifest selected `shadow-throne-management`, but Devkit accepted only the separately compiled `shadow-throne-management-2` identity. The resulting fail-closed rejection prevented both declared GUI services from launching after recovery.

## options

1. Make the manifest controller GUI unconstrained.
2. Keep one hard-coded GUI identity and force the manifest back to it.
3. Accept either of the two compiled Management GUI identities while continuing to require the exact compiled controller node and Product-agent host.

## selection_rationale

Option 3 preserves the compiled capability allowlist while allowing the authoritative manifest to select its active member. It ranks correctness and explicit contracts first, remains directly testable, and avoids configuration drift between Management and Devkit.

## safety_checks

- A focused test accepts `shadow-throne-management`.
- The existing `shadow-throne-management-2` path remains accepted.
- An undeclared Management-like identity is rejected with the existing fail-closed error.
- Controller node, Product-agent host, inventory hashes, operation handle, mount policy, and permission contract validation are unchanged.

## rollback_plan

Revert this commit if either compiled GUI identity can launch without being selected by the immutable manifest, or if an uncompiled identity passes validation.

## decision_scope

Devkit Management-controller profile validation only; no Product, persistence, provider, credential, or public API change.
