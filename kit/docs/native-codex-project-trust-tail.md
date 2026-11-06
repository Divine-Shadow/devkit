# Native selected Codex config: project trust preservation

The selected GUI source remains the immutable base. Native preparation preserves
an existing owned config.toml only when its exact base bytes are followed by
the closed Codex-managed project extension: one trust_level = "trusted" or
"untrusted" declaration for each canonical absolute project path. This is
preservation of already present operator/Codex state, not a new trust decision.

Project paths are decoded using the TOML basic-string escape subset. Invalid
UTF-8, control paths, relative/noncanonical paths, Go-only escapes, missing or
duplicate fields, and duplicate decoded project paths (including those already
in the immutable base) receive no preservation credit. There is no plugin-tail
contract in this Devkit seeder. Unknown settings and protected base/MCP drift
retain ordinary atomic replacement from the validated source.

The preservation branch performs no config write, chmod or replacement. The
existing handle-relative verifier reopens without following links and checks the
same device/inode, owner, mode 0600 and exact observed bytes. Unsafe metadata
refuses; no symlink, owner or path guard is relaxed. Ordinary exact-source
materialization and replacement retain their existing behavior.

## Repair scope and evidence

This bounded Source correction follows the actual selected frontend constructor
that would otherwise overwrite a legitimate projects tail. SeedCodexConfig
reads the plan's validated immutable GUI source after native/backlink/worktree
validation; its original seeder block was unchanged from published Devkit250.
The real-filesystem preservation regression failed on the unrepaired seeder with the expected
mutation finding. The repaired focused suite passes both preservation and
protected/unknown-drift controls, TOML/duplicate negatives and existing filesystem
guard tests. Full owning tests and normal Source CI/publication are recorded in
the issuing handoff; focused checks do not claim those later stages.

No live HOME, trust setting, profile, registry, parent or constructor was changed
by authoring/testing this repair. Future composition must select the qualified
new Devctl explicitly; an existing running consumer does not silently receive
this change. Selecting only Devctl can retain the separately selected Devkit
runtime/tools/shell and its exact immutable profile/projection.
