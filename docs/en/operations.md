**English** · [Français](../exploitation.md)

# Operations: update, backup, monitoring

English translation of [`docs/exploitation.md`](../exploitation.md), the French reference versioned with the code. Update both in the same merge request.

## Updating

1. **Back up** (below): database migrations apply by themselves at start and are **one-way**.
2. `docker pull ghcr.io/sebdew/leassh:<version>` then recreate the container (`docker compose up -d`), or replace the binary and restart the service.
3. Check `GET /health`, the "leassh démarré" log line and `leassh version`.
4. **Settings → General → Verify integrity**.

## Rollback

There is no downward migration: you **restore the backup taken before the update**.

1. Stop the container (or service).
2. Put back the previous image or binary version.
3. Replace `leassh.db` with the pre-update backup (delete `leassh.db-wal` and `leassh.db-shm`).
4. Restart with the **same** `LEASSH_MASTER_KEY` and verify integrity.

Anything written between the backup and the rollback is lost: note it down first.

## Backup

Two items, to be kept **separately**:

1. the **database** (`leassh.db`): configuration, logs, *encrypted* secrets;
2. the **master key** `LEASSH_MASTER_KEY`: without it, SSH private keys are unusable.

```bash
# Docker
docker exec leassh /leassh backup /data/leassh-$(date +%F).db
docker cp leassh:/data/leassh-$(date +%F).db ./backups/

# Binary
LEASSH_DATA_DIR=/var/lib/leassh leassh backup /var/backups/leassh-$(date +%F).db
```

The command produces a **consistent** copy (`VACUUM INTO`, server running), mode `0600`, **without** the master key. Schedule it (cron, systemd timer), encrypt it and send it off the machine. Do not store the master key in the same place as the database.

## Restore

1. Stop Leassh.
2. Put the backup under the name `leassh.db` in the data folder.
3. Restart with the **same** master key.
4. **Settings → General → Verify integrity**, then test a server's connection (this proves secrets decrypt).

## Recorded restore test

| Date | Version | Result |
|---|---|---|
| 2026-11-11 | `dev` (branch `fix/11-documentation`) | **Passed**: fresh instance, `admin` account created, `leassh backup`, stop, copy of the backup into an empty folder, start of a second instance, sign-in with the original password, `POST /api/v1/integrity/verify` → `ok: true`. Not covered: secret decryption (no SSH identity in the test database). |

The detailed procedure and future tests are recorded in [`docs/exploitation.md`](../exploitation.md).

## Change the master key

Server **stopped**, after a backup:

```bash
LEASSH_MASTER_KEY="<old>" LEASSH_NEW_MASTER_KEY="$(openssl rand -base64 32)" leassh rotate-master-key
```

All secrets (private keys, webhook, SSO, syslog, AI, connectors) are re-encrypted **in one transaction**; nothing changes if one of them fails to decrypt. Then replace `LEASSH_MASTER_KEY` with the new value before restarting.

## Lost administrator password, SSO down

```bash
LEASSH_DATA_DIR=/data leassh reset-password <username>        # Docker: docker exec -it leassh /leassh reset-password <username>
LEASSH_DATA_DIR=/data leassh enable-local-login                # if the local password was disabled and SSO is unavailable
```

The new password is typed at the terminal (10 characters minimum), the account is re-enabled, its sessions are closed and the operation is written to the log (`user.password_reset_cli`).

## CLI commands

| Command | Purpose |
|---|---|
| `leassh serve` | Start the server |
| `leassh backup <file>` | Consistent database backup |
| `leassh rotate-master-key` | Change the master key |
| `leassh reset-password <username>` | Reset a password |
| `leassh enable-local-login` | Allow local sign-in again |
| `leassh healthcheck` | Probe used by the Docker image |
| `leassh version` | Print the version |

## Monitoring

- `GET /health` → `ok` (`/healthz` still accepted), used by the image's `HEALTHCHECK`.
- `GET /metrics` (Prometheus): **closed by default** (401 until a token is set in Settings → General). Present the token with `Authorization: Bearer <token>`. Also block `/metrics` at the proxy if Prometheus is on the same network.
- Metrics: `leassh_executions_total{status}`, `leassh_execution_duration_seconds`, `leassh_ssh_errors_total{kind}`, `leassh_pending_approvals`, `leassh_servers{host_key_status}`, `leassh_mcp_keys{state}`, `leassh_outbound_duration_seconds{target,result}` (outgoing call duration: `database`, `webhook`, `ai`, `idp`).

```yaml
scrape_configs:
  - job_name: leassh
    authorization: { credentials: "<metrics token>" }
    static_configs: [{ targets: ["leassh.example.com"] }]
    scheme: https
```

Useful alerts: `leassh_servers{host_key_status="mismatch"} > 0`; `leassh_pending_approvals > 0` for more than 10 minutes; `rate(leassh_ssh_errors_total[5m]) > 0`. Application logs are JSON on standard output, with no secret.

## Docker images

The `Dockerfile` pins Node 24 LTS, Go and distroless by `@sha256:` digest. To update them: `docker buildx imagetools inspect <image>:<tag>`, replace the digest, rebuild and test. Publishing: `echo $GHCR_TOKEN | docker login ghcr.io -u sebdew --password-stdin` then `docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/sebdew/leassh:<version> --push .`

## Sending logs to Wazuh (syslog)

**Settings → Syslog** sends executions (requests, denials, approvals, results), proposals and the administration log. Never a command output nor a secret.

1. On the server side, open a syslog receiver. Wazuh example (manager's `/var/ossec/etc/ossec.conf`): TCP + TLS is not natively supported by `<remote>`; use TCP or UDP, or put `rsyslog`/`stunnel` in front for TLS.

```xml
<remote>
  <connection>syslog</connection>
  <port>514</port>
  <protocol>tcp</protocol>
  <allowed-ips>192.0.2.0/24</allowed-ips>
</remote>
```

2. In Leassh: protocol `TCP`, server and port above, **JSON format**, facility `local0`, minimum level `information`. **Send a test message**, then check `/var/ossec/logs/archives/archives.log` (enable `<logall_json>yes</logall_json>` for the duration of the test).

3. Decoder and rules (`/var/ossec/etc/decoders/leassh_decoder.xml` and `/var/ossec/etc/rules/leassh_rules.xml`), to validate with `wazuh-logtest`: the RFC 5424 header may or may not be stripped by the pre-decoder depending on your version, adjust the `prematch` accordingly.

```xml
<decoder name="leassh">
  <prematch>"app":"leassh"</prematch>
</decoder>
<decoder name="leassh-json">
  <parent>leassh</parent>
  <regex type="pcre2">"event":"([^"]+)".*?"severity":"(\w+)"</regex>
  <order>leassh.event,leassh.severity</order>
</decoder>
```

```xml
<group name="leassh,">
  <rule id="100500" level="3">
    <decoded_as>leassh</decoded_as>
    <description>Leassh: $(leassh.event)</description>
  </rule>
  <rule id="100501" level="8">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^execution.denied$</field>
    <description>Leassh: command denied to an agent</description>
  </rule>
  <rule id="100502" level="12">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^audit.host_key.mismatch$</field>
    <description>Leassh: SSH host key mismatch (server blocked)</description>
  </rule>
  <rule id="100503" level="6">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^execution.pending_approval$</field>
    <description>Leassh: command waiting for human approval</description>
  </rule>
  <rule id="100504" level="5">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^audit\.</field>
    <description>Leassh: administration change</description>
  </rule>
</group>
```

With the **CEF** format, use your SIEM's CEF decoder; with **text**, a `regex` decoder on `^execution\.\w+ id=`. For rsyslog: `module(load="imtcp") input(type="imtcp" port="514")` is enough; with TLS, `StreamDriver.Mode="1"` with the authority provided to Leassh. If the server is unreachable, Leassh keeps up to 10,000 messages in memory and resumes by itself; the state and last error are shown in the screen.

---

[← Previous : Single sign-on (OpenID Connect)](sso.md) · [Home](README.md) · [Next : Architecture →](architecture.md)
