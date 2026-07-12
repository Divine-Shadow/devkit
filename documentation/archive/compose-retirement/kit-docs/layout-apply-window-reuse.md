# Layout Apply Window Reuse Fix

This is a historical Compose-era note retained for migration context. It is not
supported runtime guidance. Current agent runtime operation uses Nix flakes and
the native devkit sandbox through `kit/scripts/devkit`.

## Summary

- Layouts that reuse the same container for multiple tmux windows now keep every
  window alive.
- Containers are resolved once per window, and mux commands call `docker exec`
  directly using the resolved name.
- Codex/SSH seeding is skipped when a container has already been prepared to
  avoid wiping credentials for earlier windows (for example, `front-1` versus
  `front-2`).

## Operational Notes

- `scripts/devkit -p dev-all layout-apply --file devkit/kit/examples/orchestration-ouro8-doh1-front2-devall1.yaml`
  produced the full window set (`ouro-1` through `ouro-8`, `doh-1`, `front-1`,
  `front-2`, `dev-all-1`) for the retired runtime.
- Direct `docker exec` lookups made tmux commands resilient to container index
  gaps and restarts in the retired runtime.
- Reapplying a layout was idempotent with respect to Codex credentials because
  reseeding was skipped for already-initialized containers.

## Historical Testing

```bash
scripts/devkit -p dev-all layout-apply --file devkit/kit/examples/orchestration-ouro8-doh1-front2-devall1.yaml
```
