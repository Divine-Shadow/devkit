# Tech Debt: Layout Apply Resilience

## Context
The mixed layout fix exposed several fragilities in tmux orchestration and
native agent state seeding:
- `layout-apply` reseeded Codex credentials every time a window pointed at the
  same native agent home, wiping state for earlier windows that reused that
  agent (for example, `front-1` vs `front-2`).
- Agent resolution relied on shell loops with little observability when a
  window failed to appear.
- Layout YAML files provide no static validation that window indices map to
  distinct agents or that counts are sufficient.

## Suggestions
1. **Agent Resolution Helper**
   - Extract agent lookup, reuse tracking, and skip logic into a shared
     internal package so `layout-apply`, `tmux-sync`, and `tmux-add-cd` cannot
     drift.
   - Add structured debug logging (respecting `DEVKIT_DEBUG`) that records the
     resolved agent and whether seeding was skipped.
2. **Seeding Coordination**
   - Persist a marker file under the anchor home (e.g., `.devhome/.seeded`) so future execs can check locally instead of relying on CLI process memory.
   - Consider a lightweight host-side cache keyed by overlay, service, and
     agent index to persist across CLI invocations.
3. **Layout Validation Tooling**
   - Introduce `layout-validate` to check that `count` values cover all window
     indices, warn when multiple windows reuse the same agent, and highlight
     missing definitions before tmux commands run.
   - Extend `--dry-run` output to include the agent each window would target to
     simplify troubleshooting in CI.
4. **Observability / Metrics**
   - Capture wait/seeding durations and emit them under `DEVKIT_DEBUG=1` (or
     future telemetry hooks) to spot slow native launches or credential copy
     failures early.

## Proposed Owners
- DevKit CLI maintainers
- Platform DX / tmux tooling contributors

## Priority
Medium — windows now render correctly, but the above improvements would make the workflow more transparent and resilient, reducing future regressions and shortening debugging loops.

## Updates (2025-09-19)
- Agent resolution and seeding logic now lives in `internal/agentexec`, shared
  by `layout-apply`, `tmux-sync`, and `tmux-add-cd`.
- Anchor seeding skips when an agent-local marker (`$target/.codex/.seeded`) is
  present to avoid clobbering reused agents.
- `layout-validate` surfaces missing overlays, count mismatches, and duplicate
  window/agent pairs before tmux orchestration.
