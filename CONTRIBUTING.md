# Contribuer à Leassh

Règles de la [constitution](docs/standards/constitution.md) (principe 10) et de la [checklist de revue](docs/standards/checklist-revue.md). Le projet est de niveau **N3** : tout le cadre s'applique.

## Flux de travail

1. **Un ticket GitHub** décrit le besoin (constat, critère de clôture).
2. **Une branche dédiée** depuis `main` à jour : `feat/12-nom-court`, `fix/15-nom-court`, `docs/11-nom-court`.
3. **Le test d'abord** : pour un bug, un test qui reproduit le défaut et échoue ; pour une fonction, sa spécification dans `docs/specs/<nom>/` (`spec.md`, `plan.md`, `tasks.md`) avant le code. Les tests restent à côté du code qu'ils testent et ne sont jamais livrés dans le binaire ([ADR 0007](docs/adr/0007-tests-a-cote-du-code.md)).
4. **Réutiliser plutôt que réinventer** (principe 2) : socle, bibliothèque standard, dépendance déjà présente, librairie reconnue, et seulement ensuite du code maison. Toute dépendance nouvelle est justifiée dans le plan.
5. **Vérifier** : `make lint` et `make test` (et `make e2e` pour l'interface). `make e2e` rejoue d'abord les parcours fonctionnels (identité, serveur et clé d'hôte, commande, règle, simulateur, exécution MCP réelle sur un sshd Docker, validation depuis l'écran mobile, proposition d'un agent), puis visite automatiquement toutes les routes de `web/src/App.tsx` en thème clair et sombre, sur bureau et mobile, et échoue sur une erreur console, une réponse 5xx, un débordement horizontal ou une violation d'accessibilité/contraste (axe). Une nouvelle page est donc testée sans rien ajouter ; une route à paramètre (`$id`) exige une URL d'exemple dans `web/e2e/tous-les-ecrans.spec.ts`. Relire la modification au regard de l'OWASP Top 10 et des invariants de sécurité de [`CLAUDE.md`](CLAUDE.md).
6. **Commits Conventional Commits citant le ticket** : `fix: ne relaie plus le corps distant (#35)`.
7. **Une merge request** par modification, dont la description contient `Closes #N`. La documentation (README, `docs/fr/` et `docs/en/`, `SPEC.md`, `architecture.md`, ADR, `CHANGELOG.md`) est mise à jour **dans la même merge request**.
8. **Revue par un agent** (commande `/code-review`, plus `/security-review` en N3), dont le résultat est posté en **commentaire de la merge request** (trace de la revue), puis **relecture humaine du diff** avant fusion. L'agent commente seulement, ni n'approuve ni ne fusionne ; la décision de fusion reste humaine (développeur seul : pas d'approbation tierce exigée, `required_approvals` vaut 0). `main` est protégée : CI verte obligatoire.
9. **Version** : décrire le changement dans [`CHANGELOG.md`](CHANGELOG.md), section « Non publié ». Le tag SemVer et le package sont créés automatiquement au merge (voir « Versions et packages »).

## Décisions et documentation

- Décision structurante → un [ADR](docs/adr/README.md) numéroté, avant le code.
- Documentation utilisateur → [`docs/fr/`](docs/fr/README.md) **et** [`docs/en/`](docs/en/README.md), en français et en anglais (mêmes pages, mêmes captures dans `docs/images/`).
- Comportement normatif → [`docs/specs/SPEC.md`](docs/specs/SPEC.md).

## Versions et packages

Chaque merge dans `main` dont la CI est verte déclenche le job `release` de `.github/workflows/ci.yml`. Il déduit la version `vA.B.C` des Conventional Commits mergés depuis le dernier tag, pousse le tag, publie l'image `ghcr.io/sebdew/leassh:<version>` (+ `latest`, amd64 et arm64) et crée la release GitHub (binaires Linux, `SHA256SUMS`, notes générées).

| Commit | Effet |
| --- | --- |
| `BREAKING CHANGE:` en pied de commit (ou `!` après le type) | **A** : version majeure |
| `feat:` | **B** : nouvelle fonctionnalité |
| `fix:` | **C** : correctif (bug ou sécurité) |
| tout autre type (`docs:`, `test:`, `chore:`, `ci:`, `refactor:`…) | aucune version |

Le type du commit décide donc de la version : choisir `feat` ou `fix` avec soin. Les merges GitHub (« Merge pull request … ») sont ignorés, ce sont les commits de la branche qui comptent (avec un squash, le titre de la PR). Un jalon se force à la main : **Actions → ci → Run workflow** sur `main`, champ `version` (ex. `1.0.0`). La CI complète tourne d'abord, le package n'est publié que si elle est verte. Le `CHANGELOG.md` reste rédigé à la main : on renomme « Non publié » en numéro de version lors des jalons.

## Migrations de base

Les bases déjà déployées ne repartent jamais de zéro : chaque évolution du schéma est une **nouvelle** migration goose dans `internal/store/migrations/` (numéro suivant, section `Up` obligatoire).

- Une migration livrée dans une release ne se modifie jamais. `TestPublishedMigrationsAreImmutable` compare leurs empreintes à `internal/store/testdata/published_migrations.sha256`.
- **À chaque release**, ajouter au manifeste les migrations nouvellement livrées : `cd internal/store/migrations && sha256sum 000NN_*.sql >> ../testdata/published_migrations.sha256`.
- Si la migration change le sens de données existantes, compléter `internal/store/testdata/seed_v0.1.0.sql` ou ajouter un jeu de données de la version concernée, pour que `TestUpgradeFromV010KeepsData` le prouve.
- Pas de migration descendante : un programme plus ancien que la base refuse de démarrer (voir `docs/exploitation.md`).

## Sécurité

Une vulnérabilité ne se signale pas dans un ticket public : contactez directement le mainteneur (dépôt d'origine dans [`NOTICE`](NOTICE)).
