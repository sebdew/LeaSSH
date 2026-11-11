# CLAUDE.md — consignes de développement

Projet : **Leassh**, serveur MCP open source qui donne à des agents IA un accès SSH limité à un catalogue de commandes validées.
Niveau du projet : **N3** (voir `docs/standards/constitution.md` et `CONTRIBUTING.md`).
Lire d'abord : `README.md`, `docs/specs/SPEC.md` (fonctionnel, référence), `docs/specs/UX.md` (interface), `docs/architecture.md`. La documentation utilisateur vit dans le **wiki** du dépôt (français et anglais).

## Stack

**Backend (Go ≥ 1.26)**
- HTTP : `net/http` + `go-chi/chi`
- MCP : SDK officiel `github.com/modelcontextprotocol/go-sdk`, transport HTTP streamable sur `/mcp`
- SSH : `golang.org/x/crypto/ssh`
- Base : SQLite via `modernc.org/sqlite` (pur Go, pas de CGO), migrations `pressly/goose`, requêtes SQL paramétrées écrites à la main dans `internal/store` ([ADR 0003](docs/adr/0003-choix-de-la-stack-go-sqlite.md) ; `sqlc` écarté pour l'instant)
- Chiffrement secrets : AES-256-GCM (`crypto/cipher`), clé maître depuis `LEASSH_MASTER_KEY`
- Mots de passe : argon2id (`golang.org/x/crypto/argon2`)
- Logs applicatifs : `log/slog` en JSON
- IDs : ULID (`oklog/ulid/v2`)

**Frontend**
- React 19 + TypeScript strict + Vite
- Tailwind CSS v4 + composants shadcn/ui (Radix), icônes `lucide-react`
- TanStack Query (données), TanStack Router (routing, filtres dans l'URL), TanStack Table (journal)
- Polices Inter + JetBrains Mono embarquées (pas de CDN)
- i18n : `i18next`, français par défaut, anglais prévu dès la structure
- Build embarqué dans le binaire via `go:embed`

## Arborescence cible

```
cmd/leassh/            point d'entrée (serve, rotate-master-key, version)
internal/
  config/             variables d'environnement
  store/              accès base (SQL écrit à la main, voir ADR 0003), migrations
  crypto/             chiffrement des secrets, hash des jetons
  authz/              évaluation des droits (fonction unique, cf. SPEC §5)
  catalog/            commandes, paramètres typés, rendu, linter
  sshexec/            connexion, vérification clé d'hôte, exécution, pool
  executions/         cycle de vie : demande → validation → exécution → journal
  mcpserver/          outils MCP
  api/                API JSON de l'interface (session + CSRF)
  audit/              événements d'administration, chaîne d'intégrité
  webhook/            envoi signé HMAC
  packs/              import / export YAML
web/                  frontend React
packs/                packs YAML fournis
docs/
```

## Commandes

- `make dev` : backend avec rechargement + Vite en mode dev (proxy API)
- `make test` : tests Go (dont corpus d'injection) + tests frontend (Vitest)
- `make lint` : `go vet`, `gofmt`, `golangci-lint` v2 (version figée dans le Makefile, dont `gosec`), `eslint`, `tsc --noEmit`
- `make build` : build frontend puis binaire unique
- `make e2e` : parcours Playwright sur le binaire réel
- `make docker` : image multi-arch (amd64/arm64), base distroless, publiée dans le registre Gitea `gitea.dewarlez.fr/sebastien/leassh:<version>`
- Version : SemVer (`vMAJEUR.MINEUR.CORRECTIF`), tag Git et entrée de `CHANGELOG.md` ; `make build` injecte `git describe` (`dev` sans tag)

## Invariants de sécurité — ne jamais enfreindre

1. **Aucun chemin d'exécution SSH ne contourne `executions.Run()`**, qui applique dans l'ordre : authz → validation des paramètres → rendu → vérification du hash → décision de validation → exécution → journal.
2. **Aucun type de paramètre « texte libre ».** Toute valeur est validée selon son type puis échappée POSIX avant insertion.
3. **Canal `exec` uniquement** : jamais de PTY, de shell, d'agent forwarding, de port forwarding, de X11.
4. **Clé d'hôte strictement vérifiée** ; divergence = serveur bloqué.
5. **Une clé MCP ne peut rien modifier** : ni configuration, ni validation, ni lecture des exécutions d'une autre clé.
6. **Toute demande `run_command` est journalisée**, y compris les refus.
7. **Les secrets ne sont jamais journalisés ni renvoyés** par l'API (clé privée, mot de passe, jeton MCP après création). Masquage centralisé dans le logger et dans les diffs d'audit.
8. Les messages d'erreur MCP ne révèlent pas l'existence de serveurs ou commandes non visibles pour la clé.
9. Jetons MCP stockés uniquement sous forme de hash ; comparaison à temps constant.
10. Aucune exécution de commande locale (`os/exec`) dans le backend.

## Tests obligatoires

- **Corpus d'injection** (`internal/catalog/injection_test.go`) : chaque type de paramètre × charges malveillantes (`; id`, `$(id)`, `` `id` ``, `| id`, `&& id`, `\n id`, `-rf`, `--help`, `../`, `'`, `"`, `\`, octets nuls, unicode, longueurs extrêmes). Les valeurs acceptées sont rendues puis exécutées dans un vrai `sh -c 'printf "%s\n" …'` (conteneur de test) pour prouver qu'aucune n'est interprétée. Objectif ≥ 300 combinaisons.
- **Linter** : un test par règle (SPEC §6.4).
- **Authz** : table de vérité des profils × risques × `write_requires_approval`, clés expirées / désactivées, serveurs `mismatch`.
- **Exécution SSH** : tests d'intégration avec un serveur SSH en conteneur (`linuxserver/openssh-server`) : succès, échec, timeout, sortie tronquée, divergence de clé d'hôte.
- **MCP** : tests de bout en bout des 5 outils avec le client du SDK.
- **Frontend** : tests des composants critiques (journal, validations, éditeur de commande), contrastes vérifiés dans les deux thèmes.

## Conventions

- Go : erreurs enveloppées (`fmt.Errorf("…: %w", err)`), pas de panic hors init, contexte propagé partout, pas de variables globales mutables.
- API JSON : `/api/v1/...`, erreurs au format `{ "error": { "code": "…", "message": "…" } }`, pagination par curseur.
- Toute mutation de configuration passe par une fonction qui écrit l'`audit_event` dans la même transaction.
- Frontend : uniquement les tokens de couleur définis dans `docs/specs/UX.md` (CSS variables), aucun hex en dur dans les composants.
- Textes de l'interface via i18n, jamais en dur.
- Commits conventionnels (`feat:`, `fix:`, `docs:`…).

## Jalons

Avancer jalon par jalon ; chaque jalon se termine par des tests verts et une démo manuelle.

1. **M1 — Socle** : config, base + migrations, chiffrement, comptes et sessions, écran d'initialisation, layout de l'interface (barre latérale, thème clair/sombre, palette `⌘K` vide).
2. **M2 — Catalogue** : paramètres typés, rendu, linter, CRUD commandes et groupes, éditeur avec aperçu. Corpus d'injection.
3. **M3 — Infra** : identités SSH (génération ed25519), serveurs, épinglage de clé d'hôte, test de connexion, groupes de serveurs.
4. **M4 — Accès** : clés MCP, règles d'accès, fonction authz, simulateur d'accès.
5. **M5 — Exécution + MCP** : `executions.Run()`, pool SSH, les 5 outils MCP, test de commande depuis l'éditeur.
6. **M6 — Journal et validations** : écran Journal (filtres, direct SSE, tiroir, export), journal d'administration avec diff, chaîne d'intégrité, écran Validations (mobile), expiration des demandes.
7. **M7 — Propositions, packs, webhook** : `propose_command`, écran Propositions, import/export YAML avec aperçu, packs fournis, webhook signé.
8. **M8 — Finitions** : tableau de bord et checklist, états vides, accessibilité, `/metrics`, image Docker, documentation d'installation.

## Définition de « terminé »

- Tests verts, lint propre.
- Écran vérifié en thème clair **et** sombre, et en largeur mobile pour Validations et Journal.
- Toute nouvelle action d'administration produit un événement d'audit.
- Documentation mise à jour (`SPEC.md` si le comportement change).
