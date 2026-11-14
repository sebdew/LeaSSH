**English** · [Français](../fr/contribuer.md)

# Contributing and development

Leassh is a **level N3** project governed by a [constitution](../standards/constitution.md) (French). The reference version is [`CONTRIBUTING.md`](../../CONTRIBUTING.md).

## Workflow

1. **A Gitea ticket** describes the need.
2. **A dedicated branch** from an up-to-date `main`: `feat/12-short-name`, `fix/15-short-name`.
3. **Test first** (a failing test that demonstrates the problem; for a feature, its specification in `docs/specs/`).
4. **Reuse rather than reinvent**: platform, standard library, dependency already present, recognised library, and only then custom code.
5. **Check**: `make lint` and `make test`.
6. **Conventional Commits citing the ticket**: `fix: do not relay the remote body (#35)`.
7. **One merge request** with `Closes #N`; documentation is updated in the **same** merge request.
8. **Agent review** then **human diff review**; green CI required.
9. **Version**: `CHANGELOG.md` entry and SemVer tag.

## Development environment

Go ≥ 1.26, Node ≥ 20.

```bash
make dev     # backend with reload + Vite (API proxy)
make test    # Go tests (incl. injection corpus) + Vitest
make lint    # go vet, gofmt, golangci-lint (gosec), tsc --noEmit, eslint
make e2e     # Playwright (Chromium) journeys on the real binary
make build   # front end then single binary bin/leassh
make docker  # multi-architecture image
go test -tags integration ./internal/sshexec -run Integration   # real sshd through Docker
```

## Code layout

`cmd/leassh` (entry point), `internal/` (`api`, `authz`, `catalog`, `executions`, `mcpserver`, `sshexec`, `store`, `audit`, `crypto`, `oidc`, `provisioning`, `auditreport`, `syslog`, `webhook`, `packs`…), `web/` (React 19, TypeScript, Vite, Tailwind), `packs/` (YAML), `docs/` (architecture, ADRs), `docs/specs/` (SPEC, UX, one specification per feature).

## Continuous integration

The Gitea Actions workflow runs on every merge request and on `main`: `make lint`, `make test`, SSH integration tests (real `openssh-server`), Playwright journeys, **gitleaks** (secrets), **Trivy**, **govulncheck**, `npm audit`. Nothing is merged without green CI; `main` is protected.

## Documentation

- [`docs/en/`](README.md) and [`docs/fr/`](../fr/README.md): user documentation, in English and French.
- [`docs/specs/SPEC.md`](../specs/SPEC.md): the normative functional and technical specification (French).
- [`docs/adr/`](../adr/README.md): architecture decisions.
- [`docs/specs/`](../specs/README.md): one specification per feature.

License: Apache 2.0; keep the license and the `NOTICE` file, credit the creator (Sébastien Dewarlez) and the modifications.

---

[← Previous : Repository layout and decisions](repository-layout.md) · [Home](README.md)
