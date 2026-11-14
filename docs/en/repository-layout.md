**English** · [Français](../fr/structure-du-depot.md)

# Repository layout and decisions

The repository structure, each folder's role and the "where to maintain what" rule are described in the **[`docs/` README](../README.md)** (bilingual), versioned with the code. This page is deliberately not a copy of it.

## Architecture decision records (ADR)

One structuring decision = one numbered file in [`docs/adr/`](../adr/README.md) (written before the code; the first five were written after the fact). The ADRs themselves are in French.

| No. | Decision |
|---|---|
| [0001](../adr/0001-authentification-sso-et-compte-local.md) | Authentication: mandatory SSO, local first-sign-in account |
| [0002](../adr/0002-binaire-unique-front-et-back.md) | Single binary, front end and back end not split |
| [0003](../adr/0003-choix-de-la-stack-go-sqlite.md) | Stack: Go and SQLite |
| [0004](../adr/0004-chaine-integrite-du-journal.md) | Log integrity chain |
| [0005](../adr/0005-synthese-ia-configurable-en-base.md) | Audit report AI summary configurable in the database |

## Specifications

- [`docs/specs/SPEC.md`](../specs/SPEC.md): functional and technical specification, normative reference (French).
- [`docs/specs/UX.md`](../specs/UX.md): design system (French).
- [`docs/specs/`](../specs/README.md): one specification per feature (SSO, audit report, provisioning).

---

[← Previous : Troubleshooting and FAQ](troubleshooting.md) · [Home](README.md) · [Next : Contributing and development →](contributing.md)
