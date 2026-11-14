**English** · [Français](../fr/api.md)

# API reference

The administration interface uses a **JSON API** (`/api/v1/…`). The machine-readable specification is [`docs/openapi.json`](../openapi.json) (OpenAPI 3.0.3); every request and response body has its own per-resource schema; a test fails if a router route is missing from it, if a schema field differs from the matching Go type, or if a real API response does not match the specification. This page describes it route by route (105 operations). The MCP transport (`/mcp`) follows the MCP protocol: see [MCP agents and tools](mcp-agents.md).

## Conventions

- **Authentication**: `leassh_session` session cookie (obtained by `POST /api/v1/auth/login`, SSO or setup) and, for every mutation (`POST`, `PUT`, `DELETE`), the **`X-CSRF-Token`** header (the `csrf_token` value returned at sign-in). `/metrics` uses an `Authorization: Bearer` token.
- **Roles**: `admin` (everything), `approver` (decisions), read for any signed-in account. Permissions are always checked server-side (403 otherwise). "public" routes need no session.
- **Error format**: `{ "error": { "code": "…", "message": "…" } }`. The `code` never changes; `message` follows `Accept-Language` (fr or en).
- **Cursor pagination**: list `GET`s take `cursor`; the response carries `next_cursor`.
- **Dates** in ISO 8601 UTC; **identifiers** are ULIDs.
- **Secrets**: never returned (`has_client_secret`, etc.); an empty field on save keeps the existing value.
- **Log**: `GET /executions` filters: `period|from|to`, `status` (multiple), `key_id`, `server`, `server_group`, `command`, `risk`, `decided_by`, `q`. Same idea for `/audit` (`action` = prefix, e.g. `server.`). Exports `?format=csv|ndjson`.

### Example

```bash
# Sign in, then list servers
curl -c cj -H 'Content-Type: application/json' -d '{"username":"admin","password":"…"}' https://leassh.example.com/api/v1/auth/login
curl -b cj https://leassh.example.com/api/v1/servers

# Mutation: CSRF token required
curl -b cj -H "X-CSRF-Token: <csrf_token>" -H 'Content-Type: application/json' \
  -d '{"decision":"approve","comment":"OK"}' https://leassh.example.com/api/v1/executions/<id>/decision
```

## Routes

### Authentication and session

| Method | Path | Role | Description |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | public | Open a local session |
| `POST` | `/api/v1/auth/logout` | any signed-in role | Close the session |
| `GET` | `/api/v1/auth/me` | any signed-in role | Current user |
| `GET` | `/api/v1/auth/oidc/callback` | public | SSO provider callback (redirect) |
| `GET` | `/api/v1/auth/oidc/start` | public | Start SSO sign-in (redirect) |
| `POST` | `/api/v1/auth/password` | any signed-in role | Change your own local password |
| `GET` | `/api/v1/auth/providers` | public | Available sign-in methods |
| `PUT` | `/api/v1/auth/theme` | any signed-in role | Save the user's theme |

### Administration log

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/audit` | any signed-in role | Administration log |
| `GET` | `/api/v1/audit/export` | any signed-in role | Export the administration log |

### Audit reports

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/audit-reports` | admin | List audit reports |
| `POST` | `/api/v1/audit-reports` | admin | Generate an audit report |
| `POST` | `/api/v1/audit-reports/verify` | admin | Verify a report's integrity |
| `GET` | `/api/v1/audit-reports/{id}` | admin | Audit report detail |
| `GET` | `/api/v1/audit-reports/{id}/html` | admin | Audit report as HTML |
| `GET` | `/api/v1/audit-reports/{id}/json` | admin | Audit report as JSON (signed) |
| `POST` | `/api/v1/audit-reports/{id}/summary` | admin | Generate a report's AI summary |

### Executions and approvals

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/executions` | any signed-in role | Execution log |
| `GET` | `/api/v1/executions/export` | any signed-in role | Export the execution log |
| `GET` | `/api/v1/executions/stream` | any signed-in role | Real-time execution stream (SSE) |
| `GET` | `/api/v1/executions/{id}` | any signed-in role | Execution detail |
| `POST` | `/api/v1/executions/{id}/decision` | admin, approver | Approve or reject an execution |

### Servers

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/servers` | any signed-in role | List servers |
| `POST` | `/api/v1/servers` | admin | Create a server |
| `DELETE` | `/api/v1/servers/{id}` | admin | Delete a server |
| `GET` | `/api/v1/servers/{id}` | any signed-in role | Server detail |
| `PUT` | `/api/v1/servers/{id}` | admin | Update a server |
| `POST` | `/api/v1/servers/{id}/host-key` | admin | Pin the host key |
| `POST` | `/api/v1/servers/{id}/test-connection` | admin | Test the SSH connection |

### Server groups

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/server-groups` | any signed-in role | List server groups |
| `POST` | `/api/v1/server-groups` | admin | Create a server group |
| `DELETE` | `/api/v1/server-groups/{id}` | admin | Delete a server group |
| `PUT` | `/api/v1/server-groups/{id}` | admin | Update a server group |
| `PUT` | `/api/v1/server-groups/{id}/members` | admin | Replace a server group's members |

### SSH identities

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/identities` | any signed-in role | List SSH identities |
| `POST` | `/api/v1/identities` | admin | Create an SSH identity |
| `DELETE` | `/api/v1/identities/{id}` | admin | Delete an SSH identity |

### Provisioning

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/provisioners` | any signed-in role | List provisioning connectors |
| `POST` | `/api/v1/provisioners` | admin | Create a provisioning connector |
| `GET` | `/api/v1/provisioners/types` | any signed-in role | Connector types |
| `DELETE` | `/api/v1/provisioners/{id}` | admin | Delete a provisioning connector |
| `PUT` | `/api/v1/provisioners/{id}` | admin | Update a provisioning connector |
| `POST` | `/api/v1/provisioners/{id}/host-key` | admin | Pin a connector's host key |
| `POST` | `/api/v1/provisioners/{id}/preview` | admin | Preview a synchronisation |
| `POST` | `/api/v1/provisioners/{id}/sync` | admin | Synchronise servers |
| `POST` | `/api/v1/provisioners/{id}/test` | admin | Test a connector |

### Commands

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/commands` | any signed-in role | List commands |
| `POST` | `/api/v1/commands` | admin | Create a command |
| `POST` | `/api/v1/commands/lint` | admin | Lint a command |
| `POST` | `/api/v1/commands/preview` | admin | Preview a command's rendering |
| `DELETE` | `/api/v1/commands/{id}` | admin | Delete a command |
| `GET` | `/api/v1/commands/{id}` | any signed-in role | Command detail |
| `PUT` | `/api/v1/commands/{id}` | admin | Update a command |
| `POST` | `/api/v1/commands/{id}/test` | admin | Test a command on a server |

### Command groups

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/command-groups` | any signed-in role | List command groups |
| `POST` | `/api/v1/command-groups` | admin | Create a command group |
| `DELETE` | `/api/v1/command-groups/{id}` | admin | Delete a command group |
| `PUT` | `/api/v1/command-groups/{id}` | admin | Update a command group |
| `PUT` | `/api/v1/command-groups/{id}/members` | admin | Replace a command group's members |

### Packs

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/packs` | any signed-in role | List packs |
| `GET` | `/api/v1/packs/export` | any signed-in role | Export commands as YAML |
| `POST` | `/api/v1/packs/import` | admin | Import a pack |
| `POST` | `/api/v1/packs/preview` | admin | Preview a pack import |

### Proposals

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/proposals` | any signed-in role | List proposals |
| `POST` | `/api/v1/proposals/lint` | admin | Lint a proposal |
| `GET` | `/api/v1/proposals/{id}` | any signed-in role | Proposal detail |
| `POST` | `/api/v1/proposals/{id}/approve` | admin | Approve a proposal |
| `POST` | `/api/v1/proposals/{id}/reject` | admin | Reject a proposal |

### Access rules

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/rules` | any signed-in role | List access rules |
| `POST` | `/api/v1/rules` | admin | Create an access rule |
| `DELETE` | `/api/v1/rules/{id}` | admin | Delete an access rule |
| `PUT` | `/api/v1/rules/{id}` | admin | Update an access rule |

### MCP keys

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/keys` | any signed-in role | List MCP keys |
| `POST` | `/api/v1/keys` | admin | Create an MCP key (token shown only once) |
| `DELETE` | `/api/v1/keys/{id}` | admin | Delete an MCP key |
| `PUT` | `/api/v1/keys/{id}` | admin | Update an MCP key |
| `POST` | `/api/v1/keys/{id}/revoke` | admin | Revoke an MCP key |

### Simulator and dashboard

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/dashboard` | any signed-in role | Dashboard |
| `GET` | `/api/v1/dashboard/insights` | any signed-in role | Usage statistics |
| `GET` | `/api/v1/meta` | any signed-in role | Public parameters for the interface |
| `GET` | `/api/v1/simulator` | any signed-in role | Simulate a key's permissions |

### Settings

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/settings/audit-ai` | admin | Read the configuration (AI summary) |
| `PUT` | `/api/v1/settings/audit-ai` | admin | Update the configuration (AI summary) |
| `GET` | `/api/v1/settings/general` | admin | Read the configuration (general settings) |
| `PUT` | `/api/v1/settings/general` | admin | Update the configuration (general settings) |
| `GET` | `/api/v1/settings/oidc` | admin | Read the configuration (SSO) |
| `PUT` | `/api/v1/settings/oidc` | admin | Update the configuration (SSO) |
| `POST` | `/api/v1/settings/oidc/test` | admin | Test the SSO configuration |
| `GET` | `/api/v1/settings/syslog` | admin | Read the configuration (syslog) |
| `PUT` | `/api/v1/settings/syslog` | admin | Update the configuration (syslog) |
| `POST` | `/api/v1/settings/syslog/test` | admin | Send a test message (syslog) |
| `GET` | `/api/v1/settings/webhook` | admin | Read the configuration (webhook) |
| `PUT` | `/api/v1/settings/webhook` | admin | Update the configuration (webhook) |
| `GET` | `/api/v1/settings/webhook/deliveries` | admin | Webhook delivery history |
| `POST` | `/api/v1/settings/webhook/test` | admin | Send a test event (webhook) |

### Users

| Method | Path | Role | Description |
|---|---|---|---|
| `GET` | `/api/v1/users` | admin | List users |
| `POST` | `/api/v1/users` | admin | Create a user |
| `DELETE` | `/api/v1/users/{id}` | admin | Delete a user |
| `PUT` | `/api/v1/users/{id}` | admin | Update a user |

### Setup and probes

| Method | Path | Role | Description |
|---|---|---|---|
| `POST` | `/api/v1/setup` | public | Create the first administrator (setup token required) |
| `GET` | `/api/v1/setup/status` | public | Tell whether the instance is initialised |

### Other

| Method | Path | Role | Description |
|---|---|---|---|
| `POST` | `/api/v1/integrity/verify` | admin | Verify the logs' integrity chain |
| `GET` | `/health` | public | Liveness probe |
| `GET` | `/healthz` | public | Liveness probe (former name of /health) |
| `GET` | `/metrics` | any signed-in role | Prometheus-format metrics |

---

[← Previous : Architecture](architecture.md) · [Home](README.md) · [Next : Troubleshooting and FAQ →](troubleshooting.md)
