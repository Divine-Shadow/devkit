# Native selected-lane Git reciprocal projection

## Purpose and authority

Correct existing package-owned Product native Git projection when host
`<root>/agentN/ouroboros-ide` is mounted at `/workspaces/dev/ouroboros-ide`.
The selected native and host views must share common repository, index, HEAD
and refs, with reciprocal registration valid in each view and sibling custody
unchanged. ROOT authorized an unpublished Devkit candidate in the existing
native source session. Publication, pins, deployment and Product effects remain
with ROOT; the goal remains paused.

## Admission and evidence limits

Fetched clean `origin/master` at `19c048ac869ebe13d263f0f8689f55c8a2711ef8`;
Git metadata is writable. ROOT supplies accepted Management e18 / WSL cc710
convergence and native topology. Real nested bubblewrap succeeds in the declared
Nix dev-all toolchain. No live Product inspection or repair is assigned.
The existing whole-root relocation test preserves agentN geometry and does not
prove the isolated lane projection. Its fake exec PASS is not namespace proof.

## Progress

- [x] Inspect source guidance, source admission, metadata and mount composition.
- [x] Record bounded representation decision before implementation.
- [x] Reproduce reciprocal failure with real Git and selected native geometry.
- [x] Implement validated native reverse-pointer projection; extend plan,
  worktrees and actual CLI integration including refusal and sibling controls.
- [x] Run focused fixtures, format/vet and full source-owned make ci-cheap using
  immutable matrix inputs and normal native-reset-test-config derivation.
- [x] Prepare clean unpublished candidate and terminal evidence for ROOT review.

## Decision Log

Use one native-only read-only projection of the selected reverse `gitdir` file,
validated against canonical host reciprocity and package lane ownership. Keep
host metadata bytes and physical common/index/HEAD/refs unchanged. See
[decision](../../docs/decision-framework/governance_decisions/20260912_native_git_reciprocal_projection.md).
No aliases, registry, reset changes or additional FETCH_HEAD admission gate.

## Verification and next action

Exercise real Git status/index/commit/ref changes through the emitted selected
lane plan and actual native CLI execution; prove host coherence and valid
non-prunable registration from both views. Retain whole-root portability as a
separate control. Refuse malformed, conflicting, foreign, traversing or symlinked
metadata; revalidate before preparation and command construction. Prove sibling
metadata unchanged. Focused commands use `nix develop .#dev-all`; full acceptance
uses `make ci-cheap`, its native reset fixture, complete Nix checks/package and
source-declared static/authority gates, one Nix job/core and sequential heavy
checks. Receipt/logs live in /tmp; update this plan with actual outcomes.
Real namespace regression first failed with `/workspaces/agent1/ouroboros-ide`
prunable, then passed with the emitted native-only pointer. Real CLI prepare/exec
also passed, including a host-visible ref write and unchanged host pointers.
Native commit/index/HEAD/ref coherence and sibling byte preservation passed.
Focused logs: `/tmp/devkit-git-backlink-red.log` and
`/tmp/devkit-git-backlink-focused2.log`. The first CLI attempt skipped on denied
module download and supplies no proof; later runs used the exact declared Nix
`devctl.goModules` output via a temporary source vendor link, removed at closure.
Full source CI initially exposed shell-environment issues: long Nix-shell TMPDIR
socket paths, absent setsid, then inherited Go 1.22 GOROOT with the flake's Go 1.24
test toolchain. Use the declared devctl-go-tests environment, clear inherited
GOROOT, and TMPDIR=/tmp. This retains every gate. Its first complete Go run
exposed an ETXTBSY race in the existing OpenSSH fixture installer; a test-only
atomic rename preserves the same real SSH checks. Review also removed an
unnecessary other-registration scan: exact selected reciprocity remains the
binding; unrelated prunable metadata cannot become a new admission gate.
The corrected OpenSSH delayed-pack fixture and selected native CLI both passed.
Complete `make ci-cheap` passed with every existing phase, including all 12
x86_64-linux flake checks, the built devctl package's overlay authority, runtime
metadata, full matrix, and both static guards. `go vet ./...`, tracked-Go gofmt
check and `git diff --check` passed. Final fetch still resolves origin/master to
19c048a. No integration change was needed.

The full command used `nix develop .#checks.x86_64-linux.devctl-go-tests` with
Go 1.24, dev-all's declared jq/bubblewrap, the declared `devctl.goModules` vendor
output, and accepted WSL cc710's immutable matrix inputs (Terraform a84b3b7).
It cleared inherited GOROOT, used TMPDIR=/tmp, and inherited NIX_CONFIG
max-jobs=1 / cores=1 through all nested scripts. A stopped intermediate CI run
had passed Go and was only evaluating Nix; it was cancelled before nested Nix
commands to enforce those inherited limits. No gate was waived.
Terminal evidence: `/tmp/devkit-native-git-reciprocal-review-receipt.json`.
Next: ROOT independent read-only review of the exact unpublished candidate.

## Surprises & Discoveries

Relative metadata alone cannot preserve both non-isomorphic host and isolated
native reciprocal paths. The public cwd need not change if only the native
reverse pointer is projected; a full-root alias would not establish that claim.

## Outcomes & Retrospective

The source repair and required source CI are complete. Real namespace and CLI
fixtures establish native reciprocal registration, shared index/HEAD/refs and
unchanged host pointers/sibling metadata. Selected ownership and mount conflicts
still refuse; unrelated stale registrations are not admission gates. The only
additional code change is atomic replacement inside the existing real SSH test
fixture, preserving its refusal and delayed-pack checks.

The final review receipt records the candidate, exact diff, tested source hashes,
all command/log results, package realization and clean source refs. The final
plan outcome update changes no tested code. Temporary vendor/build outputs are
removed before closeout. ROOT independent review remains mandatory. No source
publication, pins, deployment or live Product effects were performed; source
proof does not establish deployed behavior. The existing goal remains paused.
