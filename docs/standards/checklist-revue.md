# Checklist de revue d'un projet

À passer avant chaque mise en production, et à chaque changement de niveau. Chaque point indique le niveau à partir duquel il est exigé (voir `constitution.md`). Coche si c'est conforme, commente sinon.

## 1. Cadrage

- [ ] [N1] Le `README.md` indique l'objet du projet et son niveau.
- [ ] [N1] La fonctionnalité a une spécification écrite dans `specs/` avant le code.
- [ ] [N2] Le cadrage répond aux questions de base : usage interne ou externe, un ou plusieurs modules, connexions aux outils du socle.
- [ ] [N2] Un plan technique découpé en tâches existe pour chaque fonctionnalité.
- [ ] [N1] Le projet a été créé à partir du dépôt modèle, et `docs/standards/` n'a pas été modifié.

## 2. Socle

- [ ] [N2] Pas de login maison : l'authentification passe par Keycloak (OIDC).
- [ ] [N1] Les appels aux modèles d'IA utilisent l'API compatible OpenAI, configurée par `LLM_BASE_URL`, `LLM_API_KEY` et `LLM_MODEL`, avec une clé dédiée au projet.
- [ ] [N1] Aucun SDK propre à un fournisseur ni fonction propre au proxy : changer de proxy ne demande qu'une modification du `.env`.
- [ ] [N2] `/health` et `/metrics` existent et sont supervisés dans Zabbix ou Grafana.
- [ ] [N3] Les appels sortants (base, API, proxy d'IA) sont mesurés pour repérer les lenteurs.

## 3. Stack

- [ ] [N1] Le langage et le framework font partie de la liste standard, ou un ADR justifie l'exception.
- [ ] [N1] Le choix de la stack est consigné dans `docs/adr/0001-choix-de-la-stack.md`.
- [ ] [N1] Un seul langage pour le backend, un seul pour le front.
- [ ] [N2] Le front repose sur React ou Vue, en TypeScript.

## 3 bis. Réutilisation

- [ ] [N1] Aucun code maison pour la cryptographie, l'authentification, les sessions ou l'analyse de formats standards.
- [ ] [N2] Chaque nouvelle dépendance est justifiée dans le plan (raison, alternatives écartées).
- [ ] [N2] Les dépendances ajoutées sont maintenues, largement utilisées et sous licence compatible.
- [ ] [N2] Avant le lancement du projet, on a vérifié qu'aucune application open source existante ne répondait déjà au besoin.

## 3 ter. Séparation front et back

- [ ] [N2] Le backend expose une API REST documentée en OpenAPI.
- [ ] [N2] Le front ne contient ni règle métier, ni secret, ni accès direct à la base ou au proxy d'IA.
- [ ] [N2] Les droits sont vérifiés côté backend.
- [ ] [N2] `backend/` et `frontend/` sont séparés, chacun avec ses dépendances et son conteneur.
- [ ] [N2] Dans le backend, les routes appellent des services métier ; aucune règle métier dans les routes ni dans le SQL.

## 4. Structure et exploitation

- [ ] [N1] Les dépendances ont des versions fixées, le fichier de verrouillage est commité.
- [ ] [N2] L'application tourne en conteneur, sans utilisateur root.
- [ ] [N2] Les logs sortent en JSON sur la sortie standard, sans secret.
- [ ] [N2] `docs/exploitation.md` décrit mise à jour, sauvegarde, restauration et retour arrière.
- [ ] [N2] La restauration a été testée au moins une fois.

## 5. Sécurité

- [ ] [N1] Aucun secret dans le code ni dans l'historique (job gitleaks vert).
- [ ] [N1] `.env.example` est à jour, `.env` n'est pas commité.
- [ ] [N2] Aucune vulnérabilité HIGH ou CRITICAL corrigeable (job Trivy vert).
- [ ] [N2] Revue OWASP Top 10 faite sur les dernières modifications.
- [ ] [N2] Si IA : revue OWASP Top 10 for LLM faite (injection de prompt, droits des outils, fuite de données).
- [ ] [N3] Exigences OWASP ASVS niveau 1 vérifiées (niveau 2 pour l'authentification et les droits).
- [ ] [N3] L'exposition Internet passe par un point d'entrée maîtrisé (reverse proxy, Cloudflare), en HTTPS uniquement.

## 6. Qualité

- [ ] [N2] Chaque modification part d'un ticket, sur une branche dédiée, avec une merge request.
- [ ] [N2] La CI est verte avant chaque fusion, et la branche `main` est protégée.
- [ ] [N2] Une revue de code par agent est faite, puis une relecture humaine du diff.
- [ ] [N2] `make lint` et `make test` sont définis et non vides.
- [ ] [N3] Les parcours principaux sont testés de bout en bout (Playwright).

## 7. Documentation et traçabilité

- [ ] [N2] Les commits suivent Conventional Commits et citent le ticket.
- [ ] [N2] Chaque version a un tag SemVer et une entrée dans `CHANGELOG.md`.
- [ ] [N2] `docs/architecture.md` est à jour, avec son schéma.
- [ ] [N2] Les tickets clos indiquent ce qui a été réalisé.

## 8. Données

- [ ] [N2] `docs/architecture.md` liste les données stockées et leur durée de conservation.
- [ ] [N3] Si données personnelles : base légale, minimisation et droit à l'effacement traités (RGPD).
- [ ] [N3] Environnements séparés : dev et prod n'utilisent ni la même base ni les mêmes clés.
