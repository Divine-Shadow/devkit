# Software-only Local3 native startup

Local3's running Codex 0.154 cannot satisfy the selected 0.160 persisted task
constructor. Normal native preparation also hydrates credentials, including AWS,
even when seeding is not forced. The user reserves hydration separately.

The existing typed `gui.start-app-server` operation accepts `--software-only`
only for exact `shadow-throne-local-3`. It preserves the selected source/config,
native isolation, lifecycle lease, ordinary supported idle drain and all refusal
gates. It rejects reset/reconstruct/force modes, route overrides and other targets.
The derived command passes the selector to selected Devctl's native exec, which
permits only this inventory target's Codex app-server. Its public receipt includes
`software_only_preparation: true`.

Software preparation requires an existing real native home and `.codex` directory.
It converges public software/configuration and runtime support. It does not call
Codex/SSH/AWS/Terraform credential hydration, rewrite SSH identity/config files or
migrate legacy Codex state. It uses the existing credential paths. Missing or
unusable credentials remain a separate user-owned boundary; there is no redirected
credential root, fake identity, ignored hydration error or implicit fallback.

Normal preparation is unchanged. This selector is not a task-admission exemption:
the actual 0.160 version, configured model catalog/settings, durable task/goal,
clean current main lineage, native tools/real compilation and ordinary Desktop
composer acceptance must still be proved on the intended consumer.

After published composition and review, the runtime judge owns the exact operation:
`fleet gui start-app-server --software-only --idempotency-key batch-local3-software-only-startup-20261005 --format json shadow-throne-local-3`.
This is source documentation, not an execution or activation receipt.
