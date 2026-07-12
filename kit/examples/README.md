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

From an `ouro-N` window, run the app frontend through Netlify dev on that
agent's indexed host ports:

```bash
agent=${DEVKIT_NATIVE_AGENT:-1}
app_port=$((45173 + agent))
app_target_port=$((46173 + agent))
cd /workspaces/dev/ouroboros-ide/frontend
NETLIFY_TELEMETRY_DISABLED=1 BROWSER=none netlify dev \
  --offline \
  --no-open \
  --skip-gitignore \
  --port "$app_port" \
  --target-port "$app_target_port" \
  --command "npm run dev -- --host 127.0.0.1 --port $app_target_port"
```

The direct app URL is `http://127.0.0.1:$app_port`. When native ingress is
running, the matching route is `https://ouroboros-N.test`; agent 1 also backs
`https://ouroboros.test`.

From a `static-N` window, run the fixture-safe Gatsby server with:

```bash
port=$((8000 + ${DEVKIT_NATIVE_AGENT:-1}))
USE_CONTENTFUL_FIXTURE=1 GATSBY_TELEMETRY_DISABLED=1 \
  npm run dev -- --host 127.0.0.1 --port "$port"
```

The dev-all ingress config includes matching app routes to Netlify ports
`45174` through `45181` and matching `http://static-N.localhost` routes to
Gatsby ports `8001` through `8008`. These per-agent host ports avoid collisions
because native agents share host networking. Use fixture mode by default so
agents can inspect the marketing surface without Contentful credentials. If a
native ingress process is not running, use the direct per-agent URLs, for
example `http://127.0.0.1:45174` for app agent 1 and
`http://static-1.localhost:8001` for static agent 1.

Useful dry-run:

```bash
kit/scripts/devkit --dry-run -p dev-all layout-apply \
  --file kit/examples/orchestration-ouro8-static-devall.yaml \
  --skip-broker --skip-ready
```
