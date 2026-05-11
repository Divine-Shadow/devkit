Codex Logout Revocation Retrospective

Date: 2026-04-28

Context
- The `dev-all` / `ouro8` workflow was using a shared host Codex auth mount as the reseed source for every agent.
- Agents 1 and 2 were intended to use distinct ChatGPT credentials.
- The operator workflow copied `auth.json` between host and agent homes, then used logout/login locally to switch accounts before reseeding the next agent.

What Changed
- Codex CLI logout now revokes managed ChatGPT OAuth tokens before removing local auth state.
- The relevant upstream change is `[codex] Revoke ChatGPT tokens on logout (#17825)`, commit `22f7ef1cb7`, dated 2026-04-16.
- The logout path calls `logout_with_revoke`, which posts the stored refresh token to `https://auth.openai.com/oauth/revoke`, then clears local auth.

Observed Failure
- Agent auth files remained present and byte-stable, but `codex exec` failed with `401 Unauthorized` / `token_invalidated`.
- Reseeding from the shared host mount did not repair the agent if the copied refresh token had already been revoked server-side.
- Copying the same `auth.json` into multiple places preserved invalid credentials; it did not create independent credentials.

Root Cause
- Devkit treated host `~/.codex` as a reusable credential source.
- That assumption was acceptable when logout only deleted local files.
- After Codex began revoking OAuth tokens on logout, the operator sequence "logout account A, login account B, copy files" invalidated account A's refresh token even if the earlier `auth.json` had been saved elsewhere.

Immediate Operator Guidance
- Do not run `codex logout` to switch between accounts whose credentials need to remain usable.
- Use separate `CODEX_HOME` directories when performing separate logins:
  - `CODEX_HOME=/abs/path/to/codex-home-agent1 codex login --device-auth`
  - `CODEX_HOME=/abs/path/to/codex-home-agent2 codex login --device-auth`
- Copy or seed each resulting Codex home into the matching agent home.
- Treat `CODEX_HOME` as the Codex home directory itself, not a parent directory containing `.codex`.

Devkit Implications
- `codex-auth reseed` and `reseed-all` are unsafe for multi-account use when they only copy from `/var/host-codex/auth.json`.
- Agent homes must be seeded from explicit per-agent sources, not from whichever identity most recently occupied host `~/.codex`.
- Logging and debug commands should show source identity by path or slot name only; never print token contents.
- Verification should remain behavioral: run `codex exec "reply with exactly Ok"` in the target agent after seeding.

Long-Term Fix
- Implement or finish an opt-in credential pool:
  - Each slot is a complete Codex home for one identity.
  - The pool is mounted read-only.
  - Agent N maps to an explicit slot or configured source.
  - Agent-local refresh writes stay inside the agent home, never back to the pool.
- Extend reseed commands to accept a source slot/path or to operate in pool mode.
- Make host `~/.codex` seeding the single-account default only, not the documented multi-account path.

Takeaway
- OAuth credential files are not durable secrets once logout revokes the server-side token.
- Multi-agent auth must model identities as separate credential homes from the start.
