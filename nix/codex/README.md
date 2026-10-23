# Codex 0.163 production package

`packages.${system}.codex` is the shared Devkit/WSL package. Devkit's runtime
shell selects this same output. WSL consumers should use the output directly,
rather than rebuilding an independent prebuilt-package derivation.

The non-flake `codex-source` input pins upstream commit
`83086188ae0614bd882818ff1aa23f3194962012` (`rust-v0.163.0`).
`web-search-json.patch` is the accepted three-file change from local source commit
`b3e1e01cd3304f4247fa79dc33a2032436131d43`; its SHA256 is
`bf6f08b425c94590c559494b6569c0b5542f6734b79773fb85ae370aa330aace`.
It removes the redundant flattened web-search `id`, preserving the canonical
outer item identity. Raw serialized start/completion regression checks cover
that identity and duplicate-key rejection. The strict governance parser is
unchanged. Public Rust callers constructing `WebSearchItem` must remove its old
`id` field; clients that previously decoded duplicate keys last-wins now receive
the canonical item ID.

The upstream `codex-rs/default.nix` remains the source builder. Its override uses
Rust 1.95.0, builds only `codex-cli`, and explicitly replaces its computed
`cargoDeps`. This local Cargo.lock normalizes exactly 160 workspace package
version labels from 0.0.0 to 0.163.0 without changing dependency revisions.
Its SHA256 is `cae541406f7ec43a418e126be2bcd5de00b243e7bbf6382570028727d99c1069`.
Git source hashes are fixed; the changed crossterm and rust-sdk trees were
prefetched at the exact Cargo.lock revisions with submodules enabled.

The complete official 0.163 bundle supplies code-mode-host, ripgrep, bubblewrap,
and voice resources. Only `bin/codex` is replaced by the patched source build.
The package checks its version and helper/resource layout during assembly.
`share/codex-source/source.json` binds the source revision, accepted patch,
normalized lock, Rust version, target, and official resource bundle; the sibling
`binary.sha256` records the actual executable digest. This is independent of the
older `codex-native-tool-witness` diagnostic package.

Qualification before source acceptance includes the full changed Codex crate
(155 tests passed) and full upstream `just fmt` (exit 0). An actual Nix component
build is also required. These checks do not establish WSL closure, runtime
adoption, or Product business acceptance. Those remain with their owning gates.
