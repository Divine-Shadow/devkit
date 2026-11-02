# Standalone native Devkit

The retained Devkit Git repository and committed flake.lock supply the CLI,
runtime tools, bubblewrap and guarded Postgres broker. Management/Fleet
repositories, controller profiles, callback services and prior agent state
are not prerequisites.

Build from a clean retained checkout, and pin the Product clone to the
intended accepted commit:

```bash
runtime=$(nix build --no-link --print-out-paths .#devctl)
workspace=/absolute/path/to/fresh-workspace
mkdir -p "$workspace/agent-worktrees/agent1"
git clone git@github.com:Divine-Shadow/ouroboros-ide.git "$workspace/agent-worktrees/agent1/ouroboros-ide"
"$runtime/kit/bin/devctl" -p dev-all native exec --host-root "$workspace" --repo ouroboros-ide --index 1 --no-containers -- bash -c 'pwd; java -version'
```

This is the existing native entry. The explicit host root selects mutable
geometry; runtime authority remains the immutable package. Worktrees and
state default to agent-worktrees and .devkit/native-agents beneath that root.
Persistent user homes and conversation history retain their reset protections.

The sandbox uses bubblewrap, isolated worktree/home mounts, a read-only Nix
store, excluded Windows mounts, and a separate network namespace. Network
access uses the package's managed allowlist proxy. Container endpoints are
limited to the guarded broker. --no-containers selects a build-only sandbox without a broker
or container endpoint. Omit it for established guarded Postgres/Testcontainers
access. Ordinary broker acquisition uses Devkit's existing local lifecycle and
source-declared image policy. Explicitly composed station owners retain their
binding checks without becoming ordinary entry prerequisites.

For a fresh Product checkout run its normal generated-graph tasks before Peano:

```bash
"$runtime/kit/bin/devctl" -p dev-all native exec --host-root "$workspace" --repo ouroboros-ide --index 1 --no-containers -- bash scripts/sbt2 writeArtifactColumnV2Projections writeArtifactColumnSourceOwnershipJson
"$runtime/kit/bin/devctl" -p dev-all native exec --host-root "$workspace" --repo ouroboros-ide --index 1 --no-containers -- bash scripts/sbt2 "backend/runMain com.crib.bills.ouroboros.apps.dev.peano.Cli add -2 3"
```

Product supplies its own accepted artifact, review, CI and publication
interfaces. Devkit never selects a Product revision. Optional GUI configuration
uses /etc/devkit/codex-config-projections.json; non-GUI execution does not need it.

The default package is the runtime bundle (CLI and tools). The postgres-broker
package remains separately named. make ci-cheap runs source tests with their
Nix-supplied fixture tools and all flake checks.
