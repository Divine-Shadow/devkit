# SSH + Worktrees Workflow

This guide shows how to run multiple isolated dev agents using Git worktrees and per‑agent SSH/Codex state.

## When to use
- You want N parallel agents, each with its own working copy and independent Codex/SSH state.
- You want quick switching between branches without stepping on other agents.

## One-time setup (flake-backed overlays)
1) Bring up the native overlay: `scripts/devkit -p <overlay> up`
2) Create N worktrees for a repo:
   - `scripts/devkit -p <overlay> worktrees-setup <repo> 2`
3) Open tmux with N windows, one per worktree:
   - `scripts/devkit -p <overlay> worktrees-tmux <repo> 2`
   - Per window:
     - Agent 1: `/worktrees/agent1/<repo>`
     - Agent 2: `/worktrees/agent2/<repo>`

## SSH (GitHub) notes
- For flake-backed overlays, `ssh-setup` seeds host SSH material into the native agent state home; native `exec` also seeds SSH before launching the sandbox.
- For legacy Compose overlays, `ssh-setup` copies your host key and writes the proxy-aware container SSH config.
- Test: `scripts/devkit -p <overlay> ssh-test <index>`

## Common workflows
- Switch worktree branch:
  - `scripts/devkit -p <overlay> worktrees-branch <repo> 2 feature/my-branch`
- Status across worktrees:
  - `scripts/devkit -p <overlay> worktrees-status <repo>`
- Sync:
  - Pull: `scripts/devkit -p <overlay> worktrees-sync <repo> --pull --all`
  - Push: `scripts/devkit -p <overlay> worktrees-sync <repo> --push --all`
- Flip origin to SSH and push:
  - `scripts/devkit -p <overlay> repo-config-ssh <repo> --index 1 && scripts/devkit -p <overlay> repo-push-ssh <repo> --index 1`

## Alternative: codex overlay (shared mount)
- For quick starts without worktrees:
  - `scripts/devkit open 2`
  - Opens `tmux` with 2 windows and sets per‑container HOME via the index‑free anchor `/workspace/.devhome`.
  - Use this when shared working copy is acceptable (Codex/SSH still isolated).

## Caveats
- Avoid running global Git maintenance (e.g., `git gc`) across worktrees concurrently.
- sbt targets (`target/`) are shared per repo path; parallel builds can contend unless using isolated output dirs.
- Legacy Compose SSH over port 443 still depends on `ssh.github.com` in the allowlist; native host-worktree git commands use the host SSH path.
