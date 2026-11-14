# Constitution des projets

Ce document fixe les règles communes à tous mes projets. Il sert de base à la constitution Spec Kit de chaque projet (`/speckit.constitution`) et s'impose à l'agent de code.

Version : 1.1 — dernière révision : novembre 2026.

## Niveaux de projet

Chaque projet déclare son niveau dans son `README.md`. Une règle s'applique à partir du niveau indiqué entre crochets.

| Niveau | Nom | Exemple | Exigence |
| --- | --- | --- | --- |
| N1 | Prototype | Test d'une idée, script jetable | Pas de secret dans Git, pas d'exposition Internet |
| N2 | Usage interne | Outil du homelab, utilisé au quotidien | Cadre complet sauf sécurité renforcée |
| N3 | Exposé ou entreprise | Accessible depuis Internet, ou destiné à être repris en entreprise | Cadre complet |

Un projet qui monte de niveau doit d'abord remplir les règles du nouveau niveau.

## Principe 1 — S'appuyer sur le socle, ne pas le redévelopper

- [N2] **Authentification** : OpenID Connect via Keycloak. Aucun stockage local de mots de passe, aucun écran de login maison. Les droits viennent des rôles ou groupes Keycloak.
- [N1] **Modèles d'IA** : tous les appels utilisent l'API compatible OpenAI (`/v1/chat/completions`, `/v1/embeddings`…), à travers le proxy d'IA du socle, aujourd'hui LiteLLM. Le code ne dépend jamais d'un proxy ni d'un fournisseur précis :
  - l'adresse du proxy, la clé et le nom du modèle sont des variables d'environnement (`LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`) ;
  - le client utilisé est un SDK compatible OpenAI (ou une librairie qui l'est), jamais le SDK propre à un fournisseur (Anthropic, Mistral, Google…) ;
  - aucune fonction propre à LiteLLM n'est utilisée dans le code ;
  - chaque projet a sa propre clé, limitée aux modèles dont il a besoin.

  Changer de proxy ou de modèle ne demande ainsi qu'une modification du fichier `.env`.
- [N1] **Code source** : GitHub, un dépôt par projet, créé à partir du dépôt modèle.
- [N2] **Supervision** : l'application expose `/health` et `/metrics` (format Prometheus), lisibles par Zabbix et Grafana. Pas d'outil de supervision embarqué.
- [N2] **Images** : les images Docker sont publiées dans le registre de conteneurs de GitHub (`ghcr.io`).

## Principe 2 — Réutiliser plutôt que réinventer

Avant d'écrire du code, l'agent cherche une solution existante, dans cet ordre :

1. **Le socle** (principe 1).
2. **Le langage et le framework retenus** : leurs fonctions natives (validation, ORM, routage, gestion des dates, sérialisation…).
3. **Une dépendance déjà présente dans le projet.**
4. **Une librairie reconnue**, qui remplit tous ces critères : maintenue (version publiée dans l'année), largement utilisée, licence compatible (MIT, Apache 2.0, BSD de préférence), sans vulnérabilité critique non corrigée.
5. **Du code maison**, seulement si aucune des solutions précédentes ne convient.

Règles associées :

- [N1] On ne développe jamais soi-même : la cryptographie, le hachage de mots de passe, l'authentification, la gestion des sessions et des jetons, l'analyse de formats standards (JSON, CSV, XML, dates, e-mails, URL).
- [N1] On n'ajoute pas une grosse dépendance pour quelques lignes de code.
- [N2] Le plan technique liste chaque nouvelle dépendance, avec sa raison et les alternatives écartées.
- [N2] Avant de lancer un projet entier, on vérifie qu'une application open source existante ne répond pas déjà au besoin. Si elle existe, on l'installe ou on y contribue plutôt que de la recoder.

## Principe 3 — Une stack connue et justifiée

- [N1] Le langage et le framework sont choisis au départ, parmi la liste ci-dessous, et le choix est consigné dans un ADR (`docs/adr/0001-choix-de-la-stack.md`).
- [N1] Un projet utilise le moins de langages possible : un pour le backend, un pour le front.
- [N2] Toute technologie hors liste demande un ADR qui explique pourquoi la liste ne suffit pas.

| Situation | Choix par défaut |
| --- | --- |
| Backend, cas général | Python (FastAPI) |
| Gros projet découpé en modules | Laravel (PHP) |
| Temps réel (websockets, files de messages) | Node.js (NestJS) |
| Calcul intensif | Go |
| Interface web | React ou Vue, en TypeScript |
| Base de données | PostgreSQL (SQLite accepté en N1) |

## Principe 4 — Séparer le front et le back

- [N2] Le backend expose une API HTTP (REST, en JSON), documentée au format OpenAPI. Il porte toute la logique métier, les droits d'accès et l'accès aux données.
- [N2] Le front ne fait qu'afficher et appeler l'API. Il ne contient aucune règle métier, aucun secret, aucun accès direct à la base de données ni au proxy d'IA.
- [N2] Les droits sont toujours vérifiés côté backend, même si le front masque déjà un bouton.
- [N2] Front et back vivent dans deux dossiers séparés (`backend/` et `frontend/`), avec chacun ses dépendances, et tournent dans deux conteneurs.
- [N2] Dans le backend, la logique métier est séparée des couches techniques : les routes API appellent des services métier, qui appellent l'accès aux données. Une règle métier n'est jamais écrite dans une route ni dans une requête SQL.
- [N1] Un projet sans interface (API seule, script, agent) n'a qu'un dossier `backend/`. Un prototype N1 peut regrouper front et back, mais le passage en N2 impose la séparation.

Structure attendue :

```
backend/
  app/
    api/        # routes HTTP : reçoivent la requête, appellent les services
    services/   # logique métier
    data/       # accès à la base et aux API externes (dont le proxy d'IA)
    core/       # configuration, logs, authentification
  tests/
frontend/
  src/
docs/
specs/
```

Les noms des sous-dossiers peuvent suivre les conventions du framework choisi, à condition de garder la même séparation.

## Principe 5 — Configuration et secrets hors du code

- [N1] Aucun secret (clé d'API, mot de passe, jeton) dans le code ni dans l'historique Git. La CI le vérifie avec gitleaks.
- [N1] Toute la configuration passe par des variables d'environnement. Le fichier `.env.example` liste chaque variable avec une valeur factice et un commentaire.
- [N2] Le même code et la même image tournent en dev et en prod : seules les variables changent.

## Principe 6 — Conteneurisé et exploitable

- [N2] L'application tourne dans des conteneurs Docker, lancés par `docker-compose.yml`.
- [N2] Les images n'utilisent pas l'utilisateur root, partent d'une image de base officielle avec une version fixée, et restent aussi légères que possible.
- [N2] Les logs sont écrits sur la sortie standard, en JSON, avec un niveau (info, warning, error). Ils ne contiennent ni secret ni donnée personnelle en clair.
- [N2] Le fichier `docs/exploitation.md` décrit l'installation, la mise à jour, la sauvegarde, la restauration et le retour arrière. La restauration a été testée au moins une fois.
- [N3] Les temps de réponse des appels sortants (base de données, API externes, proxy d'IA) sont mesurés et exposés dans `/metrics`.

## Principe 7 — Sécurité

- [N1] Les dépendances ont des versions fixées (fichier de verrouillage commité).
- [N2] La CI analyse les vulnérabilités des dépendances et des Dockerfile avec Trivy. Aucune vulnérabilité critique n'est acceptée sans justification écrite dans la merge request.
- [N2] Le code respecte l'OWASP Top 10. Les entrées utilisateur sont validées côté backend, les requêtes SQL sont paramétrées, les en-têtes de sécurité HTTP sont posés.
- [N2] Si le projet utilise un modèle d'IA : il respecte l'OWASP Top 10 for LLM Applications (injection de prompt, fuite de données, outils et droits limités au strict nécessaire).
- [N3] Le projet vise le niveau 1 de l'OWASP ASVS, et le niveau 2 pour les fonctions d'authentification et de gestion des droits.
- [N3] Une revue de sécurité par un agent dédié passe sur chaque merge request.

## Principe 8 — Spécifier avant de coder

- [N1] Chaque fonctionnalité part d'une spécification écrite (`/speckit.specify`) qui dit quoi et pourquoi, avant tout code.
- [N2] Chaque fonctionnalité suit la chaîne Spec Kit complète : specify, clarify, plan, tasks, analyze, implement, converge. Chaque étape est relue avant la suivante.
- [N2] Le plan technique montre comment il respecte les principes 2 (réutilisation) et 4 (séparation front et back).

## Principe 9 — Tests et qualité

- [N2] Chaque fonctionnalité livre ses tests automatisés. Le correctif d'un bug livre le test qui reproduit ce bug.
- [N2] La logique métier (dossier `services/`) est couverte par des tests unitaires.
- [N2] `make lint` et `make test` fonctionnent et passent en CI. Rien n'est fusionné sans CI verte.
- [N3] Les parcours utilisateurs principaux sont couverts par des tests de bout en bout dans un navigateur (Playwright).
- [N2] Le code est lisible : noms explicites, fonctions courtes, commentaires qui expliquent le pourquoi et non le comment.

## Principe 10 — Traçabilité

- [N2] Chaque modification part d'un ticket GitHub, se fait sur une branche dédiée (`feat/12-nom-court`, `fix/15-nom-court`) et arrive dans `main` par une merge request.
- [N2] Les messages de commit suivent Conventional Commits et citent le ticket : `feat: ajoute l'export CSV (#12)`.
- [N2] Une revue de code par un agent passe sur la merge request, puis je relis le diff avant de fusionner.
- [N2] Chaque version livrée porte un tag SemVer (`v1.2.0`) et une entrée dans `CHANGELOG.md`.
- [N2] Chaque décision technique structurante fait l'objet d'un ADR.

## Principe 11 — Documentation

- [N1] Le `README.md` dit à quoi sert le projet, son niveau, et comment le lancer.
- [N2] `docs/architecture.md` décrit les composants et les flux, avec un schéma Mermaid.
- [N2] L'API est documentée automatiquement au format OpenAPI, générée à partir du code.
- [N2] La documentation est mise à jour dans la même merge request que le code qu'elle décrit.

## Gouvernance de ce document

Ce document vit dans le dépôt modèle. Une évolution se fait par merge request sur le dépôt modèle, avec une nouvelle version. Les projets existants reprennent les changements lors de leur prochaine évolution.

| Version | Changements |
| --- | --- |
| 1.1 | Connexion aux modèles d'IA par API compatible OpenAI, indépendante du proxy. Ajout des principes « Réutiliser plutôt que réinventer » et « Séparer le front et le back ». |
| 1.0 | Version initiale. |
