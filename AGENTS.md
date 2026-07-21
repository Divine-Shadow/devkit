Devkit Agent Notes

Principles
- Single path for ordinary non-Product overlays: use `kit/scripts/devkit` as the canonical entrypoint. It execs the compiled CLI at `kit/bin/devctl`.
- Product exception: Product source acquisition, consumer construction, and GUI lifecycle must consume the installed manifest-bound adapter from the sole `fleet-runtime-authority/v1` manifest. Raw `kit/scripts/devkit`, direct `devctl`, and native lifecycle subcommands are not Product authority or Product promotion paths.
- No fallbacks: wrappers must not silently fall back to alternative scripts or binaries. If the binary is missing, fail loudly with a clear message and instructions to build it.
- Minimal wrappers: keep shell wrappers as thin exec shims only; no hidden logic.

Canonical flow
- Build once: `make -C devkit/cli/devctl build` (outputs `devkit/kit/bin/devctl`).
- Run ordinary non-Product overlays with `devkit/kit/scripts/devkit …` (this calls the binary). Direct invocation is diagnostic only and never establishes Product authority.
- Run Product only through the installed authority manifest and its exact adapter/launcher paths. The Devkit consumer-boundary lifecycle is a prerequisite diagnostic; only the governed Product-owned twice-fresh promotion app may promote the resulting runtime.

Break-glass behavior
- If `kit/bin/devctl` is missing or not executable, the wrapper must exit non‑zero and print build instructions. Do not add alternative code paths.

Why no fallbacks?
- Multiple code paths cause confusion and mask failures. A single, enforced path ensures errors are obvious and fixes are straightforward.
