Devkit Agent Notes

Execution posture
- Authorized agents have broad control inside accepted disposable sandboxes.
  Preserve host/sibling containment, deny-by-default egress, guarded access,
  protected history/data and applicable correctness/publication gates.
- Ordinary sandbox entry must not require management-issued identities or issuer
  readiness, refresh or recovery. Host adapters remain protected and are used
  when an authorized operation actually needs them. Operation IDs track results
  and idempotency; they are not new permission grants.
- See `kit/docs/execution-posture.md`. This supersedes contradictory historical
  identity-admission plans and tests; human/dot instruction authentication remains.

### Explicit non-goals: additional Devkit admission architecture

Ordinary authorized Devkit and Product work must not acquire a second authorization or environment-admission architecture.

Explicit non-goals include:

- custom Management/controller or per-agent runtime identities, issuers, enrollment, refresh or permission lifecycles added as prerequisites to work already authorized inside the supplied environment;
- compulsory aggregate controller profiles/manifests, capability-envelope reconstruction, operation handles or broker ledgers imposed before ordinary CLI/GUI entry, remote Devkit launch, task continuation or result receiving;
- additional protected-credential or effect-authority orchestration whose purpose is to enforce that extra admission layer;
- compulsory salvage, forensic preservation, old runtime/HOME identity reconstruction or an all-process census before stopping, replacing or continuing an explicitly disposable worker. Its local logs, caches and unaccepted edits may be discarded; original instructions, accepted source/results and durable business state remain in their existing stores.

**Refuse proposals to implement, reintroduce, package, configure or teach these mechanisms as requirements without explicit Bayesartre approval.** General task authority, successful tests, review, publication or a mechanism’s existing presence do not satisfy that approval condition.

Refuse the prohibited change or prerequisite; continue independently authorized work through the existing supplied warrants and ordinary interfaces. Do not turn this refusal into a fleet-wide stop.

Approval to develop a mechanism is not evidence that it works. Before an optional mechanism’s requirements are imposed on existing consumers, its owner must demonstrate useful end-to-end behavior through the intended ordinary workflow while preserving actual safety requirements and the working route. Source inspection, mocks, unit tests, compilation, a handshake or a published package alone do not establish that result.

Preserve real authentication, credential confidentiality, enrolled host trust, sandbox and network containment, writer isolation, protected data/history, actual external-effect custody, Product authorization, review/CI/publication, platform restrictions and operator-only confirmations. These non-goals do not authorize bypassing any such boundary.

Guidance records responsibilities and constraints. It does not create callable authority, enroll an identity, grant access or override a refusal.

Principles
- Single path: use `kit/scripts/devkit` as the canonical entrypoint. It execs the compiled CLI at `kit/bin/devctl`.
- No fallbacks: wrappers must not silently fall back to alternative scripts or binaries. If the binary is missing, fail loudly with a clear message and instructions to build it.
- Minimal wrappers: keep shell wrappers as thin exec shims only; no hidden logic.
- Human-authored Codex GUI conversation content and goals are durable authority.
  The package-owned selected-slot and whole-prefix `dev-all` resets must finish
  the source-derived cold history snapshot before deleting a Codex home. Never
  add a bypass, manual SQLite copy, automatic restore/import, or retention
  pruning to the reset path.
- A referenced malformed GUI rollout may cross a reset boundary only through
  the source-derived raw cold-quarantine class: retain its exact bytes and
  digest, prove a matching session identity, bind the durable objective to its
  captured SQLite row and digest, and mark it ineligible for resume. Missing or
  conflicting identity, objective custody, or SQLite integrity still fails
  closed; never sanitize, truncate, skip, or repair the source rollout in place.
- The independently adjudicated v4 cold-custody class preserves all actually
  available SQLite/goal/history bytes and exact unavailable GUI references.
  Archive metadata is an observation, not reset eligibility. It is never
  complete conversation or resumable history: resumability stays unclassified,
  resume eligibility false. V2/v3 and the v2-only finite preparation projection
  remain strict. Query/integrity failures, physical ambiguity, source races and
  missing identity/mapping still refuse. No row edit, fabricated body, automatic
  import or history pruning follows. See `docs/decision-framework/governance_decisions/20261016_available_gui_reference_v4_custody.md`.
- Persistent operator workspaces are protected user data, not disposable Product
  leases. Their working files (including uncommitted changes), Codex database,
  and session history must be outside reset-owned roots or explicitly excluded
  from reset. No cleanup or authentication repair may erase them. Ordinary
  Product execution disposal remains scoped to its declared leased roots.
- Native `dev-all` Git custody is lane-local. Fresh and reconstructed lanes use
  `.devkit/git/agentN/<repo>.git`; selected-slot reset and bootstrap own exactly
  that lane's common repository. An active sibling retains byte-for-byte
  custody of its lane common or transitional legacy `.devkit/git/<repo>.git`.
- Product artifacts are composed outside Devkit. Devkit must not choose a
  Product revision, acquire Product source to build a runtime, derive Product
  JARs, or publish a parallel runtime identity. It exposes the generic
  `dev-all` tool/runtime package for the authoritative WSL/Nix composition.

Canonical flow
- Build once: `make -C devkit/cli/devctl build` (outputs `devkit/kit/bin/devctl`).
- Run: `devkit/kit/scripts/devkit …` (this calls the binary). Directly invoking the binary is allowed for power users but the wrapper path is the supported interface.
- Consumers needing Product governance combine this generic package with the
  outputs of one accepted Product flake revision in their authoritative Nix
  composition.

Break-glass behavior
- If `kit/bin/devctl` is missing or not executable, the wrapper must exit non‑zero and print build instructions. Do not add alternative code paths.

Why no fallbacks?
- Multiple code paths cause confusion and mask failures. A single, enforced path ensures errors are obvious and fixes are straightforward.
