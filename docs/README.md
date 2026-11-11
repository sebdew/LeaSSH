# Documentation et structure du dépôt · Documentation and repository layout

**Français** · [English](#english)

La documentation **utilisateur** (installation, manuel écran par écran, SSO, exploitation, dépannage) est dans le **[wiki](https://gitea.dewarlez.fr/sebastien/Leassh/wiki)**, en français et en anglais. Ce dossier `docs/` contient ce qui doit évoluer **avec le code** et être relu en merge request.

## Structure du dépôt

```
.
├── cmd/leassh/             point d'entrée (serve, backup, rotate-master-key, reset-password…)
├── internal/               backend Go
│   ├── api/                API JSON de l'interface (session + CSRF), SSE, OpenAPI testée
│   ├── mcpserver/          les 5 outils MCP (sans état)
│   ├── executions/         executions.Run() : authz → validation → rendu → décision → exécution → journal
│   ├── authz/              évaluation des droits (fonction unique)
│   ├── catalog/            commandes, paramètres typés, rendu, linter, corpus d'injection
│   ├── sshexec/            connexion, clé d'hôte épinglée, canal exec, pool
│   ├── store/              accès base SQLite, migrations (goose)
│   ├── audit/ crypto/ oidc/ config/ metrics/ retention/ rotation/ settings/
│   ├── webhook/ syslog/ provisioning/ auditreport/ packs/ proposals/ accounts/
│   └── ci/                 tests qui vérifient la chaîne qualité et la documentation
├── web/                    frontend React 19 + TypeScript (Vite, Tailwind), embarqué dans le binaire
├── packs/                  packs de commandes YAML fournis
├── docs/                   ce dossier (voir ci-dessous)
├── .gitea/workflows/       intégration continue
├── Dockerfile, docker-compose.yml, Makefile, .env.example
├── CLAUDE.md               consignes de développement
├── CONTRIBUTING.md         flux de travail (ticket, branche, merge request, revue)
├── CHANGELOG.md            historique des versions (SemVer)
└── README.md, README.en.md présentation, fonctionnalités, démarrage rapide
```

## Contenu de `docs/`

| Chemin | Rôle |
|---|---|
| [`specs/SPEC.md`](specs/SPEC.md) | Cahier des charges fonctionnel et technique, **référence normative** |
| [`specs/UX.md`](specs/UX.md) | Guide UX/UI et design system (tokens de couleur) |
| [`specs/<fonctionnalité>/`](specs/README.md) | Une spécification par fonctionnalité : `spec.md`, `plan.md`, `tasks.md` (SSO, rapport d'audit, provisioning) |
| [`adr/`](adr/README.md) | Décisions d'architecture (un fichier numéroté par décision) |
| [`architecture.md`](architecture.md) | Composants, flux, frontières de confiance, données stockées et conservation |
| [`exploitation.md`](exploitation.md) | Mise à jour, retour arrière, sauvegarde, test de restauration consigné |
| [`openapi.json`](openapi.json) | Spécification OpenAPI de l'API (un test la compare au routeur) |
| [`securite-sorties-et-urls.md`](securite-sorties-et-urls.md) | Décisions sur les sorties de commandes non fiables et les URL sortantes |
| [`standards/`](standards/) | Constitution et checklist de revue, **non modifiés ici** (modèle de dépôt) |
| [`revues/`](revues/) | Rapports de revue (ex. revue du 2026-11-07) |
| [`logo/`](logo/) | Logos PNG (logos, symboles, icônes, favicons) ; copies redimensionnées dans `web/public/` |

## Où maintenir quoi ?

- **Une seule source par information.** Le wiki ne répète pas ce dossier : ses pages Architecture et Exploitation en sont des copies à régénérer, jamais à éditer à la main.
- Un changement de comportement met à jour **dans la même merge request** : le code, `specs/SPEC.md`, l'ADR ou l'architecture concernés, `CHANGELOG.md`, et la page du wiki si l'usage change.

<a id="english"></a>

## English

**User** documentation (installation, screen-by-screen manual, SSO, operations, troubleshooting) lives in the **[wiki](https://gitea.dewarlez.fr/sebastien/Leassh/wiki/EN-Home)**, in French and English. This `docs/` folder holds what must evolve **with the code** and be reviewed in merge requests.

### Repository layout

| Path | Purpose |
|---|---|
| `cmd/leassh/` | Entry point (`serve`, `backup`, `rotate-master-key`, `reset-password`…) |
| `internal/` | Go backend: `api` (JSON API, SSE), `mcpserver` (the 5 MCP tools), `executions` (the single execution path), `authz`, `catalog` (typed parameters, linter), `sshexec`, `store` (SQLite), `audit`, `crypto`, `oidc`, `webhook`, `syslog`, `provisioning`, `auditreport`, `packs`… |
| `web/` | React 19 + TypeScript front end, embedded in the binary |
| `packs/` | Provided YAML command packs |
| `docs/` | This folder (below) |
| `.gitea/workflows/` | Continuous integration |
| `Dockerfile`, `docker-compose.yml`, `Makefile`, `.env.example` | Build and deployment |
| `CLAUDE.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `README*.md` | Development rules, workflow, version history, overview |

### Contents of `docs/`

| Path | Purpose |
|---|---|
| [`specs/SPEC.md`](specs/SPEC.md) | Functional and technical specification, **normative reference** (French) |
| [`specs/UX.md`](specs/UX.md) | UX/UI guide and design system (French) |
| [`specs/<feature>/`](specs/README.md) | One specification per feature: `spec.md`, `plan.md`, `tasks.md` |
| [`adr/`](adr/README.md) | Architecture decision records |
| [`architecture.md`](architecture.md) | Components, flows, trust boundaries, stored data and retention |
| [`exploitation.md`](exploitation.md) | Update, rollback, backup, recorded restore test |
| [`openapi.json`](openapi.json) | OpenAPI specification (a test compares it to the router) |
| [`securite-sorties-et-urls.md`](securite-sorties-et-urls.md) | Decisions on untrusted command output and outgoing URLs |
| [`standards/`](standards/) | Constitution and review checklist, **not modified here** (template repository) |
| [`revues/`](revues/) | Review reports |
| [`logo/`](logo/) | PNG logos (logos, symbols, icons, favicons); resized copies in `web/public/` |

### Where to maintain what

- **One source per piece of information.** The wiki does not repeat this folder: its Architecture and Operations pages are copies to regenerate, never to edit by hand.
- A behaviour change updates **in the same merge request**: the code, `specs/SPEC.md`, the relevant ADR or architecture file, `CHANGELOG.md`, and the wiki page if usage changes.
