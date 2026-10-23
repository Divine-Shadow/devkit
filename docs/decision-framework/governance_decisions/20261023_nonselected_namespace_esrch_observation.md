# Physically proven procfs attachment exclusion

## problem

Installed Devkit b16a93bcb0778c77f92a7b5796118d94374436c4 refuses selected-slot
custody planning when a live unrelated process has cwd and root in a stale
procfs chroot directory. Its links and inode remain observable, but directory
open and parent lookup return ESRCH. This is not proof of process exit.

## options

1. Preserve refusal when physical ancestry is unavailable.
2. Ignore ESRCH for all nonlexical paths.
3. Exclude a named process, stop it, or bypass custody checks.
4. Recognize only a pinned procfs directory whose exact mount attachment is
   exposed by a stable same-mount-namespace representative and whose complete
   physical attachment ancestry excludes the pinned selected boundaries.

## selection_rationale

Option 2 was rejected by independent review and reproduced by direct/deeper
physical-alias tests: nonlexical spelling does not prove physical separation.
Option 3 changes policy and is outside this repair's authority. Keep option 1
as the default and implement option 4 only when all physical evidence exists.

One authorized bounded read-only observation established the missing evidence:
the stale cwd/root pins were device87/inode11519/mount541; a same-namespace
representative exposed that exact full procfs mount at device87/inode1, with
mountinfo root `/`. The unchanged pinned ancestry algorithm completed from
that attachment through tmpfs device86/inode2 to its physical root, excluding
the selected worktree/home device2096. Subject/representative PPID, start time
and mount-namespace identities stayed stable. Those numeric observations are
evidence, never production selectors. Root accepted this physical proof as the
bounded implementation basis; design review agreed conditionally.

## safety_checks

The fallback is restricted to nonlexical ESRCH on cwd/root, with both links
pinned to the same object. Pinned fstatfs must identify procfs; kernel fdinfo
must bind the exact live mount ID. Existing process inventory supplies one
usable representative in the same pinned namespace, without another global
scan or namespace entry. Its complete procfs attachment must have root `/`,
actual procfs root inode1, and the same pinned mount/device identity.

All present selected boundary objects are pinned and must remain unchanged,
match the ordinary observer's targets, and be on a different device from that procfs.
The existing target contract omits ENOENT roots for fresh or surplus lanes.
For each such canonical declared root, pin its nearest existing directory
ancestor on a different device, require exact ENOENT and unchanged parent
identity before and after the proof, and retain that absence through the final
return. An empty target set is accepted only with explicit stable witnesses
for the whole declared worktree/home/state boundary set. No target is created.
This excludes any selected object before the procfs mount root. The existing
complete pinned ancestry walker then excludes selected ancestry beyond the
mount attachment. Missing, unreadable, malformed, ambiguous or conflicting
proof fails closed. Process/start/parent, namespace, cwd/root, selected
boundaries, attachment identity and the bounded full mountinfo snapshot are
revalidated. The original lexical selected ambiguity and all other observer
errors remain unchanged. Remaining exe/fd witnesses are still scanned; this
proof grants no ownership and selects or signals no process.

Tests use actual host procfs pins, fstatfs, fdinfo identity and ancestry with
only the failing directory-open operation injected. Positive coverage includes
later foreign-FD refusal. Adverse coverage includes direct/deeper aliases,
missing or wrong namespace/mount/root, subtree/bind attachment ambiguity,
unreadable/duplicate proof, selected procfs device, a genuinely selected
attachment ancestor, and process/link/namespace/attachment/boundary drift.
Absent-root cases cover successful physical exclusion, creation, ancestor
replacement/removal/type change, permission failure, incomplete declarations,
and continued lexical selected-path refusal.

The earlier errno-only candidate passed its full nativecmd and portable Nix Go
suite plus the disposable fixed-owner CLI consequence check before the alias
regressions exposed its defect. Those passing results do not qualify this
replacement. Replacement qualification and independent source/results review
are required before publication. Newer fetched master reset-projection source
must be retained in the integrated publication candidate.

## rollback_plan

Reject or revert the source candidate if review or required gates fail. No
installation or lifecycle effect belongs to this repair, so no runtime rollback
is performed here. Root owns canonical supply, adoption and retry decisions.

## decision_scope

Narrow maintenance of the existing physical-custody observer. This creates no
registry, service, process whitelist, policy exception, namespace transition,
source cleanup or authority to retry a denied lifecycle effect. Publication
requires independent review and qualification through the normal source lane;
Root retains consequential acceptance and canonical WSL adoption.
