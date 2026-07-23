Devkit Agent Notes

Principles
- Single path for ordinary non-Product overlays: use `kit/scripts/devkit` as the canonical entrypoint. It execs the compiled CLI at `kit/bin/devctl`.
- Product exception: Product source acquisition, consumer construction, and GUI lifecycle must consume the installed manifest-bound adapter from the sole `fleet-runtime-authority/v1` manifest. Raw `kit/scripts/devkit`, direct `devctl`, and native lifecycle subcommands are not Product authority or Product promotion paths.
- Before an expensive Product VM, repair the known lifecycle dependencies as
  one source batch and exercise them through compiled/hermetic checks that use
  the production code path. Plans and prose are never implementation or
  promotion gates. Reserve the VM for properties that require its kernel,
  systemd, storage, network, GUI, or governance boundary.
- Product lifecycle evidence must execute the production-compiled entrypoints.
  Test data may be synthetic, but alternate authority locators, weakened SSH,
  shared credentials, mocked acquisition, ambient helpers, stderr oracles, and
  test-only effect paths cannot promote the runtime.
- No fallbacks: wrappers must not silently fall back to alternative scripts or binaries. If the binary is missing, fail loudly with a clear message and instructions to build it.
- Minimal wrappers: keep shell wrappers as thin exec shims only; no hidden logic.

Canonical flow
- Build once: `make -C devkit/cli/devctl build` (outputs `devkit/kit/bin/devctl`).
- Run ordinary non-Product overlays with `devkit/kit/scripts/devkit …` (this calls the binary). Direct invocation is diagnostic only and never establishes Product authority.
- Run Product only through the installed authority manifest and its exact adapter/launcher paths. The Devkit consumer-boundary lifecycle is a prerequisite diagnostic; only the governed Product-owned twice-fresh promotion app may promote the resulting runtime.
- Do not start the expensive VM diagnostic until the production-path
  compiled/hermetic lifecycle regression passes from absent consumer state and
  the unchanged source/test tree has received ordinary review. Then run the VM
  promptly; do not add another analysis or paperwork gate.

Break-glass behavior
- If `kit/bin/devctl` is missing or not executable, the wrapper must exit non‑zero and print build instructions. Do not add alternative code paths.

Why no fallbacks?
- Multiple code paths cause confusion and mask failures. A single, enforced path ensures errors are obvious and fixes are straightforward.
