# ADR 0007 — Les fichiers de test restent à côté du code

- Statut : accepté (2026-11-11)
- Ticket : sebastien/Leassh#45

## Contexte

Des fichiers `*_test.go` et `*.test.tsx` sont déposés dans les mêmes dossiers que le code. Le ticket demande s'il est possible de les regrouper dans un dossier `tests/` dédié, au motif qu'ils ne sont pas nécessaires à l'exécution de l'application.

## Décision

On **garde les tests à côté du code**, côté Go comme côté frontend.

- **Aucun test n'est livré.** Le compilateur Go ignore les `_test.go` pour `go build` : le binaire n'en contient aucun. Le build Vite ne compile que ce qui est importé depuis `main.tsx` : les `*.test.tsx` n'entrent ni dans `web/dist` ni dans le binaire (`go:embed all:dist`). L'image Docker (distroless) ne contient que le binaire.
- **Go : 41 des 45 fichiers de test sont dans le même package que le code** et exercent des symboles non exportés. Les déplacer dans un dossier séparé impose soit d'exporter ces symboles (une API publique à maintenir, une surface plus grande), soit de réécrire les tests en boîte noire. Ce coût n'est justifié par aucun gain. L'outillage Go (`go test ./...`, couverture, `go vet`, `golangci-lint`) suppose cette disposition.
- **Frontend : les tests Vitest restent à côté du composant testé**, ce qui garde un test et son sujet visibles ensemble. Les tests de bout en bout Playwright, qui traversent l'application entière, vivent déjà à part, dans `web/e2e/`.

## Conséquences

- Toute la logique reste testable sans rien exporter pour les besoins du test.
- Un lecteur qui cherche « le code de production » filtre les `*_test.go` et `*.test.tsx` (par exemple `find … ! -name '*_test.go'`).
- Les tests d'intégration ou de bout en bout transversaux continuent d'aller dans un dossier séparé (`web/e2e/`, `internal/ci/`).
- La question peut être rouverte par un nouvel ADR si le projet adopte des tests en boîte noire.
