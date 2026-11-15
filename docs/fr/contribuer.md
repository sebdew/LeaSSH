**Français** · [English](../en/contributing.md)

# Contribuer et développer

Leassh est un projet de niveau **N3** régi par une [constitution](../standards/constitution.md). La version de référence est [`CONTRIBUTING.md`](../../CONTRIBUTING.md).

## Flux de travail

1. **Un ticket GitHub** décrit le besoin.
2. **Une branche dédiée** depuis `main` à jour : `feat/12-nom-court`, `fix/15-nom-court`.
3. **Le test d'abord** (un test qui échoue et démontre le problème ; pour une fonction, sa spécification dans `docs/specs/`).
4. **Réutiliser plutôt que réinventer** : socle, bibliothèque standard, dépendance déjà présente, librairie reconnue, et seulement ensuite du code maison.
5. **Vérifier** : `make lint` et `make test`.
6. **Commits Conventional Commits citant le ticket** : `fix: ne relaie plus le corps distant (#35)`.
7. **Une merge request** avec `Closes #N` ; la documentation est mise à jour dans la **même** merge request.
8. **Revue par un agent** puis **relecture humaine du diff** ; CI verte obligatoire.
9. **Version** : entrée de `CHANGELOG.md` et tag SemVer.

## Environnement de développement

Go ≥ 1.26, Node ≥ 24.

```bash
make dev     # backend avec rechargement + Vite (proxy API)
make test    # tests Go (dont corpus d'injection) + tests Vitest
make lint    # go vet, gofmt, golangci-lint (gosec), tsc --noEmit, eslint
make e2e     # parcours Playwright (Chromium) sur le binaire réel
make build   # frontend puis binaire unique bin/leassh
make docker  # image multi-architecture
go test -tags integration ./internal/sshexec -run Integration   # vrai sshd via Docker
```

## Organisation du code

`cmd/leassh` (point d'entrée), `internal/` (`api`, `authz`, `catalog`, `executions`, `mcpserver`, `sshexec`, `store`, `audit`, `crypto`, `oidc`, `provisioning`, `auditreport`, `syslog`, `webhook`, `packs`…), `web/` (React 19, TypeScript, Vite, Tailwind), `packs/` (YAML), `docs/` (architecture, ADR), `docs/specs/` (SPEC, UX, une spécification par fonctionnalité).

## Intégration continue

Le workflow GitHub Actions s'exécute sur chaque merge request et sur `main` : `make lint`, `make test`, tests d'intégration SSH (vrai `openssh-server`), parcours Playwright, **gitleaks** (secrets), **Trivy**, **govulncheck**, `npm audit`. Rien n'est fusionné sans CI verte ; `main` est protégée.

## Documentation

- [`docs/fr/`](README.md) et [`docs/en/`](../en/README.md) : la documentation utilisateur, en français et en anglais.
- [`docs/specs/SPEC.md`](../specs/SPEC.md) : le cahier des charges fonctionnel et technique normatif.
- [`docs/adr/`](../adr/README.md) : les décisions d'architecture.
- [`docs/specs/`](../specs/README.md) : une spécification par fonctionnalité.

Licence : Apache 2.0 ; conservez la licence et le fichier `NOTICE`, citez le créateur (Sébastien Dewarlez) et les modifications.

---

[← Précédent : Structure du dépôt et décisions](structure-du-depot.md) · [Accueil](README.md)
