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

## Full-suite prerequisite and synthetic-subject correction

The first changed normal CI on990f82d ran naturally to exit2/reaped. Its Go
package rows were34 passed/five failed/12 without tests; later gates did not
run. Missing sqlite3/tar/jq/setsid are supplied by a named locked-pkgs test-only
buildEnv consumed by cli/devctl/Makefile. Existing declarations already supply
these tools to portable tests or Management inspection. No global PATH or
runtime bundle is changed, and complete source tests remain mandatory.

Root explicitly adjudicated the additional inherited-selector fixture failures
as test-subject isolation work. Independent design/security review required
explicit bindings in managed_git_transport_test.go's ordinary Git control,
launch_terraform_tools_linux_test.go's provider-preservation fixture, and both
direct SeedAWS tests in launch_test.go. Git's legacy/current child commands are
both owned fixtures; an ambient poison connector must never run, and exactly
two fake SSH calls per nested-home case must occur. The AWS nested fixture
selects its synthetic credential file, counts exactly one owned refusing
hydrator call, and retains its original preservation/admission assertions.
An owned ambient poison-file inotify watch must observe zero access and the
nested selector must restore automatically. The symlink fixture explicitly
selects an absent owned credential source and asserts it remains absent.

These tests are nonparallel and use ordinary t.Setenv or owned child Env.
Production SeedAWS precedence, managed Git transport, parent Make/publication
selectors and all actual credential files/auth/security settings are unchanged.
No selector is unset to fall back to a real home, connector or auth service.
Earlier failed logs remain original evidence. Affected finite controls and
independent implementation review precede one materially changed full CI.
Root's latest allocation bars that6GiB CI while BOTH C4 and MCP4GiB controls
are active; preserve those jobs and dispatch only at the permitted boundary.

## Late overlay authority command consumption

The e20d408 normal CI completed Go39/0, native reset and its16 x86_64 flake
checks, then terminated2/reaped at the existing overlay authority helper's bare
`nix` lookup. The controller's absolute authorized Nix had actually executed
all prior builds; this was source helper command consumption, not a new grant
or a security denial. Its later jq use was also outside the Go test PATH scope.

The owning helper now accepts the Make-selected Nix executable and options as
explicit argv after `--`, retaining empty/quoted/resource arguments without
command-text evaluation. Standalone default invocation remains unchanged;
explicit executable failure cannot fall back. Its Make target supplies jq from
the already-declared immutable test-tools package only for the two child
commands. Actual runtime/publication environment, credentials and profile stay
unchanged. All package/source/lock/runtime-plan assertions are preserved.

Finite light tests use owned refusing Nix executables to check exact resource,
empty, literal argument and environment transport, nonzero propagation,
standalone default, missing explicit command without fallback, and malformed
package refusal. These witnesses stop at explicit controlled refusal and are
not successful overlay authority executions. An initial exploratory whole-helper
fake was rejected by the original immutable runtime-path assertion; no assertion
was weakened. The successful real Make/tool/authority path remains a focused
Nix gate after Root releases the C4 slot. No Nix build occurs in light tests.
Independent review precedes changed normal CI; no prior successful gate is
silently promoted to the updated source. Root's exclusive C4 packaging
allocation holds focused Nix builds and that next heavy CI until its actual
terminal boundary.
