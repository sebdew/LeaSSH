**Français** · [English](../en/repository-layout.md)

# Structure du dépôt et décisions

La structure du dépôt, le rôle de chaque dossier et la règle « où maintenir quoi » sont décrits dans le **[README du dossier `docs/`](../README.md)**, versionné avec le code. Cette page n'en est volontairement pas une copie.

## Décisions d'architecture (ADR)

Une décision structurante = un fichier numéroté dans [`docs/adr/`](../adr/README.md) (écrit avant le code ; les cinq premiers ont été rédigés a posteriori).

| N° | Décision |
|---|---|
| [0001](../adr/0001-authentification-sso-et-compte-local.md) | Authentification : SSO obligatoire, compte local de première connexion |
| [0002](../adr/0002-binaire-unique-front-et-back.md) | Binaire unique, front et back non séparés |
| [0003](../adr/0003-choix-de-la-stack-go-sqlite.md) | Stack : Go et SQLite |
| [0004](../adr/0004-chaine-integrite-du-journal.md) | Chaîne d'intégrité du journal |
| [0005](../adr/0005-synthese-ia-configurable-en-base.md) | Synthèse IA du rapport d'audit configurable en base |

## Spécifications

- [`docs/specs/SPEC.md`](../specs/SPEC.md) : cahier des charges, référence normative.
- [`docs/specs/UX.md`](../specs/UX.md) : design system.
- [`docs/specs/`](../specs/README.md) : une spécification par fonctionnalité (SSO, rapport d'audit, provisioning).

---

[← Précédent : Dépannage et FAQ](depannage.md) · [Accueil](README.md) · [Suivant : Contribuer et développer →](contribuer.md)
