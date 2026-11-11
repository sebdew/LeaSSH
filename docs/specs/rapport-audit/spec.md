# Spécification — Rapport d'audit ISO 27001 / HDS

Statut : livrée (écrite a posteriori). Détail normatif : `docs/specs/SPEC.md`, annexe « Rapport d'audit ». Décisions : [ADR 0004](../../adr/0004-chaine-integrite-du-journal.md) et [ADR 0005](../../adr/0005-synthese-ia-configurable-en-base.md).

## Quoi et pourquoi

Produire en quelques clics, pour une période donnée, un document remis à un auditeur : inventaire, activité, intégrité des journaux, changements de configuration, constats et correspondance **indicative** aux contrôles ISO/IEC 27001:2022 et à des thèmes HDS. C'est une aide à l'audit, jamais un verdict de conformité.

## Scénarios

1. L'administrateur choisit une période et génère le rapport ; il l'ouvre (HTML imprimable en PDF) ou télécharge le JSON signé.
2. L'auditeur recalcule l'empreinte du JSON ; l'administrateur confirme avec « Vérifier un rapport » que le fichier vient de l'instance.
3. Facultatif : l'administrateur active une synthèse rédigée par une IA (désactivée par défaut).

## Exigences

- Les **faits** sont calculés de façon déterministe à partir de la base, sans IA : deux calculs du même état donnent le même JSON.
- Le HTML est autonome (aucun script, aucune ressource externe).
- Le JSON est signé : SHA-256 du contenu + HMAC dérivé de la clé maître.
- Les constats suivent des règles fixes avec gravité ; les seuils sont indicatifs.
- La synthèse IA ne reçoit que des agrégats (aucun nom, aucune sortie), est stockée hors empreinte et étiquetée « à relire ».
- Administrateurs uniquement ; les rapports ne se suppriment pas ; chaque génération est auditée.

## Critères d'acceptation

- Un test échoue si un nom présent en base sort vers l'IA.
- La modification d'une ligne de journal est détectée par la vérification d'intégrité incluse au rapport.
- `jq -cj .snapshot rapport.json | sha256sum` redonne `signature.digest`.
