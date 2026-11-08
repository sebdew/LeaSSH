# ADR 0003 — Stack : Go et SQLite

- Statut : accepté (2026-11-08)
- Ticket : sebastien/Leassh#7 (constat ARC-05 de la revue du 2026-11-07)
- Remarque : la constitution attend `0001-choix-de-la-stack.md` ; le numéro 0001 était déjà pris par l'ADR sur l'authentification, cet ADR en tient lieu.

## Contexte

La constitution (principe 3) réserve Go au calcul intensif, prend Python/FastAPI par défaut et PostgreSQL pour les données (SQLite accepté en N1). Toute technologie hors liste demande un ADR.

## Décision

1. **Go pour le backend.** Leassh est un serveur réseau qui tient des connexions SSH et un transport MCP : le SDK officiel MCP, `golang.org/x/crypto/ssh` et un binaire statique sans CGO sont les raisons du choix. Le binaire unique (ADR 0002) en découle. TypeScript (React) pour le front : un langage par côté.
2. **SQLite (`modernc.org/sqlite`, pur Go) pour les données.** Aucune base externe à installer, sauvegarde par copie d'un dossier, adapté à un outil mono-instance. Migrations avec `goose`.
3. **Une seule connexion à la base** (`SetMaxOpenConns(1)`) : choix volontaire pour que les transactions d'audit restent atomiques et sérialisées sur SQLite. La concurrence d'écriture est de toute façon unique avec SQLite ; les lectures sont courtes.
4. **Pas de `sqlc` pour l'instant.** `CLAUDE.md` le prévoyait, mais les requêtes paramétrées écrites à la main, confinées dans `internal/store` et testées, suffisent aujourd'hui. Les migrer vers `sqlc` reste possible table par table, sans changer les signatures du paquet `store`. Tant que ce n'est pas fait, `CLAUDE.md` ne doit pas l'annoncer comme acquis.
5. **PostgreSQL : pas maintenant.** Une option PostgreSQL sera étudiée si un usage entreprise multi-instances se confirme. Le paquet `store` est la seule frontière à adapter (les routes et services ne font pas de SQL).

## Conséquences

- Écart assumé avec la liste de la constitution, justifié par le déploiement autonome.
- Limite : une instance, pas de haute disponibilité. À réévaluer avant tout passage en usage entreprise.
