# Dev-workspace bwrap closure convergence

This living ExecPlan records the bounded repair that lets a GUI-owned
`dev-workspace` runtime launch nested source-derived native tasks without
losing its Nix-authored Codex policy or common shell compatibility. The
business consequence is that the lease, Terraform-security, and Product
objectives can run with `approval_policy = "never"` and
`sandbox_mode = "danger-full-access"` while retaining workspace-egress
filesystem and network isolation.

## Progress

- [x] Read the repository and fleet Devkit operating contracts.
- [x] Read back affected task rollouts and prove their Codex turn contexts
  already use `never` plus `danger-full-access`.
- [x] Reproduce the nested-launch failure: `/etc/codex/config.toml` and
  `.devkit/nix-codex-config.toml` are absent inside the deployed
  `dev-workspace`, although the current `$CODEX_HOME/config.toml` is a valid
  Nix-authored config.
- [x] Verify the pinned Go executable and `GOROOT` are both Go 1.22.4; no Go
  dependency repair is required.
- [x] Export the validated current Codex config as the authoritative source
  for nested Devkit launches from the `dev-workspace` flake.
- [x] Materialize `/usr/bin/bash` in both rendered and executed bwrap plans.
- [x] Chain a nested workspace-egress proxy through its healthy outer proxy and
  route authenticated Git SSH through CONNECT on `ssh.github.com:443`.
- [x] Add focused regression tests and run the relevant Go/Nix checks.
- [ ] Publish the source repair and produce a source/closure/readback receipt.
- [ ] Reactivate the preserved GUI objectives without replacing healthy
  homes, auth, or validated candidates.

## Surprises & Discoveries

- The apparent permission problem was not a lower Codex sandbox setting.
  Independent rollout readback showed the intended full-access settings.
- `workspace-egress` intentionally blocks direct network access; the native
  Unix-socket proxy is the supported Git/HTTP route. Direct SSH/DNS failure is
  therefore not an auth signal.
- The deployed Devkit checkout is deliberately over-mounted read-only. This
  repair is being developed in a clean local clone at the same source revision
  rather than mutating runtime authority in place.
- The Go mismatch observed in a management helper was not present in the
  actual `dev-workspace` flake: `go`, `$GOROOT/bin/go`, and `GOROOT` all resolve
  to pinned Go 1.22.4.
- A nested proxy could reach its Unix socket but returned 502 because it tried
  a direct TCP dial from inside the already isolated outer workspace. The
  repaired proxy preserves its own allowlist and chains only when the caller
  itself declares `workspace-egress`.
- Private HTTPS source correctly reached GitHub but had no non-interactive
  username. The authenticated repository route is SSH on
  `ssh.github.com:443` through the managed CONNECT proxy, using the already
  seeded per-agent key.
- The repo-owned Terraform flake cannot bootstrap an empty Terraform worktree;
  source acquisition uses the source-independent dev-workspace runtime first,
  then hands the populated repo to the Terraform runtime.

## Decision Log

- Preserve healthy home/auth/runtime closure. Replace a layer only after one
  bounded failing canary implicates it.
- Keep authority precedence as Nix system config, synchronized cache, then an
  explicitly exported current controller config. Do not synthesize policy or
  accept an unmarked mutable target config.
- Scope the config-source export to `overlays/dev-workspace/runtime.nix`; other
  runtimes retain their existing authority behavior.
- Add `/usr/bin/bash` as a compatibility symlink to the same immutable system
  Bash already exposed at `/bin/bash`.

## Implementation Milestones

1. Add a guarded `DEVKIT_CODEX_CONFIG_SOURCE` export to the dev-workspace
   shell hook only when `$CODEX_HOME/config.toml` is readable.
2. Add `/usr/bin/bash` to `plan.launcherArgs` and `launch.BuildBubblewrap` and
   assert both plan and execution rendering.
3. Build `kit/bin/devctl`, run focused runtime plan/launch tests, evaluate the
   dev-workspace shell, and execute a nested canary proving config policy,
   Bash path, Go identity, workspace visibility, and proxy-routed source
   access.
4. Publish the validated commit, reconverge only the implicated runtime layer,
   and attach the receipt to the preserved GUI tasks.

## Acceptance

- A source-derived `dev-workspace` shell exports a readable
  `DEVKIT_CODEX_CONFIG_SOURCE` whose contents retain the Nix marker, OpenAI
  profile, `approval_policy = "never"`, and
  `sandbox_mode = "danger-full-access"`.
- A nested native prepare/exec succeeds without `/etc/codex/config.toml` or a
  pre-existing `.devkit/nix-codex-config.toml`.
- Both generated plan JSON and the actual bwrap command create
  `/usr/bin/bash` without broadening filesystem or network policy.
- Pinned Go remains self-consistent, required workspace sources are visible,
  and Git/HTTP egress succeeds only through the managed proxy route.
- Existing tests plus focused new regressions pass; the three business owners
  resume from clean source and the preserved Joe candidate remains byte exact.

## Outcomes & Retrospective

Validation completed before publication:

- `go test ./...` passed across the complete `cli/devctl` module.
- `make -C cli/devctl build` produced the canonical `kit/bin/devctl`.
- `nix flake check --no-build` evaluated every x86_64 package, shell, app, and
  check successfully.
- A real nested dev-workspace canary read back full-access Codex policy,
  `/usr/bin/bash`, and pinned Go 1.22.4, then resolved both public HTTPS Git and
  authenticated private Terraform Git. Private source HEAD was
  `cabbf8bbd8c4eec871041faf77a794885c1e8c24`.

Publication and task reactivation remain pending.
