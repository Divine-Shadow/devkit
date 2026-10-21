# Retain the source-CI dependency symlink identity through child exit

## problem

Normal Native Runtime Gates run `39627807974`, candidate `801121c`, failed
before its Go/Nix gates. Management's recovered job `119317614457` identifies
`test_preserves_replacement_after_child`: cleanup called `vendor.unlink()` on
the replacement directory and raised `IsADirectoryError`. The original public
log is retained by Management with SHA256
`58690dbe6d0c6791e8e6fe127b2d3a65afe749f90d9e88b08fd376026e97e05e`;
Source has received that diagnosis, not the full original log bytes.

The adapter compares device/inode numbers after the child exits, but does not
keep the created symlink inode alive. Its synchronous replacement test allows
the filesystem to reuse that inode. The same problem could affect a replacement
symlink, even when its target text equals the original supplier.

## options

1. Add only a symlink-type check. This prevents the observed directory unlink
   but does not prevent a foreign replacement symlink from reusing the inode.
2. Retain a Linux `O_PATH|O_NOFOLLOW|O_CLOEXEC` descriptor to the created symlink,
   validate type and identity before starting the child, and keep the descriptor
   through cleanup. Also require the current entry to be a symlink.
3. Retain every binding, remove replacement directories, or suppress arbitrary
   cleanup exceptions. These change the existing contract or hide real errors.

## selection_rationale

Select option 2 within the existing Linux source-CI adapter. Correctness requires
preserving replacement entries, not merely avoiding this exception. An open
descriptor prevents reuse of the deleted original inode while the child runs.
Real child replacement and descriptor-lifetime tests can verify this property.
Preserving cancellation/error behavior and foreign entries takes priority over
making CI appear green. Independent design review recommended the retained pin.

This is normal bounded source tooling repair, not a new dependency supplier,
runtime capability, custody exemption or alternative CI/publication path.

## safety_checks

Keep exclusive symlink creation, exact go.mod/go.sum checks, immutable vendor
payload, process-group cancellation and interrupted-binding retention. Before
child dispatch, compare the held symlink type/device/inode to the observed path.
Close the descriptor on every outcome. Exercise real directory, file, symlink,
dangling-link and missing-entry replacements, failed children, initial identity
changes, descriptor retention after deletion, and eventual descriptor closure.

The initial create/stat/open observations and final lstat/unlink are not atomic
conditional namespace operations. This adapter retains its single source-CI
writer assumption; it does not claim protection against a hostile concurrent
same-UID pathname writer. Do not retry an unlink or recursively remove a foreign
directory if a late race produces an exception. Preserve genuine failure.

Keep the existing witness candidate and its accepted source tests/review at
their actual scope. Run the full adapter contract suite and ordinary source/CI
gates, with independent correction review. No additional heavy producer is
dispatched while the previous operation's remaining disposition is reconciled.

Actual bounded validation passed all 14 adapter contracts. The deleted-inode
lifetime control fails against the unchanged `801121c` adapter, as expected;
this is not a claim to replay the historical filesystem's inode allocation.
The normal one-worker `make build-cli` command also passed through the corrected
adapter and left no vendor entry. Independent correction review passed. These
source results do not replace full native CI, immutable builds or receiving.

## rollback_plan

Before publication, preserve the candidate, original failure and validation
receipts. If this correction loses replacement protection or error/cancellation
semantics, revise its owning source and rerun normal gates. After publication,
use an owning-source revert and ordinary immutable convergence; no live surgery.

## decision_scope

Source1 lease `source-devkit-selected-slot-custody-20261021`, exact CI dependency
binding cleanup. No Product edit, custom Codex expansion, direct push, Runtime
adoption, process reset, security/auth/network change, or claimed immutable
qualification follows from this decision. The signed Management follow-up was
verified by the existing receiver as dot/current/authority-message-v1 with an
exact returned-byte match; it adds no platform waiver or human signature.
