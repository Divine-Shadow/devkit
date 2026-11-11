# Codex 0.167.1 production package

`packages.${system}.codex` is the shared Devkit/WSL package. Devkit's runtime
shell selects this same output. WSL consumers should use the output directly,
rather than rebuilding an independent prebuilt-package derivation.

The non-flake `codex-source` input pins upstream commit
`0a94398fa60c4fac0e7659e4213a0a36e613dd46` (`rust-v0.167.1`).
`web-search-json.patch` ports the accepted three-file change from local source commit
`b3e1e01cd3304f4247fa79dc33a2032436131d43`; its SHA256 is
`b791ac32d6e2a5cc0b649ee29a2ecbaf018a127ec5b492a2c3d7bd03139887a2`.
It removes the redundant flattened web-search `id`, preserving the canonical
outer item identity. Raw serialized start/completion regression checks cover
that identity and duplicate-key rejection. The strict governance parser is
unchanged. Public Rust callers constructing `WebSearchItem` must remove its old
`id` field; clients that previously decoded duplicate keys last-wins now receive
the canonical item ID.

The upstream `codex-rs/default.nix` remains the source builder. Its override uses
Rust 1.95.0, builds only `codex-cli`, and explicitly replaces its computed
`cargoDeps`. This local Cargo.lock normalizes exactly 171 workspace package
version labels from 0.0.0 to 0.167.1 without changing dependency revisions.
Its SHA256 is `9efc26116852723f76540da85d9e4537979014f166c39494bef61e49a9cc0883`.
Git source hashes are fixed; the changed crossterm and rust-sdk trees were
prefetched at the exact Cargo.lock revisions with submodules enabled.

The complete official 0.167.1 bundle supplies code-mode-host, ripgrep, bubblewrap,
and voice resources. Only `bin/codex` is replaced by the patched source build.
The package checks its version and helper/resource layout during assembly.
`share/codex-source/source.json` binds the source revision, accepted patch,
normalized lock, Rust version, target, and official resource bundle; the sibling
`binary.sha256` records the actual executable digest. This is independent of the
older `codex-native-tool-witness` diagnostic package.

Qualification required before source acceptance: the complete changed `codex-exec`
crate tests (including raw JSONL identity regression), upstream Rust formatting,
and an actual Nix package build. The port and hashes have only static/prefetch
qualification until those commands execute. Normal Devkit full CI/publication
then precede Environment receiving and real shared-server governed-child use.
No WSL closure, runtime adoption or Product acceptance is claimed by this source.

MXC's unchanged generated-binding check uses its own upstream workspace lock.
The Nix vendor overlay supplies the exact `src/Cargo.lock` from pinned MXC
`b21120168d70befe12356411423266bfef168446` beside its crate, after checking that
its Windows version equals the actual Codex lock's MXC dependency and the
normalized MXC manifest requirement. This avoids selecting an unrelated Windows
version from the shared vendor-parent lock. The Codex dependency graph and MXC
binding guard remain unchanged; the original dependency source is immutable.
