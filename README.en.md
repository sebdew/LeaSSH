# Leassh

> **Give your AI agents SSH access… on a leash.**
> A catalogue of validated commands, clear permissions, and a log of everything that was run, by whom and where.

**Level: N3** (exposed or enterprise use) — rules of the [constitution](docs/standards/constitution.md) and the [review checklist](docs/standards/checklist-revue.md) (French) in [`docs/standards/`](docs/standards/). · [Version française](README.md)

![Leassh dashboard](docs/images/en/01-dashboard.png)

## Overview

Leassh is an open source **MCP** (Model Context Protocol) server that lets AI agents act on your servers **without ever giving them a shell**. The agent does not type commands: it **chooses** from a catalogue you wrote or approved, with typed and validated parameters. Every request — allowed, denied or waiting for human approval — is logged.

Filtering an agent's commands after the fact (blacklists, regex) can be bypassed: `rm` is blocked, but `find -delete`, `python -c`, `truncate` or `$( )` get through. Leassh reverses the logic:

| Classic approach | Leassh |
|---|---|
| The agent writes a command, we try to filter it | The agent picks a command from a catalogue |
| Blacklist (anything not forbidden goes through) | Allow-list (nothing goes through unless planned) |
| Free-form parameters | Typed, validated, then escaped parameters |
| Raw SSH logs | Business log: agent, server, command, parameters, reason, result, approver |

## Features

- 🗂️ **Command catalogue** with templates, typed parameters (`enum`, `int`, `service`, `path`, `identifier`, `regex`), risk level (`read` / `write` / `destructive`) and an AI-facing description; editor with preview and test.
- 🖥️ **Servers and SSH identities**: ed25519 key generation from the interface, **strict host key pinning**.
- 👥 **Groups** of servers and commands.
- 🔑 **MCP keys** per agent, never stored in clear, with expiry and rate limiting.
- 🛡️ **Access rules**: server groups + command groups + keys + profile (`RO`, `RW`), and an **access simulator**.
- ✋ **Human approval** of sensitive commands, from the interface (mobile included).
- 💡 **Proposals**: the agent can propose a command; a linter rejects dangerous templates, you approve.
- 📜 **Complete log** (executions and administration) with an **integrity chain**: filters, live view, CSV/NDJSON export, retention.
- 📦 **YAML command packs** (system, systemd, MySQL, PostgreSQL, nginx, Docker, pfSense, Proxmox, UniFi), importable and shareable.
- 🔐 **OpenID Connect SSO** (Keycloak, Entra ID, Authentik, Google…), PKCE, roles from groups ([ADR 0001](docs/adr/0001-authentification-sso-et-compte-local.md), French).
- 🔄 **Provisioning**: server synchronisation from a Warpgate bastion.
- 📡 **Syslog export** (UDP/TCP/TLS, RFC 5424, text/JSON/CEF) and **webhook** (optional HMAC signature, Mattermost and Slack compatible).
- 🧾 **ISO 27001 / HDS audit report**, optional AI summary.
- 🌍 **French and English** interface, light and dark themes, usable on mobile for approvals and the log.
- 📦 **A single binary** (or a Docker image), SQLite by default, `/health` and `/metrics`.

## Quick install

Prerequisites: Docker, a TLS reverse proxy in front of Leassh (Caddy, nginx…) and SSH access to the servers you want to drive.

```bash
export LEASSH_MASTER_KEY="$(openssl rand -base64 32)"
echo "$LEASSH_MASTER_KEY"   # keep it in a password manager

docker run -d --name leassh --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v leassh-data:/data \
  -e LEASSH_MASTER_KEY \
  ghcr.io/sebdew/leassh:latest

docker logs leassh   # setup token, printed only once
```

Open the URL, create the administrator account with the setup token, then follow the welcome checklist: SSH identity → server and fingerprint → command pack → access rule → MCP key → agent configuration.

> **Keep `LEASSH_MASTER_KEY`** (outside the database backup): it encrypts SSH private keys. Without it, those secrets cannot be recovered.

## Documentation

Full documentation is in the [`docs/`](docs/README.md) folder, in English ([`docs/en/`](docs/en/README.md)) and French ([`docs/fr/`](docs/fr/README.md)):

| | |
|---|---|
| [Overview and concepts](docs/en/overview.md) | [Detailed installation](docs/en/installation.md) |
| [Getting started](docs/en/getting-started.md) | [User manual, screen by screen](docs/en/user-manual.md) |
| [MCP agents and tools](docs/en/mcp-agents.md) | [Security](docs/en/security.md) |
| [SSO (Keycloak step by step)](docs/en/sso.md) | [Operations: backup, update, monitoring](docs/en/operations.md) |
| [Architecture](docs/en/architecture.md) | [API reference](docs/en/api-reference.md) |

## Connect an agent

Streamable HTTP MCP endpoint `https://leassh.example.com/mcp`, authenticated by `Authorization: Bearer <MCP key>`.

```json
{
  "mcpServers": {
    "leassh": {
      "type": "http",
      "url": "https://leassh.example.com/mcp",
      "headers": { "Authorization": "Bearer lsh_xxxxxxxxxxxxxxxx" }
    }
  }
}
```

Five tools: `list_servers`, `list_commands`, `run_command`, `get_execution`, `propose_command` ([details](docs/en/mcp-agents.md)).

## Security in brief

- No PTY, no interactive shell: SSH `exec` channels only.
- No free-text parameter; every value is validated then escaped.
- MCP keys can neither change the configuration nor approve anything.
- Secrets encrypted at rest (AES-256-GCM), never logged; passwords in argon2id.
- Pinned host keys; any mismatch blocks the server.

See [Security](docs/en/security.md) and [`docs/specs/SPEC.md`](docs/specs/SPEC.md) (French).

## Development

Go ≥ 1.26, Node ≥ 20.

```bash
make dev     # backend with reload + Vite (API proxy)
make test    # Go tests (incl. injection corpus) + Vitest
make lint    # go vet, gofmt, golangci-lint (incl. gosec), tsc --noEmit, eslint
make e2e     # Playwright (Chromium) journeys on the real binary, fresh database
make build   # build the front end then the single binary (bin/leassh)
make docker  # multi-architecture Docker image
go test -tags integration ./internal/sshexec -run Integration  # real sshd through Docker
```

Contributing: [`CONTRIBUTING.md`](CONTRIBUTING.md) (ticket, branch, merge request, review). History: [`CHANGELOG.md`](CHANGELOG.md). Versions: [SemVer](https://semver.org/), tags `vMAJOR.MINOR.PATCH`.

## License

[Apache License 2.0](LICENSE). You may reuse, modify and redistribute this project, including commercially, provided you **keep the license and the [`NOTICE`](NOTICE) file** and **credit the creator** (Sébastien Dewarlez) and the modifications made (§4).
