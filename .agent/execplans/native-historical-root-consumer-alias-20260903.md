# Historical-root consumer-alias manifest shrink

This ExecPlan is a living document maintained according to `.agent/PLANS.md`.

## Purpose

Permit the existing native manifest-shrink transaction to recognize an exact
historical-root linked worktree whose Git link files were written from the
source-derived sandbox namespace instead of the corresponding host namespace.
Also recognize the one source-derived legacy Product identity that live rollout
exposed: the same GitHub repository expressed by the old SCP origin instead of
the declared port-443 SSH origin, and the exact old `codex/agentN/main` branch
instead of `agentN`. Admit the newly observed nested SBT target only through an
exact source declaration.
The operator-visible outcome is that an idle, clean, current-remote-contained
surplus lane can be retired without manual station repair, while arbitrary
aliases and every existing custody failure continue to refuse before mutation.

## Progress

- [x] (2026-09-03) Audited the command and classifier: the reverse `gitdir`
  comparison accepts only the host spelling and therefore prevents the later
  forward-link and `commondir` checks from seeing an exact consumer alias.
- [x] (2026-09-03) Added and ran a red historical-root fixture whose forward
  `.git`, absolute `commondir`, and reverse `gitdir` use the exact
  manifest-derived sandbox namespace while that namespace is deliberately
  absent from the host filesystem.
- [x] (2026-09-03) Admit only byte-exact host or manifest-derived sandbox
  counterparts (plus Git's exact derived relative-host spelling and at most
  one conventional trailing newline); keep all protected inspection and
  canonical-directory validation on host paths.
- [x] (2026-09-03) Replaced operation-wide metadata-view caching with a fresh
  bounded view per custody pass. Deterministic HEAD and valid-index mutations
  during history capture are observed and refused by the destructive recheck.
- [x] (2026-09-03) Removed the recursive shared `.git` crawl, bounded pointer,
  config, index, directory, and Git-output reads, and made capture timeout or
  overflow terminate the complete process group.
- [x] (2026-09-03) Reject repo-local includes and clean/process filters before
  status, preserve common exclude and worktree config semantics, support one
  bounded real split-index companion, and reject non-empty worktree refs.
- [x] (2026-09-03) Added hostile wrong-alias, reverse-mismatch,
  foreign-common, whitespace, redundant-separator, dotdot, over-limit,
  symlink, non-regular, config, and between-pass mutation cases. Focused tests
  prove refusals preserve manifest and protected trees and clean owned scratch.
- [x] (2026-09-03) Preserved `Capture`'s closed-stdin contract in bounded
  capture and made the first selected deadline/cancel/idle cause authoritative
  over a concurrently observed output overflow. Added success, nonzero,
  exact-limit, overflow, idle, cancel, deadline, closed-stdin, and race tests.
- [x] (2026-09-03) Made bounded capture clean the process group even when the
  leader exits first, and wait for proven group disappearance after SIGKILL
  before any return. Regressions cover a redirected-stdio descendant across
  normal wait, overflow, explicit cancellation, and idle expiry, with the
  leader exiting during cleanup and immediate post-return PID checks.
- [x] (2026-09-03) Ran final focused cached Go tests, formatting inspection, and
  `git diff --check`; record results below. At that review checkpoint, no
  publication or deployment had yet occurred.
- [x] (2026-09-03) Published the reviewed alias repair, repinned and centrally
  deployed it, then observed the first live Meowlnir reconstruction canary fail
  before lane mutation because the shared historical registration directory
  legitimately contains more than the arbitrary limit of 32 entries.
- [x] (2026-09-03) Replaced the shared-directory scan with target-shaped
  selection: the bounded exact forward link for a present worktree, or at most
  three exact common-domain probes for an absent worktree. Added more-than-32,
  unrelated-symlink, empty-lane-common, equal/divergent-branch, and detached
  lane-registration regressions.
- [x] (2026-09-03) Completed independent static review and focused verification;
  both approved the target-shaped correction without another rebuild.
- [x] (2026-09-03) Published the target-shaped correction as Devkit
  `8832cd957729ae402364246a377d23623e1bc007`, repinned it in WSL commit
  `0691a3239108a02227c7e401c33d0365c68a6ea8`, deployed the controller and
  Meowlnir closures centrally, and ran exactly one lane-2 canary. The former
  32-registration failure disappeared; reconstruction then refused the old SCP
  source origin against the declared port-443 SSH spelling before lane setup.
- [x] (2026-09-03) Audited the exact surplus agent3 state read-only. The
  remaining sequential false negatives are the source-origin spelling, the
  exact historical `codex/agent3/main` ref, and ignored
  `project/project/target`; process, socket, topology, index, dirt, lock, and
  current-remote/ahead predicates otherwise pass.
- [x] (2026-09-03) Added strict known-migration origin parsing, the one derived
  historical branch ref, its lock and fresh-remote checks, the fifth declared
  residue root, and one combined Meowlnir-shaped acceptance regression plus
  hostile near-miss refusals.
- [x] (2026-09-03) Hardened managed command I/O after review found that an
  escaped session could retain inherited pipes after its leader exited. Pipe
  draining and wait cleanup are now grace-bounded and cleanup failures cannot
  be mistaken for Git's ordinary predicate exits.
- [x] (2026-09-03) Completed two adversarial reviews and cached owning-package,
  race, vet, formatting, and diff gates. Review-found absent-ref lock,
  unselected-ref custody, unbounded marker, and process-group proof gaps now
  have focused regressions; final review approved the corrected source.
- [ ] (2026-09-03) Publish the comprehensive correction, repin/deploy once
  through the central Nix lane, and run one final bounded Meowlnir lane-2
  canary.

## Context

`cli/devctl/internal/commands/nativecmd/native_slot_reset.go` derives three
possible common repositories. Lane-local and transitional legacy repositories
retain their small, owned reverse-registration scans. Historical-root custody
now selects a present worktree through its own exact bounded `.git` pointer,
then validates the selected reverse link and metadata `commondir`; an absent
worktree is classified by its exact branch across those three common domains.
Historical source-created worktrees can contain
absolute consumer paths such as `/workspaces/dev/...` even when the same
manifest declares `/home/bayesartre/dev/...` as the host geometry. The two
namespaces are already paired by `HostWorktreeRoot`/`SandboxWorktreeRoot` and
each agent's `HostWorktree`/`SandboxWorktree`; no ambient symlink or caller path
is authority. Sandbox paths are bwrap mount targets and need not exist while
the host-side shrink classifier runs.

The existing historical-root retirement remains responsible for exact source
identity, branch, fresh current-remote containment, dirt and index custody,
transaction locks, process absence, cold-history capture, durable staging,
manifest CAS,
rollback, and crash recovery. This change touches only recognition and
validation of the exact declared migration identities before those checks.

## Invariants

- The historical host common remains exactly
  `filepath.Dir(manifest.HostWorktreeRoot)/manifest.Repo/.git`; the consumer
  counterpart is derived identically from `manifest.SandboxWorktreeRoot`.
- Only a one-to-one relative mapping beneath those two exact roots is accepted.
  No prefix match, filesystem discovery, `EvalSymlinks`-based equivalence,
  caller override, or arbitrary absolute path may confer authority.
- Reverse `gitdir` must select exactly `spec.HostWorktree/.git` or
  `spec.SandboxWorktree/.git`. Forward `.git` must select the one matched host
  metadata directory or its exact derived consumer counterpart. `commondir`
  must select the exact historical host common or its exact consumer
  counterpart. Absolute text is never trimmed or cleaned before comparison;
  relative host text is accepted only when it equals the exact
  `filepath.Rel` result. Only one final LF may be ignored as Git's conventional
  line terminator.
- Physical worktree, source root, common repository, and registered metadata
  checks continue to use canonical real non-symlink host paths. A consumer
  spelling is compared only as manifest-derived text and is never traversed
  for inspection or mutation.
- Git commands that need linked-worktree state use a fresh bounded, minimal
  metadata view for each custody pass under the already owned remote-proof
  scratch. `HEAD`, `index`, optional `config.worktree`, and at most one exact
  SHA-1/SHA-256-named `sharedindex` companion are copied; scratch-only
  `commondir` and `gitdir` select validated host paths. Non-empty
  worktree-specific refs and other unsupported metadata shapes refuse closed.
- Repository config is read as a bounded regular file and parsed without
  following includes. Any repo-local `include`/`includeIf` or clean/process
  filter refuses before status. Common `info/exclude`, the copied
  `config.worktree`, sparse config checks, and index flags retain or validate
  the semantics used by custody.
- Origin equality remains byte-exact outside `dev-all`/`ouroboros-ide`. Inside
  that one migration domain, only strict lowercase GitHub SSH spellings for the
  same two-component owner/repository identity are equivalent: SCP
  `git@github.com:owner/repo.git`, standard SSH on the default port, or
  `ssh://git@ssh.github.com:443/owner/repo.git`. Raw bounded
  `remote.origin.url` values are read with `--no-includes`; rewrites, multiple
  values, malformed URLs, wrong hosts/users/ports, escapes, and extra path
  components refuse.
- The canonical branch remains exact `refs/heads/agentN`. Only historical-root
  custody for this same migration domain may additionally select the fully
  derived `refs/heads/codex/agentN/main`. A present worktree's bounded `HEAD`
  chooses one of those exact refs, but every other existing allowed ref remains
  custody and must pass the same fresh-remote proof. An absent worktree probes
  both historical spellings, proves every discovered commit against the same fresh remote, and
  prefers the canonical ref if both survive. Both exact lock paths are checked;
  near-miss `codex/agentN/*` refs remain outside authority.
- Ignored generated residue may leave only beneath the five exact overlay
  declarations `.bsp`, `logs`, `project/project/target`, `project/target`, and
  `target`. This does not broaden ambient ignore rules or accept a parent glob.
- The shared historical registration directory is never enumerated. Selected
  pointer/config/index inputs have separate byte limits, selected metadata
  enumeration is capped, Git stdout is capped, and fixed command deadlines
  kill and prove disappearance of the complete original process group. Owned
  pipe drains are grace-bounded, and any escaped descriptor holder or cleanup
  failure is typed as infrastructure failure rather than a Git predicate exit.
  Lock inspection names only exact active
  common, selected metadata, and branch transaction surfaces; it never walks
  the object store.
- Any active branch domain not contained in the same fresh current remote, or
  disagreement with known lane/legacy registration metadata, wrong lanes,
  foreign common repositories,
  malformed selected pointers, selected symlinks/non-regular nodes/locks,
  dirty/ahead/wrong-branch present worktrees, and failed current-remote proof
  refuse before history capture or durable mutation. An absent stale historical
  reverse registration—including detached HEAD or metadata locks—is inert but
  remains byte-preserved because no root-common path is staged or removed.
- Historical common metadata and refs remain byte-identical; only the existing
  typed transaction may retire the surplus worktree, home, and state.

## Decision Log

- 2026-09-03: Repair the existing Devkit classifier rather than add a fleet
  command or station-local migration. The defect is path identity at an already
  governed shrink boundary.
- 2026-09-03: Compare exact manifest-derived lexical counterparts instead of
  canonicalizing arbitrary aliases. Canonicalization could accidentally admit
  a foreign symlink that happens to reach protected Git state.
- 2026-09-03: Limit path compatibility to historical-root registrations.
  Lane-local and transitional legacy paths keep their existing contracts; the
  later strict source-identity exception also applies to the exact v1 legacy
  ownership marker because that marker was written before the port-443 overlay
  origin became authoritative.
- 2026-09-03: Do not require the consumer namespace to exist on the host.
  Normalize only an operation-owned scratch metadata view for Git custody
  commands; never rewrite or ask Git to follow the protected consumer-valued
  metadata.
- 2026-09-03: Tested the simpler real-metadata route with sanitized
  `GIT_COMMON_DIR=<host common>`, `--git-dir=<host metadata>`, and
  `--work-tree=<host worktree>`. Git still followed/validated the
  consumer-valued reverse `gitdir` and failed on the absent consumer root, so
  the minimal scratch view remains necessary.
- 2026-09-03: A review found stale view reuse, normalized attacker-controlled
  pointer text, unbounded scans/output, and config-dependent status execution.
  Treat all four as blocking and retain the correction regressions in the
  owning packages.
- 2026-09-03: The live fleet disproved 32 registrations as a valid bound: the
  analogous controller root has 48 legitimate entries. Do not raise the cap.
  Treat the present forward link as the sole active historical-registration
  selector; for an absent worktree, protect surviving exact branches across all
  three source-derived common domains. When multiple domains survive, every
  discovered commit must be independently contained in one fresh current base;
  equal or divergent contained branch copies are migration residue and select
  the preserved historical domain when present. A sole historical branch keeps
  the same proof; sole lane/legacy domains keep their existing local-base
  check. Unselected/stale historical metadata is
  protected but inert because this transaction never mutates it. A known
  lane/legacy registration remains active custody because it claims the
  selected physical worktree; the lane common is additionally a reset
  candidate, so disagreement with branch selection fails closed.
- 2026-09-03: Treat the SCP and port-443 SSH URLs as one repository only after
  strict parsing and only for the exact Product migration domain. Reading raw
  config with includes disabled prevents `url.*.insteadOf` from manufacturing
  equivalence. Keep new v2 lane creation/reuse byte-exact; the exception is
  read-only retirement compatibility for historical-root and exact v1 marker
  custody.
- 2026-09-03: Accept only the old `codex/agentN/main` branch whose complete
  spelling is derived from the manifest's exact project/repo/prefix/base tuple.
  General Codex prefixes, arbitrary branch aliases, and normalization are not
  retirement authority.
- 2026-09-03: Classify inability to reap managed command state or drain owned
  pipes as infrastructure failure even when a leader supplies an ordinary exit
  code. This preserves fail-closed Git predicate handling without allowing an
  escaped descriptor holder to hang fleet convergence indefinitely.

## Verification

Before the implementation, the exact regression failed as intended:

    go test ./internal/commands/nativecmd -run '^TestNativeSlotManifestShrinkHistoricalRootConsumerAlias$' -count=1
    --- FAIL: TestNativeSlotManifestShrinkHistoricalRootConsumerAlias
    retire exact historical consumer-alias surplus: refuse shared native manifest shrink: surplus native slot 3 has a worktree without its package-owned common Git repository (neither exact lane, legacy, nor historical-root registration)

After lexical recognition was added and the fixture stopped materializing a
consumer alias, it exposed the second host-dependency at the later Git branch
check:

    --- FAIL: TestNativeSlotManifestShrinkHistoricalRootConsumerAlias
    retire exact historical consumer-alias surplus: refuse shared native manifest shrink: surplus native slot 3 must be checked out on exact branch refs/heads/agent3

Final verification is intentionally bounded to the owning Go package and
changed-file hygiene:

    cd cli/devctl
    go test ./internal/commands/nativecmd -run '^TestNativeSlotManifestShrinkHistoricalRootConsumerAlias' -count=1
    go test ./internal/commands/nativecmd -run 'TestNativeSlotManifestShrink(HistoricalRoot|RetiresLegacy|TransitionRejects)' -count=1
    go test ./internal/execx -count=1
    git diff --check

Focused correction results so far:

    go test ./internal/commands/nativecmd -run 'TestNativeSlotManifestShrink(HistoricalRoot|TransitionRejects)' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 11.956s

    go test ./internal/commands/nativecmd -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 16.630s

    go test ./internal/worktrees -count=1
    ok devkit/cli/devctl/internal/worktrees 6.529s

    go test ./internal/commands/nativecmd -run 'TestNativeSlotManifestShrinkHistoricalRootAbsentWorktreeStillUsesBranchCustody' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 1.585s

    git diff --check
    # no output

    gofmt -d cli/devctl/internal/commands/nativecmd/native_slot_manifest_shrink_root_common_test.go cli/devctl/internal/commands/nativecmd/native_slot_reset.go
    # no output

    go test ./internal/commands/nativecmd -run '^TestNativeSlotManifestShrinkHistoricalRootConsumerAlias' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 3.748s

    go test ./internal/commands/nativecmd -run '^TestNativeSlotManifestShrinkHistoricalRoot' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 9.518s

    go test ./internal/execx -count=1
    ok devkit/cli/devctl/internal/execx 2.023s

    go test -race ./internal/execx -run '^(TestCaptureManaged|TestManagedCaptureResult)' -count=1
    ok devkit/cli/devctl/internal/execx 2.173s

    go test ./internal/execx -run '^(TestCaptureManagedLeaderExitCleansRedirectedDescendantBeforeReturn|TestCaptureManagedOverflowAndLeaderExitCleanDescendantBeforeReturn|TestCaptureManagedExplicitCancelCleansLeaderExitDescendantBeforeReturn|TestCaptureManagedIdleCleansLeaderExitDescendantBeforeReturn)$' -count=20
    ok devkit/cli/devctl/internal/execx 13.065s

    go test ./internal/commands/nativecmd -run '^(TestNativeSlotManifestShrinkRetiresCleanRealGitSuffixAtomically|TestNativeSlotManifestShrinkRetiresLegacySurplusWithoutTouchingRetainedCustody|TestNativeSlotManifestShrinkRejectsLegacyLookalikeBeforeMutation|TestNativeSlotManifestShrinkPreflightsEntireSuffixBeforeMutation|TestNativeSlotManifestShrinkRejectsAheadAndWrongBranch|TestNativeSlotManifestShrinkTransitionRejectsGitMetadataLockAndAheadCommit)$' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 1.986s

    go test ./internal/worktrees -count=1
    ok devkit/cli/devctl/internal/worktrees 6.139s

    go test ./internal/commands/nativecmd -run '^TestNativeSlotManifestShrinkHistoricalRootConsumerAlias' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 3.741s

    gofmt -d cli/devctl/internal/commands/nativecmd/native_slot_manifest_shrink_root_common_test.go cli/devctl/internal/commands/nativecmd/native_slot_reset.go cli/devctl/internal/execx/run.go cli/devctl/internal/execx/run_test.go
    # no output

    git diff --check
    # no output

Comprehensive post-canary correction checks currently passed:

    go test ./internal/commands/nativecmd -run '^TestNativeSlotManifestShrinkHistoricalRoot(GitHubOriginIdentity|ConsumerAlias|RejectsNearMissCodexBranchAlias|AbsentWorktreeStillUsesBranchCustody)$|^TestNativeSlotManifestShrinkLegacy' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 2.760s

    go test ./internal/commands/nativecmd -run '^(TestNativeSlotManifestShrinkHistoricalRootConsumerAliasRefusesHostileGeometry|TestNativeSlotManifestShrinkHistoricalRootConsumerAliasRecheckUsesFreshMetadata)$' -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 2.560s

    go test ./internal/config -run '^TestDevAllDeclaresOnlyExactGeneratedSetupLayerFiles$' -count=1
    ok devkit/cli/devctl/internal/config 0.002s

    go test ./internal/execx -count=1
    ok devkit/cli/devctl/internal/execx 2.617s

    go test ./internal/execx -run '^(TestRunManagedEscapedPipeHolderDoesNotHang|TestRunManagedNonzeroLeaderWithEscapedPipeHolderIsCleanupFailure|TestCaptureManagedEscapedPipeHolderDoesNotHang|TestCaptureManagedNonzeroLeaderWithEscapedPipeHolderIsCleanupFailure|TestManagedResultPreservesJoinedExitAndCleanupClassification|TestManagedWaitFailuresAreClassifiedAsCleanup)$' -count=20
    ok devkit/cli/devctl/internal/execx 11.772s

    go test -race ./internal/execx -count=1
    ok devkit/cli/devctl/internal/execx 3.654s

Final cached owning-package gates after adversarial fixes:

    go test ./internal/commands/nativecmd ./internal/config ./internal/worktrees -count=1
    ok devkit/cli/devctl/internal/commands/nativecmd 19.126s
    ok devkit/cli/devctl/internal/config 0.018s
    ok devkit/cli/devctl/internal/worktrees 6.752s

    go test ./internal/execx -count=1
    ok devkit/cli/devctl/internal/execx 2.783s

    go test -race ./internal/execx -count=1
    ok devkit/cli/devctl/internal/execx 3.764s

    go vet ./internal/commands/nativecmd ./internal/config ./internal/worktrees ./internal/execx
    # no output

Independent final review approved the source with no remaining blocker.

## Surprises & Discoveries

- `plan.Build` first projects a host worktree beneath `HostRoot` into the fixed
  `/workspaces/dev` namespace. The first green fixture tried to replace only
  `WorktreeContainerRoot`, which produced an internally inconsistent test
  manifest. The fixture now places its host worktree root outside `HostRoot`,
  so `BuildManifest` uses the declared test-local consumer root and proves the
  same `agentN/<repo>` mapping exercised by production manifests.
- An exact lexical match was not sufficient by itself: later `git -C <host
  worktree>` calls followed the worktree's consumer-valued `.git` file. Passing
  host `--git-dir`, `GIT_COMMON_DIR`, and `--work-tree` still failed with
  `fatal: Invalid path '<consumer root>': No such file or directory` because
  Git validated the consumer-valued reverse `gitdir`. A minimal metadata view
  in the existing owned proof scratch is therefore required for truly
  host-only custody reads.
- The first centrally deployed target-shaped canary proved the registration cap
  was gone and exposed the next exact historical difference rather than failing
  setup: source metadata still records
  `git@github.com:Divine-Shadow/ouroboros-ide.git`, while the overlay now declares
  `ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git`. The same
  read-only audit then found `refs/heads/codex/agent3/main` at zero commits ahead
  of current local main and the nested SBT target as the only later residue
  miss.
- `os/exec` can finish waiting for a leader while a child that called `setsid`
  retains stdout/stderr descriptors. A bare pipe-copy wait therefore made a
  bounded Git operation unbounded. Owning and forcibly closing the read ends
  after the declared grace converts that state into a typed cleanup failure.

## Outcomes & Retrospective

- Historical-root shrink now recognizes only the two exact path spellings and
  narrowly derived source/branch identities described above. The complete success fixture keeps
  the consumer namespace absent, retires only surplus worktree/home/state,
  observes full proof-scratch cleanup, and finds the protected source root
  byte-identical afterward.
- Later Git custody reads no longer follow consumer-valued protected pointers.
  Each preflight/recheck pass gets a new minimal view under the already
  identity-owned proof scratch and uses the validated host common, metadata,
  and worktree.
- The intentionally narrow compatibility set is ordinary linked-worktree
  metadata with regular `HEAD`, `index`, `commondir`, and `gitdir`; optional
  regular `ORIG_HEAD` and `config.worktree`; real `logs`; and empty real `refs`
  directory trees. One regular exact `sharedindex.<40-or-64-lowercase-hex>` is
  supported. Pointers are capped at 4 KiB, config at 1 MiB, copied index files
  at 64 MiB, selected metadata entries at 16, and Git stdout at 4 MiB. The
  historical registration directory is not enumerated; absent-worktree
  selection probes only the three exact source-derived common repositories and
  at most four exact branch refs.
  Extra companions, non-empty worktree-specific refs,
  repo-local includes/effectful filters, and unknown top-level state refuse.
- Residual risk is limited to an otherwise safe historical worktree using one
  of those deliberately unsupported Git metadata forms; it will fail closed
  and require a separately reviewed extension. Earlier staged corrections were
  deployed and each canary advanced to the next exact predicate; this final
  comprehensive candidate still awaits its one bounded rollout canary. The two
  custody passes and exact Git lock checks reduce the race but do
  not create a filesystem-wide read transaction: even a cooperative Git writer
  can acquire, rename, and release its config lock entirely between observations
  and replace common config before status. Removing that final config TOCTOU
  would require an isolated common-config/object/ref view or an additional
  execution sandbox, beyond this bounded alias repair. Managed capture cleans
  and proves disappearance of same-process-group descendants on normal leader
  exit, deadline/cancellation, idle expiry, or stdout overflow; a deliberately
  self-daemonizing child that escapes that group cannot hang the caller:
  retained pipes are force-closed after grace and reported as cleanup failure.
  Killing an arbitrary escaped session remains outside the guarantee, which is
  acceptable here only because the executables are package-owned Git/env/SSH.
