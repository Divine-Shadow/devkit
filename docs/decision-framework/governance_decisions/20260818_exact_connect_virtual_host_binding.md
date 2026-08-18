# Bind exact CONNECT policy to TLS SNI

## Problem

An exact CONNECT authority plus fixed-IP dialing prevents a second DNS lookup,
but it does not by itself prevent an untrusted caller from naming one allowed
host and then selecting a different virtual host on the same provider or CDN
address. The EMDR workbench requires provider-only HTTPS egress without TLS
termination or a bespoke inference broker.

## Options

1. Enforce only the CONNECT authority and resolved public IP. This is the
   smallest conventional proxy, but a co-hosted virtual host remains an
   avoidable escape from the exact-host contract.
2. Terminate TLS and validate encrypted HTTP authority. This gives the proxy
   more visibility, but introduces certificate authority, plaintext, and
   application-protocol custody that v0 explicitly avoids.
3. Keep TLS opaque while parsing one bounded ClientHello and require exactly
   one SNI name equal to the canonical CONNECT authority. Selected.

## Selection rationale

The selected option preserves the explicit exact-host contract and makes the
co-hosted-address case mechanically testable. It retains end-to-end TLS and
keeps Devkit generic: the caller still owns the hostname allowlist. A strict
bounded parser adds more code than a plain tunnel, but correctness and
verifiability precede delivery speed in the workspace tradeoff value order.

This boundary does not inspect encrypted HTTP authority. For the v0
HTTPS-only, exact-provider destination set, binding CONNECT authority, DNS
result, fixed dial address, and TLS SNI is the narrowest useful enforcement
that does not become a TLS or inference broker. Encrypted ClientHello is not a
fallback: the presence of its extension fails closed even if an outer SNI
matches, because the effective name is not inspectable. Such a client must be
evaluated as an explicit future protocol change.

## Safety checks

- Only HTTP/1.1 CONNECT to port 443 is accepted.
- The first bounded TLS handshake must contain one exact matching SNI name;
  absent, duplicate, malformed, oversized, over-fragmented, or mismatched
  names close the tunnel before any client byte reaches the upstream socket.
- DNS must return a small all-public address set, and dialing uses only those
  literal addresses under one total timeout. IPv6 admission is constrained to
  `2000::/3` plus explicit reserved and unallocated exclusions rather than
  relying on `IsGlobalUnicast` alone.
- The externally owned allowlist is opened without following a final symlink
  and must have no write bits, matching its intended Nix-store custody.
- Unit and race tests cover co-hosted-IP mismatch, bounded fragmentation,
  shutdown, policy-file custody, socket custody, and connection limits.
- Nix closure checks reject unrelated network clients and secret-key material.
- Lifecycle-visible errors remain content-free.

## Rollback plan

Revert this change and repin Devkit in the authoritative WSL/Nix source. Do
not retain a local unpinned proxy binary. If the ClientHello gate blocks a
proven Codex provider flow, disable the EMDR profile while choosing and
recording a replacement boundary; do not silently fall back to plain CONNECT.

## Decision scope

This decision covers only Devkit's generic exact-allowlist HTTPS CONNECT
primitive. EMDR owns destination policy, WSL/Nix owns service composition and
vsock transport, and Management owns Fleet projection. It does not authorize
general network access, TLS interception, inference brokering, credential
handling, Terraform, or application deployment.
