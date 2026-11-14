# Contribuer à Leassh

Règles de la [constitution](docs/standards/constitution.md) (principe 10) et de la [checklist de revue](docs/standards/checklist-revue.md). Le projet est de niveau **N3** : tout le cadre s'applique.

## Flux de travail

1. **Un ticket Gitea** décrit le besoin (constat, critère de clôture).
2. **Une branche dédiée** depuis `main` à jour : `feat/12-nom-court`, `fix/15-nom-court`, `docs/11-nom-court`.
3. **Le test d'abord** : pour un bug, un test qui reproduit le défaut et échoue ; pour une fonction, sa spécification dans `docs/specs/<nom>/` (`spec.md`, `plan.md`, `tasks.md`) avant le code. Les tests restent à côté du code qu'ils testent et ne sont jamais livrés dans le binaire ([ADR 0007](docs/adr/0007-tests-a-cote-du-code.md)).
4. **Réutiliser plutôt que réinventer** (principe 2) : socle, bibliothèque standard, dépendance déjà présente, librairie reconnue, et seulement ensuite du code maison. Toute dépendance nouvelle est justifiée dans le plan.
5. **Vérifier** : `make lint` et `make test` (et `make e2e` pour l'interface). Relire la modification au regard de l'OWASP Top 10 et des invariants de sécurité de [`CLAUDE.md`](CLAUDE.md).
6. **Commits Conventional Commits citant le ticket** : `fix: ne relaie plus le corps distant (#35)`.
7. **Une merge request** par modification, dont la description contient `Closes #N`. La documentation (README, `docs/fr/` et `docs/en/`, `SPEC.md`, `architecture.md`, ADR, `CHANGELOG.md`) est mise à jour **dans la même merge request**.
8. **Revue par un agent** (commande `/code-review`, plus `/security-review` en N3), puis **relecture humaine du diff** avant fusion. `main` est protégée : CI verte obligatoire.
9. **Version** : une entrée dans [`CHANGELOG.md`](CHANGELOG.md) et un tag SemVer (`vMAJEUR.MINEUR.CORRECTIF`) à chaque livraison.

## Décisions et documentation

- Décision structurante → un [ADR](docs/adr/README.md) numéroté, avant le code.
- Documentation utilisateur → [`docs/fr/`](docs/fr/README.md) **et** [`docs/en/`](docs/en/README.md), en français et en anglais (mêmes pages, mêmes captures dans `docs/images/`).
- Comportement normatif → [`docs/specs/SPEC.md`](docs/specs/SPEC.md).

## Sécurité

Une vulnérabilité ne se signale pas dans un ticket public : contactez directement le mainteneur (dépôt d'origine dans [`NOTICE`](NOTICE)).
