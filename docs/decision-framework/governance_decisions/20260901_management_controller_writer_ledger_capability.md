# Management controller writer-ledger capability

Framework: `docs/decision-framework/tradeoff_decision_framework.md`

## problem

The installed `nixos.deploy-closure` campaign guard requires a live writer
lease in `/home/bayesartre/dev/.agent/fleet-work/work.sqlite`, while the named
`management-controller-convergence/v1` bubblewrap profile hides that canonical
directory. Reconstructing the unchanged profile therefore cannot create or
renew the lease needed by its already-declared deployment effect.

## options

1. Bypass the profile with host SSH, a privileged helper, or direct deployment.
2. Add a generic broker operation that creates arbitrary work-ledger entries.
3. Project the existing canonical fleet-work directory read-write in the
   owning Management controller profile, set its fixed `FLEET_WORK_DB`, and
   retain the existing typed local `fleet work` commands.

## selection_rationale

Option 3 restores an existing capability to its owning controller without
adding an effect class or transferring deployment custody. Correctness keeps
campaign admission and the sole typed deployment route unchanged.
Verifiability comes from exact plan, launch, mode, ownership, and consumer
closure checks. Operational safety exposes only the protected ledger
directory. Epistemic integrity keeps the local work command and deployment
validator on the same canonical SQLite authority. Speed ranks after those
properties.

## safety_checks

- Only the exact compiled directory is admitted; caller-selected source or
  target paths fail closed.
- The source must be a real, non-symlink, mode `0700` directory owned by the
  launching controller user.
- `FLEET_WORK_DB` is fixed to the canonical `work.sqlite`; the caller cannot
  select another ledger.
- The bind is required and read-write because SQLite journals and WAL files
  are created beside `work.sqlite`.
- Non-controller consumers receive no ledger bind.
- Devkit unit/integration tests and the WSL/Nix profile-consumer and full
  closure gates must pass before activation.
- The first activation may use only the documented one-time self-repair
  bootstrap for the exact published candidate closure; the reconstructed
  controller must immediately prove the ordinary typed route.

## rollback_plan

Before activation, revert the Devkit commit and WSL/Nix pin. After activation,
deploy the prior canonical closure only through the ordinary typed route. If
the profile exposes any parent of the canonical fleet-work directory, accepts
a symlink or permissive mode, or reaches a non-controller consumer, reject the
candidate rather than weakening campaign admission.

## decision_scope

Devkit Management-controller mount composition and its WSL/Nix consumer pin
only. This adds no Product authority, role schema, deployment command, broker
kind, provider, credential, or alternate lifecycle path.
