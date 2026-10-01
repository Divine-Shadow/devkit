# Codex 0.159.3 and GPT-6.1 Sol runtime

Update official x86_64/aarch64 Codex archive pins to 0.159.3 and the existing
controller auth-refresh model contract to gpt-6.1-sol. Preserve effort, tier,
permissions, isolation, credential custody, resets and user histories.

The isolated local main starts at published master dc905d316e3d0637af5105774a51ad66f0dc816b.
Official release: https://github.com/openai/codex/releases/tag/rust-v0.159.3
Official model: https://developers.openai.com/api/docs/models/gpt-6.1-sol

- [x] Verify stable release and architecture-specific official archive digests.
- [x] Update source pins and existing auth-refresh compatibility contract.
- [x] Complete all 12 x86_64 flake checks and both-system evaluation.
- [x] Realize ordinary runtime bundle, tools and shell; verify actual executable.
- [ ] Review/publish through repository source workflow and consume in WSL/Nix.

No live activation is authorized by this plan. Parent coordinates affected
workloads and any activation while desktop/UI control is paused. Live model
catalog admission on the new server remains a distinct acceptance gate.

Preparation evidence: official archive hashes were realized, and the executable
reported `codex-cli 0.159.3`. Generated binary SHA-256:
`8bf204b36a2f6dd0dab73aa2f639892e67ef9ac8befccb4a05b1496ebf25c479`.
All declared overlay version fields and the Terraform doctor expectation now
match the realized package. Checks remain separate from deployment acceptance.
Final clean candidate checks and output paths are retained in OS temporary
logs `/tmp/fleet-refresh-task15-devkit-*`; no live selection changed.
