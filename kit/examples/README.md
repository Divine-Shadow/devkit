# Native Layout Examples

Examples in this directory are for native flake-backed agents.

Useful commands:

```bash
kit/scripts/devkit --dry-run -p dev-all layout-apply --file kit/examples/orchestration-ouro8-devall.yaml
kit/scripts/devkit -p dev-all layout-apply --file kit/examples/orchestration-ouro8-devall.yaml --tmux --attach
```

`orchestration-ouro8-devall.yaml` launches the `dev-all` overlay with eight agents and prepares matching `ouroboros-ide` worktrees. Use `--tmux` to force window creation when `DEVKIT_NO_TMUX=1`; use `--attach` to attach after windows are created.

`orchestration-ouro8-static-devall.yaml` keeps the same eight `dev-all`
agents and adds paired `static-N` windows that cd into the sibling
`/workspaces/dev/ouroboros-static-front-end` checkout. This is the preferred
layout when one agent needs to compare app code, backend/domain docs, static
marketing content, and Gatsby fixture data without switching runtimes.

The paired static windows intentionally target the same `dev-agent` indexes as
the `ouro-N` windows. `layout-validate` will warn about multiple windows per
agent index; that warning is expected for this companion layout.

From a `static-N` window, run the fixture-safe Gatsby server with:

```bash
port=$((8000 + ${DEVKIT_NATIVE_AGENT:-1}))
USE_CONTENTFUL_FIXTURE=1 GATSBY_TELEMETRY_DISABLED=1 \
  npm run dev -- --host 127.0.0.1 --port "$port"
```

The dev-all ingress config includes matching `http://static-N.localhost` routes
to per-agent host ports `8001` through `8008` for those windows. Use fixture
mode by default so agents can inspect the marketing surface without Contentful
credentials. If a native ingress process is not running, use the direct
per-agent URL, for example `http://static-1.localhost:8001`.

Useful dry-run:

```bash
kit/scripts/devkit --dry-run -p dev-all layout-apply \
  --file kit/examples/orchestration-ouro8-static-devall.yaml \
  --skip-broker --skip-ready
```
