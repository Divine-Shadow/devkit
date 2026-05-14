# Overlay Template

Copy `_template` to `overlays/<your-overlay>` and update:

- `devkit.yaml`: set `defaults.repo`, `runtime.flake`, `runtime.codex_version`, and `runtime.core_check`.
- `runtime.nix`: declare the packages, environment, and shell hooks your agents need.
- `flake.nix`: expose the overlay-local flake entrypoint.

Smoke the overlay before use:

```bash
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/<your-overlay> --output-lock-file /dev/null --command true
kit/scripts/devkit --dry-run -p <your-overlay> native plan --repo <repo>
kit/scripts/devkit --dry-run -p <your-overlay> ensure-ready --repo <repo> --runtime-only
```
