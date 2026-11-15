<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/logo/leassh-sombre-fr.png">
    <img src="docs/images/logo/leassh-clair-fr.png" alt="LeaSSH, serveur MCP, actions sécurisées pour agents IA" width="450">
  </picture>
</h1>

> **Donnez à vos agents IA un accès SSH… en laisse.**
> Un catalogue de commandes validées, des droits clairs, et un journal de tout ce qui a été lancé, par qui et où.

**Niveau : N3** (exposé ou destiné à l'entreprise) — règles de la [constitution](docs/standards/constitution.md) et de la [checklist de revue](docs/standards/checklist-revue.md) dans [`docs/standards/`](docs/standards/). · [English version](README.en.md)

![Tableau de bord de Leassh](docs/images/fr/01-dashboard.png)

## Présentation

Leassh est un serveur **MCP** (Model Context Protocol) open source qui permet à des agents IA d'agir sur vos serveurs **sans jamais leur donner de shell**. L'agent ne tape pas de commandes : il **choisit** dans un catalogue que vous avez écrit ou approuvé, avec des paramètres typés et validés. Chaque demande — autorisée, refusée ou en attente de validation humaine — est journalisée.

Filtrer les commandes d'un agent après coup (liste noire, regex) est contournable : `rm` bloqué, mais `find -delete`, `python -c`, `truncate` ou `$( )` passent. Leassh inverse la logique :

| Approche classique | Leassh |
|---|---|
| L'agent écrit une commande, on essaie de la filtrer | L'agent choisit une commande dans un catalogue |
| Liste noire (tout ce qui n'est pas interdit passe) | Liste blanche (rien ne passe s'il n'est pas prévu) |
| Paramètres libres | Paramètres typés, validés puis échappés |
| Logs SSH bruts | Journal métier : agent, serveur, commande, paramètres, raison, résultat, validateur |

## Fonctionnalités

- 🗂️ **Catalogue de commandes** avec modèles, paramètres typés (`enum`, `int`, `service`, `path`, `identifier`, `regex`), niveau de risque (`read` / `write` / `destructive`) et description destinée à l'IA, éditeur avec aperçu et test.
- 🖥️ **Serveurs et identités SSH** : génération de clés ed25519 depuis l'interface, **épinglage strict de la clé d'hôte**.
- 👥 **Groupes** de serveurs et de commandes.
- 🔑 **Clés MCP** par agent, jamais stockées en clair, avec expiration et limite de débit.
- 🛡️ **Règles d'accès** : groupes de serveurs + groupes de commandes + clés + profil (`RO`, `RW`), et **simulateur d'accès**.
- ✋ **Validation humaine** des commandes sensibles, depuis l'interface (mobile compris).
- 💡 **Propositions** : l'agent peut proposer une commande ; un linter rejette les modèles dangereux, vous validez.
- 📜 **Journal complet** (exécutions et administration) à **chaîne d'intégrité** : filtres, direct, export CSV/NDJSON, rétention.
- 📦 **Packs de commandes** en YAML (système, systemd, MySQL, PostgreSQL, nginx, Docker, pfSense, Proxmox, UniFi), importables et partageables.
- 🔐 **SSO OpenID Connect** (Keycloak, Entra ID, Authentik, Google…), PKCE, rôles issus des groupes ([ADR 0001](docs/adr/0001-authentification-sso-et-compte-local.md)).
- 🔄 **Provisioning** : synchronisation des serveurs depuis un bastion Warpgate.
- 📡 **Export syslog** (UDP/TCP/TLS, RFC 5424, texte/JSON/CEF) et **webhook** (signature HMAC facultative, compatible Mattermost, Slack).
- 🧾 **Rapport d'audit** ISO 27001 / HDS, synthèse IA facultative.
- 🌍 Interface en **français et en anglais**, thèmes clair et sombre, utilisable sur mobile pour les validations et le journal.
- 📦 **Un seul binaire** (ou une image Docker), SQLite par défaut, `/health` et `/metrics`.

## Démarrage rapide

Prérequis : Docker, un reverse proxy TLS devant Leassh (Caddy, nginx…) et un accès SSH aux serveurs à piloter.

```bash
export LEASSH_MASTER_KEY="$(openssl rand -base64 32)"
echo "$LEASSH_MASTER_KEY"   # conservez-la dans un gestionnaire de mots de passe

docker run -d --name leassh --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v leassh-data:/data \
  -e LEASSH_MASTER_KEY \
  ghcr.io/sebdew/leassh:latest

docker logs leassh   # jeton d'initialisation, affiché une seule fois
```

Ouvrez l'URL, créez le compte administrateur avec le jeton d'initialisation, puis suivez la checklist d'accueil : identité SSH → serveur et empreinte → pack de commandes → règle d'accès → clé MCP → configuration de l'agent.

> **Conservez `LEASSH_MASTER_KEY`** (hors de la sauvegarde de la base) : elle chiffre les clés privées SSH. Sans elle, ces secrets sont irrécupérables.

## Documentation

La documentation complète est dans le dossier [`docs/`](docs/README.md), en français ([`docs/fr/`](docs/fr/README.md)) et en anglais ([`docs/en/`](docs/en/README.md)) :

| | |
|---|---|
| [Présentation et concepts](docs/fr/presentation.md) | [Installation détaillée](docs/fr/installation.md) |
| [Premiers pas](docs/fr/premiers-pas.md) | [Manuel utilisateur, écran par écran](docs/fr/manuel-utilisateur.md) |
| [Agents et outils MCP](docs/fr/agents-mcp.md) | [Sécurité](docs/fr/securite.md) |
| [SSO (Keycloak pas à pas)](docs/fr/sso.md) | [Exploitation : sauvegarde, mise à jour, supervision](docs/exploitation.md) |
| [Architecture](docs/architecture.md) | [Référence de l'API](docs/fr/api.md) |

## Connecter un agent

Endpoint MCP en HTTP streamable `https://leassh.example.com/mcp`, authentifié par `Authorization: Bearer <clé MCP>`.

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

Cinq outils : `list_servers`, `list_commands`, `run_command`, `get_execution`, `propose_command` ([détail](docs/fr/agents-mcp.md)).

## Sécurité en bref

- Pas de PTY, pas de shell interactif : uniquement des canaux SSH `exec`.
- Aucun paramètre en texte libre ; chaque valeur est validée puis échappée.
- Les clés MCP ne peuvent ni modifier la configuration, ni approuver quoi que ce soit.
- Secrets chiffrés au repos (AES-256-GCM), jamais journalisés ; mots de passe en argon2id.
- Clés d'hôte épinglées ; toute divergence bloque le serveur.

Voir [Sécurité](docs/fr/securite.md) et [`docs/specs/SPEC.md`](docs/specs/SPEC.md).

## Développement

Go ≥ 1.26, Node ≥ 24.

```bash
make dev     # backend avec rechargement + Vite (proxy API)
make test    # tests Go (dont corpus d'injection) + tests Vitest
make lint    # go vet, gofmt, golangci-lint (dont gosec), tsc --noEmit, eslint
make e2e     # parcours Playwright (Chromium) sur le binaire réel, base neuve
make build   # build du frontend puis binaire unique (bin/leassh)
make docker  # image Docker multi-architecture
go test -tags integration ./internal/sshexec -run Integration  # vrai sshd via Docker
```

Contribution : [`CONTRIBUTING.md`](CONTRIBUTING.md) (ticket, branche, merge request, revue). Historique : [`CHANGELOG.md`](CHANGELOG.md). Versions : [SemVer](https://semver.org/lang/fr/), tags `vMAJEUR.MINEUR.CORRECTIF`.

### Intégration continue

Le workflow `.github/workflows/ci.yml` (GitHub Actions) s'exécute sur chaque merge request et sur `main` : `make lint`, `make test`, tests d'intégration SSH contre `openssh-server`, parcours Playwright, gitleaks (historique complet), Trivy, `govulncheck ./...` et `npm audit`. Rien n'est fusionné sans CI verte ; `main` doit être protégée (merge request obligatoire, jobs `qualite`, `integration-ssh`, `e2e` et `securite` requis).

## Documents du dépôt

- [`docs/specs/SPEC.md`](docs/specs/SPEC.md) — cahier des charges fonctionnel et technique (référence normative)
- [`docs/architecture.md`](docs/architecture.md) et [`docs/exploitation.md`](docs/exploitation.md) — architecture (schéma, données, conservation) et exploitation (retour arrière, test de restauration)
- [`docs/adr/`](docs/adr/README.md) — décisions d'architecture · [`docs/specs/`](docs/specs/README.md) — spécifications par fonctionnalité
- [`docs/specs/UX.md`](docs/specs/UX.md) — guide UX/UI et design system · [`docs/openapi.json`](docs/openapi.json) — API
- [`docs/standards/`](docs/standards/) — constitution et checklist de revue · [`CLAUDE.md`](CLAUDE.md) — consignes de développement
- [`packs/`](packs/) — packs de commandes fournis

## Licence

[Apache License 2.0](LICENSE). Vous pouvez reprendre, modifier et redistribuer ce projet, y compris dans un cadre commercial, à condition de **conserver la licence et le fichier [`NOTICE`](NOTICE)** et de **citer le créateur** (Sébastien Dewarlez) ainsi que les modifications apportées (§4).
