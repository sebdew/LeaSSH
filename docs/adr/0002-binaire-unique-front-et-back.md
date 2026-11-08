# ADR 0002 — Binaire unique : front et back dans un même conteneur

- Statut : accepté (2026-11-08)
- Ticket : sebastien/Leassh#7 (constat ARC-04 de la revue du 2026-11-07)

## Contexte

La constitution (principe 4, niveau N2) demande deux dossiers (`backend/`, `frontend/`) et deux conteneurs, avec une API documentée en OpenAPI. Leassh est un outil à déploiement autonome : un opérateur le pose sur une machine, avec un volume de données, sans autre composant.

## Décision

1. **Exception motivée au principe 4** : le front React (`web/`) est compilé puis embarqué dans le binaire Go (`go:embed`). Une seule image, un seul conteneur, un seul port.
2. **La séparation logique est conservée** : le front n'appelle que l'API JSON `/api/v1`, ne contient aucune règle métier ni secret, et les droits sont vérifiés côté backend. `web/` a ses propres dépendances (`package.json`) et son propre build.
3. **L'API est documentée en OpenAPI** : `docs/openapi.json`, maintenue à la main (voir ci-dessous).
4. **Pas de renommage en `backend/` / `frontend/`** : la disposition Go standard (`cmd/`, `internal/`) et `web/` sont conservées, avec la même séparation que celle demandée.

## OpenAPI : maintenue à la main, mais testée

La constitution préfère une spécification générée depuis le code. Les générateurs Go (huma, swaggo) imposent de redéclarer chaque route dans leur DSL ou d'annoter chaque handler, ce qui est un gros changement pour 100 routes `chi` existantes. On garde donc un fichier écrit, et un test (`internal/api/openapi_test.go`, lancé par `make test` donc par la CI) garantit :

- que le fichier est une spécification OpenAPI 3 valide (librairie `kin-openapi`) ;
- que chaque route du routeur est documentée, et qu'aucune opération documentée n'a disparu du code.

Limite connue : les corps de requête et de réponse sont décrits comme des objets JSON génériques, sauf `/setup` et `/auth/login`. Les détailler (schémas par ressource) est un chantier à part ; il pourra s'appuyer sur ce même test.

## Conséquences

- Un seul artefact à déployer et à sauvegarder ; pas de CORS, pas de proxy entre front et back.
- Le front ne peut pas être mis à l'échelle séparément du back. Acceptable : l'usage visé est un outil d'administration à faible trafic.
- Si Leassh devait être repris en entreprise avec un front servi à part, rien n'est à réécrire : l'API est déjà isolée et documentée.
