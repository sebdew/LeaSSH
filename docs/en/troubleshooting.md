**English** · [Français](../fr/depannage.md)

# Troubleshooting and FAQ

| Symptom | Lead |
|---|---|
| Start refused: `LEASSH_MASTER_KEY est obligatoire` | Variable missing, not base64, or not 32 bytes: `openssl rand -base64 32`. |
| Cannot sign in behind a proxy | The proxy must serve HTTPS and forward `X-Forwarded-Proto: https`, or set the `https://…` public URL in Settings → General (`Secure` cookies). |
| "Too many attempts" at sign-in | 5 failures per IP + account in 15 minutes. Check the **trusted reverse proxies** if all clients seem to share one IP. |
| The live log does not update | The proxy buffers: turn it off for `/api/v1/executions/stream` ([Installation](installation.md)). |
| SSO button error, or SSO account refused | See [SSO](sso.md) ("Common problems"); the exact reason is in the administration log (`user.sso_login_failed`). The local account remains a fallback (`?login`). |
| Server "host key mismatch" | Reinstall or attack: check the fingerprint, then *Accept the new key* ([Servers](servers.md)). |
| Server "unknown" | Test the connection and pin the host key. |
| An agent receives "server or command unavailable for this key" | **Deliberately generic** message. Use the [Simulator](simulator.md) and the [Log](log.md): the exact denial reason is there. |
| `rate_limited` (HTTP 429) | More than 60 tool calls per minute with this key: wait for `Retry-After`. |
| An execution stays `pending_approval` | It is waiting for a decision ([Approvals](approvals.md)); it expires after the configured delay (30 min by default). |
| `ssh_cooldown` in the log | 60 s pause after an authentication failure, so as not to trigger an IP block by a bastion. Any change to the server lifts it. |
| Warpgate: "SSH auth from blocked IP" | Unblock Leassh's IP in Warpgate. |
| An approved command can be run by nobody | Add it to a command group present in a rule ([Rules](access-rules.md)). |
| Lost administrator password | `leassh reset-password <username>` ([Operations](operations.md)). |
| `/metrics` answers 401 | Normal until a token is set (Settings → General); present it as `Authorization: Bearer`. |
| After a restore, servers no longer connect | `LEASSH_MASTER_KEY` is not the backup's one: private keys cannot be decrypted. |

## FAQ

**Can an agent get a shell?** No: `exec` channel only, never a PTY nor an interactive shell, and only catalogue commands.

**Can an agent approve its own request?** No: an MCP key can neither change nor approve anything; only a human `admin` or `approver` decides from the interface.

**Can a free-form command be written?** No: there is no "free text" parameter type. An administrator writes the template; the agent only supplies typed values.

**What if a third party copies the database?** Secrets are encrypted with the master key, which is not in the database; MCP tokens and passwords are hashes. Logs, however, are readable.

**Can PostgreSQL be used?** Not today: SQLite, one instance ([ADR 0003](../adr/0003-choix-de-la-stack-go-sqlite.md)).

---

[← Previous : API reference](api-reference.md) · [Home](README.md) · [Next : Repository layout and decisions →](repository-layout.md)
