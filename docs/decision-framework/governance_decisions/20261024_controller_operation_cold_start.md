# Cold Management operation-provider readiness

A socket-activated system listener can exist while its service is inactive and
its last identity file names the preceding socket generation. Requiring that
identity before the first connection prevents the fresh controller from starting
the service that must issue the new identity. The generation guard is correct;
the ordinary launch was missing the provider-readiness step.

Only an actual systemd-owned managed app-server launch now prepares this existing
provider. It validates the named profile, exact binds, clean pinned source roots
and socket type/mode/owner first. A fully valid existing identity is reused.
Missing identity or socket-generation mismatch permits one bounded connection to
the fixed declared listener. No bytes are sent; Fleet rejects EOF before durable
operation admission. The launcher does not write identity or manage systemd.

Within a single90-second deadline the broker must issue an identity passing all
unchanged schema, nofollow, ownership, mode, generation, immutable package,
manifest, source, target and authentication-contract guards. Connection success
alone is not readiness. Malformed/foreign identity and other guard errors still
refuse; an active stale issuer that does not refresh remains unavailable after
the bounded wait. No operation ID, business dispatch, broker restart or fallback
transport is created by this preparation. Planning, dry-run and BuildBubblewrap
remain effect-free. Ordinary consumers retain their existing path.

This repairs existing provider consumption, not a new effect endpoint or a grant.
Normal source qualification/publication and existing environment receiving own
installation. Installed controller/Writer acceptance must be earned separately.
