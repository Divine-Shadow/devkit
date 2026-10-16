# Native CI fixture portability

The first published history-preflight increment (`4224f5417ea8e5716d206212f5debaa7f7564a56`)
passed the native Nix Go, fixed-owner CLI and complete flake gates, but GitHub
run `38948898401`, job `117134968883`, failed its ordinary `ci-cheap` step.
Management delivered the unchanged 212920-byte job original with SHA256
`d1f52809631af3deb5cbceddc425b9a6a0eb7a2eda6386200ff2bac35a960ea9`.
Source admitted only its two named ZIP entries and checked their CRCs and hashes.
The earlier Source anonymous job-log download refusal remains in the record;
no credentials, provider trust, security settings or denied route changed.

The actual failures expose test-environment differences:

- Controller-owner tests construct a Unix socket inside `t.TempDir`, whose
  runner path includes a long test name and exceeds the Unix socket path limit.
  Reuse the existing short-name socket fixture and exercise a deliberately long
  subtest name through the actual listener and production validator.
- Terraform readiness fixtures map fixed runtime paths but leave the generic
  `test ! -e /var/run/docker.sock` observation attached to the CI host. Map that
  exact filesystem observation to a private fixture sentinel. Keep the actual
  production generic batch unchanged and prove its refusal when the sentinel
  exists. The original generic exit does not itself expose the failing
  subcondition; do not claim a captured runner socket observation.
- Managed SSH tests use ambient `ssh`, despite testing the source-selected
  managed config's environment-expanded `Include`. The package Nix checks use
  the declared OpenSSH dependency. Make the ordinary source-test target select
  that same dependency via a narrow flake package alias and retain the other-host
  config and strict real host-key assertions. This does not claim compatibility
  with every ambient OpenSSH parser.
- The daemon fixture assumes `sshd` is next to `ssh`, which does not hold for
  the Ubuntu executable layout. Resolve the test daemon independently from the
  declared test PATH. Preserve start errors and distinguish signal termination
  after wait from absence of a process receipt. Keep deadlines, exact owned Unix
  transport, host-key rejection, none-auth refusal and all-branch cleanup.

The tradeoff is to repair test isolation and dependency selection rather than
weaken production guards, skip cases, extend timeouts or make old ambient
clients a new production support requirement. Correct source contracts and
observable adverse consequences take precedence over a fast retry.

Verify with independent source review, the real immutable native Go and flake
checks, and a new ordinary repository CI run from the changed published source.
Keep full original outputs and distinguish each result. Rollback is a source
revert; it introduces no live-state or authentication transition. WSL composition
and Runtime receiving remain pending the required accepted source/CI result.
