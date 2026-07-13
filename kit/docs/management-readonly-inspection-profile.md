# Management Read-Only Inspection Profile

The `management-inspection` Nix app gives Management a source-derived view of
one exact repository commit without mounting or copying a live Product or agent
worktree. It is for code inspection, not implementation or publication.

## Invariants

- `refresh` resolves an explicitly named Git revision and exports that commit
  with `git archive`. Dirty tracked files, untracked files, Git metadata, and
  Codex homes/sessions are not copied.
- The exported tree and its identity JSON are added to the Nix store and bound
  read-only inside the profile at `/workspace/source` and `/inspection`.
- The only persistent writable mount is the displayed, revision-specific
  runtime generation at `/workspace/state`. `HOME` and the XDG state paths are
  fresh directories within that generation. `CODEX_HOME` and
  `CODEX_ROLLOUT_DIR` are removed from the profile environment.
- A profile never follows a branch or worktree automatically. `status` and each
  `exec` print the resolved 40-character commit identity.
- Refreshing to another commit creates another runtime generation and preserves
  the old generation. There is no implicit merge, pull, reuse of mutable home
  state, or cleanup casualty.

The profile additionally unshares the network and all other namespaces through
Bubblewrap. It exposes the Nix store, proc/dev, an ephemeral `/tmp`, immutable
source/identity, and only the explicit writable runtime generation.

## Use

Choose the repository and revision explicitly. A branch name is accepted as a
one-time selector, but the resulting profile records and stays on the resolved
commit until another explicit refresh.

```bash
STATE_ROOT="$HOME/.local/state/devkit/management-inspection"

nix --extra-experimental-features 'nix-command flakes' \
  run /workspaces/dev/devkit#management-inspection -- \
  refresh \
  --name product \
  --state-root "$STATE_ROOT" \
  --repo /workspaces/dev/ouroboros-ide \
  --revision origin/main
```

Read the current source and state identity:

```bash
nix --extra-experimental-features 'nix-command flakes' \
  run /workspaces/dev/devkit#management-inspection -- \
  status --name product --state-root "$STATE_ROOT"
```

Run a bounded inspection command or enter the minimal shell:

```bash
nix --extra-experimental-features 'nix-command flakes' \
  run /workspaces/dev/devkit#management-inspection -- \
  exec --name product --state-root "$STATE_ROOT" -- \
  rg 'trait .*Service' backend

nix --extra-experimental-features 'nix-command flakes' \
  run /workspaces/dev/devkit#management-inspection -- \
  shell --name product --state-root "$STATE_ROOT"
```

Inside the profile:

- `DEVKIT_INSPECTION_SOURCE=/workspace/source`
- `DEVKIT_INSPECTION_STATE=/workspace/state`
- `DEVKIT_INSPECTION_IDENTITY=/inspection/identity.json`
- `DEVKIT_INSPECTION_REVISION=<resolved commit>`

To inspect newer source, run `refresh` again with the desired revision. The
command never fetches, pulls, changes a branch, mutates the repository, or
restarts an agent. Fetching a missing commit remains an explicit operation by
the repository owner outside this read-only profile.

## Verification

The compiled Devkit tests cover all of the following:

- a Git archive contains committed content but neither dirty/untracked content
  nor `.git` metadata;
- the Bubblewrap argument contract binds source/identity read-only, binds only
  the separate runtime generation writable, and removes Codex-home variables;
- an actual Bubblewrap test writes successfully to runtime state while a write
  to projected source fails with `Read-only file system`;
- status/readback exposes the exact revision and isolation flags.

Run the executable mount test explicitly on a NixOS host:

```bash
cd /workspaces/dev/devkit/cli/devctl
DEVKIT_MANAGEMENT_INSPECTION_BWRAP_TEST=1 \
  go test -count=1 ./internal/commands/managementinspect
```
