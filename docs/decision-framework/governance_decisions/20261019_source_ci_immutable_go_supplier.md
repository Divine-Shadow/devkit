# Source CI consumes the declared immutable Go dependencies

## Problem and decision scope

Normal `make ci-cheap` failed during its initial ambient Go build with a
`proxy.golang.org` request reporting `Forbidden`. No test or Nix gate ran.
The production Devctl fixed-output `goModules` supplier already contains the
same module versions. This decision repairs generic Devkit source CI only;
it grants no network access, Product selection, runtime adoption or deployment.

## Options considered and selection rationale

Choose a named flake binding of the existing production `goModules` output,
with exact source go.mod/go.sum bytes, consumed by the complete Make CI recipe.
Changing network policy, seeding an operator module cache, or copying foreign
sources would change a different boundary. Replacing the complete source suite
with portable Nix Go tests would omit integration/config coverage.

Correctness comes first: fail on mismatched declarations or an existing vendor
path; inherit vendor mode through normal Go builds and integration subprocesses.
Verifiability follows: preserve every existing CI command, the fixed-output
vendorHash and dependency recipe. Operational safety requires temporary local
binding only and cleanup of precisely the helper-owned symlink. Preserve the
original refusal as evidence; source tests are not an installed-runtime result.
Delivery speed does not justify a reduced gate or an alternate network route.

## Safety checks

Lightweight helper tests cover exact declaration matching, inherited flags,
child failure, existing data preservation, replacement preservation and signal
forwarding, including cancellation during child creation. Cancellation targets
the owned process group and retains the binding for inspection: waiting for
Make alone cannot establish all descendants have exited. Independent source review precedes the next allocated normal CI run.
Normal flake/source/container gates remain required at their declared scopes.
No home cache, proxy, authentication, dependency version/hash or vendor payload
is changed. The binding derivation only references the existing supplier and
copies the two declarations from its own immutable source snapshot.

## Rollback plan

Revert the named binding, Make orchestration and helper together. Retain the
original failing receipt and qualified SBT diagnostic artifact; no runtime was
adopted by this change. Interrupted bindings must be inspected, never silently
removed by a subsequent invocation.
