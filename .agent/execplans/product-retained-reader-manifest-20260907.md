# Product retained-reader manifest consumption

## Objective and authority

W2 task `01a07296-185d-7af1-9b6d-4cc3b2cc4e53` owns Root's authorized
consumption of Product reader `d99f7b73ad5b6f7739351c77bedc8ab87c42dec1`.
The existing WSL gate demonstrated that Devkit's strict v8 manifest decoder
rejects the optional `guiThreadControl` artifact emitted by the integration.
Repair that consuming schema in current canonical Devkit source; do not
reimplement the reader or broaden controller effects, mounts, or permissions.

## Progress

- [x] Confirm canonical `origin/master` at `78680509a2a35f1db36e54f974c362aca84486e9`
  and fast-forward the clean existing Devkit source checkout.
- [x] Recognize the exact optional artifact fields, validate controller,
  immutable package/executable geometry and full source revision, retaining
  strict unknown-field rejection and existing absent-field compatibility.
- [ ] Pass the existing complete Devkit flake gate, publish cleanly and pin the
  exact published revision in WSL/Nix.
- [ ] Pass WSL's existing integrated consumer/closure gates and canonical
  activation, then W2 performs the six authorized actual retained-index reads.

## Decisions and limits

This is the same declared v8 artifact projection consumed by Fleet. Devkit
validates it but neither selects Product source nor invokes the reader. No
socket, mount, lifecycle, Product dependency or permission expansion is added.
A malformed present field refuses; absence remains valid for existing v8
manifests. Outer bwrap and all Local3/Emerald exclusions remain mandatory.

## Verification and outcome

Use existing `checks.x86_64-linux` outputs, including `devctl-go-tests`, and
then `management-controller-devkit-profile-consumer` in the full WSL gate.
No live effect began from the failed candidate. Source repair is in progress.
