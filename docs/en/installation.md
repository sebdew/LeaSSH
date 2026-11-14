**English** · [Français](../fr/installation.md)

# Installation

From zero to a first agent execution in under 10 minutes.

## 1. Prerequisites

- A Linux host (amd64 or arm64) with Docker, **or** the single `leassh` binary.
- A domain name and a **TLS reverse proxy** (Caddy, nginx, Traefik…): the interface cookies are `Secure` and remote agents must reach `/mcp` over HTTPS.
- SSH access to the servers you want to drive (you will add a public key to them).

## 2. Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `LEASSH_MASTER_KEY` | — (**required**) | 32 bytes in base64 (`openssl rand -base64 32`). Encrypts SSH private keys and all secrets. |
| `LEASSH_LISTEN` | `:8080` | Listen address (interface and MCP). |
| `LEASSH_DATA_DIR` | `/data` | Folder of the SQLite database (`leassh.db`). |
| `LEASSH_SETUP_TOKEN` | generated at start | Setup token for a fresh instance; printed once in the logs if not provided. |
| `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` | empty | Defaults for the audit report's AI summary (optional). |

Everything else (public URL, trusted reverse proxies, session lifetime, log retention…) is set in the interface, **Settings → General**.

> **Keep `LEASSH_MASTER_KEY` outside the database backup.** Without it, SSH private keys and secrets cannot be recovered.

## 3. Docker

```bash
export LEASSH_MASTER_KEY="$(openssl rand -base64 32)"
echo "$LEASSH_MASTER_KEY"   # store it in your password manager

docker run -d --name leassh --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v leassh-data:/data \
  -e LEASSH_MASTER_KEY \
  gitea.dewarlez.fr/sebastien/leassh:latest
```

The image is distroless-based, runs as a non-root user (65532) and exposes port 8080. The port is published on `127.0.0.1` only: public access goes through the reverse proxy.

### Docker Compose

Create a `.env` file next to it (it must **never** be committed):

```
LEASSH_MASTER_KEY=<output of openssl rand -base64 32>
LEASSH_SETUP_TOKEN=<output of openssl rand -base64 24>
```

```yaml
services:
  leassh:
    image: gitea.dewarlez.fr/sebastien/leassh:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - leassh-data:/data
    environment:
      LEASSH_MASTER_KEY: ${LEASSH_MASTER_KEY:?set LEASSH_MASTER_KEY in .env}
      LEASSH_SETUP_TOKEN: ${LEASSH_SETUP_TOKEN:-}
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
volumes:
  leassh-data:
```

```bash
docker compose up -d
docker compose logs leassh   # setup token if you did not set one
```

## 4. Binary and systemd

```bash
# no release published yet: build the binary (make build), then install it
sudo install -m 0755 bin/leassh /usr/local/bin/leassh
sudo useradd --system --home /var/lib/leassh --create-home --shell /usr/sbin/nologin leassh
```

`/etc/leassh/env` (mode `0600`, owned by `root`):

```
LEASSH_MASTER_KEY=…
LEASSH_DATA_DIR=/var/lib/leassh
LEASSH_LISTEN=127.0.0.1:8080
```

`/etc/systemd/system/leassh.service`:

```ini
[Unit]
Description=Leassh (controlled SSH access for AI agents)
After=network-online.target

[Service]
User=leassh
EnvironmentFile=/etc/leassh/env
ExecStart=/usr/local/bin/leassh serve
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/leassh
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now leassh
journalctl -u leassh | head      # setup token
```

Building it yourself: `make build` (Go ≥ 1.26, Node ≥ 20) produces `bin/leassh`, which embeds the interface.

## 5. Reverse proxy

Leassh must be served over **HTTPS**. Two important points: turn off buffering for the live log stream (SSE) and forward `X-Forwarded-Proto`.

### Caddy

```
leassh.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1        # live log stream (SSE)
    }
}
```

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name leassh.example.com;
    # … certificates …
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    location = /api/v1/executions/stream {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;          # SSE
        proxy_read_timeout 1h;
    }
}
```

Then tell Leassh which proxy to trust for clients' real IP addresses, in **Settings → General → Trusted reverse proxies** (e.g. `127.0.0.1`, or the proxy's Docker network). Without this, `X-Forwarded-*` headers are ignored. Also set the **Public URL** (e.g. `https://leassh.example.com`): it is used for notification links and the SSO return URL.

## 6. First start

Open the URL: the setup screen asks for the **setup token**, a username and a password (10 characters minimum). See [Getting started](getting-started.md).

## 7. Updating and removal

See [Operations](operations.md): always back up before updating.

## 8. Preparing the target servers

- Create a dedicated user rather than `root`, with only the rights the catalogue commands need (e.g. a limited `sudoers`).
- Commands go through an SSH `exec` channel: never an interactive shell, a PTY or port forwarding. The provided packs do not use `sudo`; adapt them to your environment. MySQL/PostgreSQL credentials are read on the target server, never sent by Leassh.
- To constrain further, prefix the public key in `authorized_keys`: `restrict,from="<Leassh IP>" ssh-ed25519 AAAA…`.
- **Behind a bastion (Warpgate…)**: several servers share the same host and port, distinguished by the SSH user. Fill in the server's *SSH user* field (e.g. `ia:exact target name` for Warpgate). Warpgate blocks an IP after several failures: Leassh therefore pauses attempts for 60 s on a server whose authentication just failed (`ssh_cooldown`).
- If a server's fingerprint changes (reinstall), Leassh **blocks** the server: check the fingerprint before choosing "Accept the new key".

---

[← Previous : Overview and concepts](overview.md) · [Home](README.md) · [Next : Getting started →](getting-started.md)
