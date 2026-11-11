# Plan technique — Rapport d'audit

## Composants

- `internal/auditreport` : calcul de l'instantané, constats, correspondance aux contrôles, rendu HTML, signature, client IA (`ai.go`).
- Table `audit_reports` (migrations 00008 et 00014) ; paramètres IA dans les réglages.
- `internal/api/handlers_auditreports.go` : `GET|POST /audit-reports`, `/{id}[/html|/json|/summary]`, `/verify`, `GET|PUT /settings/audit-ai`.
- Interface : écran **Audit** (menu de gauche).

## Réutilisation (principe 2)

Bibliothèque standard seule : `crypto/sha256`, `crypto/hmac`, `html/template` (échappement automatique), `encoding/json`. Le client IA est un simple `net/http` vers `/chat/completions` (API compatible OpenAI, aucun SDK de fournisseur). Aucune nouvelle dépendance.

## Points d'attention

- Déterminisme : tri explicite de toutes les listes ; horodatage de génération hors du calcul des faits.
- Vie privée : liste blanche `AIInput` (voir ADR 0005).
- Rotation de clé : la clé d'API de la synthèse est rechiffrée par `rotate-master-key`.
