# Journal des modifications

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/). Versionnage : [SemVer](https://semver.org/lang/fr/). Chaque entrée cite le ticket Gitea quand il existe (`sebastien/Leassh#N`).

## [Non publié]

### Modifié
- Documentation utilisateur migrée du wiki vers `docs/fr/` et `docs/en/` (captures dans `docs/images/`), architecture et exploitation sans copie ; le wiki peut être désactivé (#65).

### Ajouté
- ADR 0007 : les fichiers de test restent à côté du code, jamais livrés dans le binaire (#45).
- Documentation : wiki bilingue (français / anglais), `CONTRIBUTING.md`, `docs/architecture.md`, `docs/exploitation.md`, `docs/specs/`, ADR 0004 et 0005, versionnement de `docs/standards/` (#11).

## [0.1.0] - 2026-11-11

Première version étiquetée. Elle regroupe le socle (jalons M1 à M8 de `CLAUDE.md`), les fonctions ajoutées ensuite et les corrections issues de la revue du 2026-11-07 (`docs/revues/2026-11-07-revue.md`).

### Ajouté
- Serveur MCP en HTTP streamable (`/mcp`) : `list_servers`, `list_commands`, `run_command`, `get_execution`, `propose_command`.
- Catalogue de commandes à paramètres typés, rendu avec échappement POSIX, linter, corpus d'injection.
- Serveurs, identités SSH (ed25519), épinglage strict des clés d'hôte, groupes.
- Clés MCP, règles d'accès (profils `RO`/`RW`), simulateur d'accès, validation humaine des commandes sensibles.
- Journal des exécutions et d'administration à chaîne d'intégrité, rétention, export CSV/NDJSON, flux en direct.
- Propositions de commandes, packs YAML (9 fournis), webhook signé HMAC, export syslog (UDP/TCP/TLS, RFC 5424).
- Provisioning des serveurs depuis Warpgate.
- Rapport d'audit ISO 27001 / HDS avec synthèse IA facultative.
- SSO OpenID Connect, interface bilingue français/anglais, thèmes clair et sombre.
- Image Docker multi-architecture (distroless, non-root), `/health` et `/metrics`.
- Intégration continue Gitea Actions : lint, tests, sshd réel, Playwright, gitleaks, Trivy, govulncheck, `npm audit` (#3, #9).
- Documentation OpenAPI de l'API d'administration (#7).
- ADR 0001 à 0003 (#1, #2, #7).

### Modifié
- Le SSO devient le mode de connexion cible ; le mot de passe local est désactivable (#1).
- Vérification OIDC par `go-oidc` et `x/oauth2` à la place du code maison (#2).
- Services séparés des routes dans le backend (#7).
- Configuration `LLM_*`, supervision et mesures des appels sortants, image de base figée par condensat (#8).

### Corrigé / sécurité
- `/setup` protégé par un jeton d'initialisation, `/metrics` fermé par défaut (#4).
- Cycle de vie des exécutions fiabilisé, décision atomique avec son audit (#5).
- Durcissement des sessions, du verrou de connexion, de HSTS et des logs MCP (#6).
- Limite de débit MCP limitée aux appels d'outils, réponse 429 explicite (#16).
- Limiteur de connexion persistant, CSP sans `unsafe-inline` (#30).
- Corps des réponses distantes non relayés dans les tests et erreurs OIDC (#35).
- Mise à jour de Go vers 1.26.11 (GO-2026-6859) (#3, #35).
- Déconnexion SSO sans réaffichage de la page de connexion (#26) ; comptage des refus au tableau de bord (#23) ; décisions sur sorties non fiables et URL sortantes documentées (#10).

[Non publié]: https://gitea.dewarlez.fr/sebastien/Leassh/compare/v0.1.0...main
[0.1.0]: https://gitea.dewarlez.fr/sebastien/Leassh/releases/tag/v0.1.0
