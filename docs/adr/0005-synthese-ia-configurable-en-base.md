# ADR 0005 — Synthèse IA du rapport d'audit configurable en base

- Statut : accepté (rétroactif, 2026-11-11)
- Ticket : sebastien/Leassh#11 (constats DOC-02 et ARC-06 de la revue du 2026-11-07)

## Contexte

Le rapport d'audit (ISO 27001, HDS) peut être accompagné d'une synthèse rédigée par un modèle d'IA. La constitution (principe 1) demande des appels par une API compatible OpenAI, configurés par `LLM_BASE_URL`, `LLM_API_KEY` et `LLM_MODEL`. Or Leassh est un produit open source repris par des tiers : l'administrateur doit pouvoir activer, désactiver ou changer de fournisseur **depuis l'interface**, sans redéployer, et sans que la fonction soit active par défaut.

## Décision

1. **Désactivée par défaut.** L'administrateur l'active dans Réglages → Audit (`PUT /api/v1/settings/audit-ai`).
2. **API compatible OpenAI uniquement** (`POST {url}/chat/completions`, `Authorization: Bearer`) : aucun SDK de fournisseur, aucune fonction propre à un proxy.
3. **Configuration en base** (URL, modèle, clé), avec les variables `LLM_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY` comme **valeurs par défaut** tant que rien n'est saisi dans l'interface ; la saisie les remplace. Écart assumé avec la lettre de la constitution (« tout par variable d'environnement ») : la variable reste prise en charge, la base ajoute la souplesse nécessaire à un produit.
4. **La clé d'API est chiffrée** (AES-256-GCM, clé maître), jamais renvoyée par l'API, jamais journalisée, et rechiffrée par `rotate-master-key`.
5. **Minimisation des données.** Le modèle ne reçoit que des agrégats : comptes et intitulés fixes (`AIInput`, liste blanche). Jamais de nom de serveur, de clé, d'utilisateur, de commande, de motif, de sortie ni d'adresse. Un test échoue si un nom présent dans la base en sort.
6. La synthèse est stockée **hors de l'empreinte** du rapport, étiquetée « générée par IA, à relire ».

## Conséquences

- Si l'URL est externe, des chiffres agrégés quittent le réseau : l'activation est un choix explicite de l'administrateur et doit être documentée à l'utilisateur (wiki, page Rapport d'audit).
- Le modèle peut se tromper : le texte est une aide, jamais une preuve.
- Les URL sortantes sont réservées aux administrateurs (voir `docs/securite-sorties-et-urls.md`).
