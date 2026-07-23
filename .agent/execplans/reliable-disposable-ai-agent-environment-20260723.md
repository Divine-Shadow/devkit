# Reliable Disposable AI-Agent Environment

This is the active implementation record for replacing the VM-first fixture
diagnostic with a production-faithful executable gate and then proving the
complete disposable environment. Planning supports delivery and is not an
admission artifact.

## Purpose

An operator must be able to obtain a clean governed Product GUI agent through
one package-owned source/Nix/Fleet path, use it, destroy it, and repeat without
station knowledge. The named QEMU diagnostic executes the production adapter,
Git/OpenSSH, proxy, supervisor, Codex app-server, thread/MCP, and teardown
paths from absence. It remains feedback; only the complete Fleet/Colmena and
Desktop-governed lifecycle can promote the environment.

## Progress

- [x] Reconstructed a clean Devkit checkout from `origin/master`
  `1a6527323a9ee90871d6e455e0a66ba7cf103254`.
- [x] Discarded the effect-free speculative matrix-era clone.
- [x] Identified the current VM-first fixture-equivalence defects.
- [x] Implement a compiled/hermetic Nix lifecycle regression using production
  executables and code paths from absent consumer state.
- [x] Make sabotage results typed and exercise deterministic teardown while a
  real managed app-server proxy session remains open.
- [x] Move GUI SSH admission to an immutable manifest-selected Nix artifact and
  use deterministic, explicitly test-only SSH fixture material.
- [x] Pass the compiled Go package tests and `git diff --check`.
- [x] Replace the unusable nspawn runner with a restricted-network QEMU node;
  the available builder declares the required `kvm` and `nixos-test` features.
- [x] Dispose of the first QEMU attempt after its single typed failure:
  unprivileged supervisor startup could not read `/proc/1/ns/user`.
- [x] Reject the mount-spoofable `/proc/self/uid_map` repair after independent
  review.
- [x] Put the four unprivileged Product entrypoints behind package-owned NixOS
  setuid wrappers. Their first action proves initial user/mount namespaces,
  drops to the caller uid/gid with no capabilities, and only then consumes the
  manifest or caller request. Direct and caller-namespace launches are negative
  lifecycle cases.
- [x] Bind Product and controller accounts to no subordinate uid/gid ranges;
  validate every real/effective/saved/fs uid/gid and capability set on every
  long-lived supervisor thread. Offline Git seeding uses a controller-only
  wrapper and verifies the controller UID from the same manifest.
- [x] Pass `native-absent-index-construction` with the exact compiled Product
  packages after the namespace-entry repair.
- [x] Dispose of the next QEMU attempt after its single source failure:
  OpenSSH `StrictModes` correctly rejected a test-VM shared-store parent.
  Preserve immutable manifest-selected keys and have one root-owned,
  username-closed Nix command present those exact keys to sshd.
- [x] Close the hostile-PATH runtime defect exposed before the next VM:
  `dev-all-runtime-tools` projected the dev-shell's `openssh` development
  output but not its executable output. Select package-owned OpenSSH explicitly;
  the runtime inventory, full Go suite, source interface, immutable OpenSSH
  authority, and absent-index construction checks now pass.
- [x] Remove a false promotion-shaped postcondition from the diagnostic. The
  deterministic MCP fixture now proves only typed tool-call transport; Product
  readiness no longer invents governed admission or run/tree identities.
  Actual governance admission remains a required Product/Fleet promotion
  effect.
- [x] Dispose of QEMU attempts that exposed fixture `StrictModes`, authority
  lock, memory, account/forced-command, and held-session teardown defects; add
  the corresponding production-path regressions instead of repairing a VM.
- [x] Obtain independent source-audit approval for the diagnostic's honest
  boundary: compiled Devkit source acquisition, session, app-server protocol,
  fixture MCP transport, and teardown only. It does not claim a real governed
  run or Product promotion.
- [x] Dispose of the first exact-tree QEMU attempt after it proved that the
  test harness had pre-created the supposedly absent consumer parent through
  the controller account home. Move that home outside the consumer root.
- [x] Pass the named QEMU boundary diagnostic on the corrected exact Devkit
  tree for two fresh consumers and deterministic teardown. Its deterministic
  MCP fixture is not a real governed run and cannot promote the environment.
- [x] Fix the repository-level Nix dependency exposed by full flake evaluation:
  pin Node 22 for the npm tools derivation and declare `node-gyp`, so `sharp`
  builds from source without ambient npm tooling.
- [x] Pass the exact-tree runtime-tools build, all 20 checks in `nix flake
  check --show-trace`, the focused Go tests, and both staged/unstaged diff
  checks after the corrected diagnostic.
- [ ] Publish the accepted Devkit source layer on trunk.
- [ ] Compose the published Devkit source layer with exact Product source in
  the authoritative WSL/Nix derivation. Its promotion check must use the
  resulting real governance runtime and Fleet task/access path; it must not
  promote the Devkit MCP fixture, a direct ephemeral thread, or a prebuilt
  fixture manifest.
- [ ] Run that unchanged composed source through two fresh complete VM
  lifecycles, deploy the identical closure through Fleet-selected Colmena, and
  prove the Shadow governed-Scala boundary.

## Milestones

1. Refactor lifecycle orchestration so the same compiled entrypoints and
   authority-loading, Git/OpenSSH, proxy, checkout, supervisor, app-server, and
   teardown code run in the hermetic gate and VM. Test data may be synthetic;
   executable authority and effect paths may not be.
2. Replace alternate locators, weakened SSH, shared/dummy credentials, mocked
   source, and stderr/grep oracles with immutable selector inputs, distinct
   credentials, real OpenSSH, typed results, and bounded effect projections.
3. Use the Devkit QEMU diagnostic only to localize kernel, namespace, sshd, and
   teardown behavior. It must not advertise its MCP fixture as Product
   governance admission or its synthetic manifest as authoritative Product
   composition.
4. In the authoritative WSL/Nix composition, make the exact acquired Product
   source feed the one Product derivation and manifest. Exercise Fleet access
   convergence, real task start, real governance admission, and teardown as one
   lifecycle. Review the exact tree, then run two fresh VMs from unchanged
   source. On failure dispose of the VM, repair source, strengthen the cheap
   regression, and rerun.

## Acceptance

The cheap gate must execute production binaries and paths, not mocks, alternate
builds, or look-alike adapters, and prove both consumers start absent and leave
no process, socket, credential, worktree, home, or temporary source residue.
Final acceptance remains two complete fresh VM lifecycles plus byte-identical
canonical deployment and a fresh Shadow governed Scala task.

## Decisions

- Phase analysis guides executable tests; it is not a gate.
- The existing fixture-only VM check will retain only genuinely VM-specific
  assertions after its production-path logic is exercised cheaply.
- One active writer owns Devkit changes; read-only reviewers may work in
  parallel.
- GUI SSH admission is derived once in Nix and shared by the manifest and sshd;
  see
  `docs/decision-framework/governance_decisions/20260723_immutable_product_gui_ssh_admission.md`.
- A Product process cannot establish its own namespace provenance from paths in
  its caller-controlled mount namespace. The package-owned NixOS entry wrapper
  supplies only the brief initial-root privilege needed to compare kernel
  namespace handles; the Product binary drops every uid/gid/capability before
  opening its authority manifest or parsing an effect request.
