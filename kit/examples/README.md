Examples for tmux layouts and orchestration

- Dry-run: append `--dry-run` to print docker/tmux commands without executing.

Useful commands
- Preview orchestration: `devkit/kit/scripts/devkit --dry-run layout-apply --file devkit/kit/examples/orchestration.yaml`
- Apply orchestration: `devkit/kit/scripts/devkit layout-apply --file devkit/kit/examples/orchestration.yaml`
- Preview layout-only windows: `devkit/kit/scripts/devkit --dry-run tmux-apply-layout --file devkit/kit/examples/tmux.yaml`
- Apply layout-only windows: `devkit/kit/scripts/devkit tmux-apply-layout --file devkit/kit/examples/tmux.yaml`

