# Native Layout Examples

Examples in this directory are for native flake-backed agents.

Useful commands:

```bash
kit/scripts/devkit --dry-run -p dev-all layout-apply --file kit/examples/orchestration-ouro8-devall.yaml
kit/scripts/devkit -p dev-all layout-apply --file kit/examples/orchestration-ouro8-devall.yaml --tmux --attach
```

`orchestration-ouro8-devall.yaml` launches the `dev-all` overlay with eight agents and prepares matching `ouroboros-ide` worktrees. Use `--tmux` to force window creation when `DEVKIT_NO_TMUX=1`; use `--attach` to attach after windows are created.
