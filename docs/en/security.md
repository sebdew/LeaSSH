**English** · [Français](../fr/securite.md)

# Security model

This page describes what Leassh guarantees, how, and what it does **not** guarantee.

## Invariants (never broken by the code)

1. **A single execution path**: no SSH execution bypasses `executions.Run()`, which applies in order: permissions → parameter validation → rendering → hash check → approval decision → execution → log.
2. **No "free text" parameter type**: every value is validated against its type then POSIX-escaped before insertion.
3. **`exec` channel only**: never a PTY, a shell, agent forwarding, port forwarding or X11.
4. **Strictly verified host key**; a mismatch blocks the server.
5. **An MCP key can change nothing**: neither configuration, nor approvals, nor read another key's executions.
6. **Every `run_command` request is logged**, denials included.
7. **Secrets are never logged nor returned** by the API (private keys, passwords, tokens, webhook and SSO secrets).
8. MCP error messages **do not reveal the existence** of servers or commands not visible to the key.
9. MCP tokens stored **only as a hash**, compared in constant time.
10. **No local command execution** (`os/exec`) in the backend.

## Defences by layer

| Topic | Measure |
|---|---|
| Command injection | Allow-list, typed parameters, POSIX escaping, linter, corpus of 300+ type × payload combinations run through a real `sh -c` |
| Secrets at rest | AES-256-GCM with `LEASSH_MASTER_KEY` (outside the database) for private keys, webhook, SSO, syslog, AI and connector secrets; rotation with `leassh rotate-master-key` |
| Passwords | argon2id |
| MCP tokens | stored as hashes, only the prefix is displayed |
| Sessions | `HttpOnly`, `Secure`, `SameSite=Strict` cookie; `X-CSRF-Token` on mutations; expiry after 30 minutes of inactivity |
| Sign-in | 5 failures per IP + account in 15 minutes, cap of 50 per IP, counters in the database |
| Setup | `/setup` protected by a token |
| Headers | Strict CSP (`style-src 'self'`, no `unsafe-inline`), `Strict-Transport-Security` (left to the trusted proxy if declared) |
| Observability | `/metrics` closed until a token is set |
| MCP rate | 60 tool calls per minute per key |
| SSH | Pinned host key, 60 s pause after an authentication failure |
| Log | Hash chain (see below), not editable from the interface |
| SSO | Authorization Code + PKCE S256, `state`/`nonce`, verified ID token, account bound to `(issuer, sub)` |
| Supply chain | CI: gitleaks, Trivy, govulncheck, `npm audit`; images pinned by digest; Go 1.26.11 |

## The log's integrity chain

Each row of the execution and administration logs stores the previous row's hash. **Verify integrity** (Settings → General) detects altered content, a row deleted in the middle or a chaining break. **Limit**: truncation of the chain's end is only detected if the **chain head** was recorded elsewhere (it is in the audit report); an attacker who controls the database **and** the master key could recompute everything. For stronger proof, send logs to a SIEM (syslog).

## Known limits, to be aware of

- **Untrusted command output**: output is returned as-is to the agent. If it contains third-party-controlled text (logs of a public application, web pages), it can carry a **prompt injection**. The effect remains bounded by the catalogue, the key's rights and human approval of writes. Do not expose such a `read` command thoughtlessly.
- **Outgoing URLs** (webhook, AI, connectors, OIDC issuer): `http(s)` only, private addresses accepted (internal use), redirects not followed for the webhook and AI. Reserved for administrators: only give that role to trusted people.
- **Single instance**: one instance, no high availability.
- An SSO account removed from the provider can no longer sign in but **keeps its session until it expires**: disable it in Leassh for immediate effect.
- The network and virtualisation packs have not been run on real hardware.

## Deployment recommendations

1. Behind a **TLS reverse proxy**, port 8080 not exposed (`127.0.0.1`).
2. Declare your **trusted reverse proxies** and the **public URL**.
3. **SSO**, then disable the local password.
4. A **dedicated SSH user** per target, minimal rights, `restrict,from=` in `authorized_keys`.
5. One MCP key **per agent**, with an **expiry**; `RO` profile by default.
6. **Back up** the database and the master key **separately**, test restoration ([Operations](operations.md)).
7. Send logs to a **SIEM**, and alert on `host_key.mismatch`.

## Reporting a vulnerability

Do not publish a vulnerability in a public ticket: contact the maintainer directly (original repository stated in the `NOTICE` file).

---

[← Previous : MCP agents and tools](mcp-agents.md) · [Home](README.md) · [Next : Single sign-on (OpenID Connect) →](sso.md)
