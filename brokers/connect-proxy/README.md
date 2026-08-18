# Exact-allowlist CONNECT proxy

`devkit-connect-proxy` is a generic host-side transport primitive. It accepts
HTTP/1.1 `CONNECT` on one caller-supplied Unix socket, permits only exact DNS
names from one caller-supplied policy file, and connects only to public
addresses on TCP 443.

The proxy owns no product or EMDR policy. Wildcards, IP literals, private or
reserved DNS results, non-CONNECT methods, and non-443 ports fail closed. It
also requires the first bounded TLS handshake to contain exactly one SNI name
matching the CONNECT authority. This prevents a caller from using a
provider/CDN address for a different virtual host. It does not terminate TLS
or inspect the encrypted HTTP authority, and it does not log policy entries,
request targets, SNI names, or payloads.

The policy and socket paths must be absolute. The policy must be a canonical,
non-symlink regular file with no write bits (a Nix-store file satisfies this
contract). The socket parent must be a real directory that is not group- or
world-writable; startup refuses an existing socket path, and cleanup removes
only the socket inode the process created.

```text
devkit-connect-proxy --socket /absolute/proxy.sock --allowlist /absolute/allowlist.txt
```
