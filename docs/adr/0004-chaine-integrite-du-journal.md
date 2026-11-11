# ADR 0004 — Chaîne d'intégrité du journal

- Statut : accepté (rétroactif, 2026-11-11)
- Ticket : sebastien/Leassh#11 (constat DOC-02 de la revue du 2026-11-07)
- Décision d'origine : jalon M6 (voir `docs/SPEC.md` §10 et annexe M6)

## Contexte

Leassh doit pouvoir répondre à « qui a lancé quoi, où, quand, pourquoi, et qui l'a validé » (SPEC §10). Un journal stocké dans une base SQLite modifiable par quiconque a accès au fichier n'est une preuve que s'il permet de **détecter** une altération. Un ancrage cryptographique externe (horodatage qualifié, ledger) serait disproportionné pour un outil mono-instance.

## Décision

1. Chaque ligne des tables `executions` et `audit_events` stocke le hash de la ligne précédente et son propre hash (SHA-256 de ses champs immuables concaténés avec le hash précédent). Les hashs sont calculés dans la **même transaction** que l'écriture de la ligne.
2. Pour les exécutions, seuls les champs immuables de la demande sont couverts (id, séquence, clé, acteur, serveur, commande, paramètres, raison, horodatage). Le statut et la sortie évoluent après coup par conception et sont hors chaîne.
3. `POST /api/v1/integrity/verify` (administrateur) recontrôle les deux chaînes. La première ligne restante sert d'ancre, parce que la rétention purge les plus anciennes.
4. La **tête de chaîne** (dernier hash) figure dans le rapport d'audit : remise à l'auditeur, elle permet de détecter une troncature ultérieure.
5. Les journaux ne sont modifiables par aucune route de l'API ; seule la rétention automatique les purge (événement `logs.purge`).

## Conséquences

- Détectés : contenu altéré, ligne supprimée au milieu, rupture de chaînage.
- **Non détectée** : la troncature de la fin de chaîne, tant que la tête n'a pas été consignée hors de la base (d'où la tête de chaîne du rapport d'audit). Un attaquant qui contrôle la base **et** la clé maître peut recalculer toute la chaîne : la chaîne détecte une erreur ou une altération maladroite, elle ne remplace pas un journal envoyé à l'extérieur (export syslog, voir `docs/SPEC.md` §11bis).
- Coût : un hash par écriture, aucune dépendance (bibliothèque standard `crypto/sha256`).
