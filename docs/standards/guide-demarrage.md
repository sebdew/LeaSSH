# Guide de démarrage

Ce guide explique, pas à pas, comment mettre en place le dépôt modèle dans GitHub, puis comment l'utiliser pour chaque nouveau projet.

## Vocabulaire

| Terme | Ce que c'est |
| --- | --- |
| Branche | Une copie de travail du code. `main` est la version de référence ; on travaille sur une branche à part, puis on fusionne. |
| Merge request | Demande de fusion d'une branche dans `main`. GitHub l'appelle *pull request*. C'est là qu'ont lieu la relecture et les contrôles. |
| CI | Intégration continue : des contrôles automatiques lancés à chaque envoi de code (secrets, vulnérabilités, tests). |
| Runner | Le programme qui exécute la CI. Sur GitHub, les runners sont fournis et hébergés par GitHub (`ubuntu-latest`). |
| Workflow | Le fichier qui décrit la CI : `.github/workflows/ci.yml`. Syntaxe GitHub Actions. |
| Tag | Une étiquette posée sur une version précise du code, par exemple `v1.2.0`. |
| ADR | Une fiche qui consigne une décision technique et ses raisons. |

## Partie 1 — Mise en place (une seule fois)

### 1.1 Activer GitHub Actions

GitHub Actions est activé par défaut sur un dépôt. Vérifie-le dans **Settings → Actions → General** (*Allow all actions and reusable workflows*).

### 1.2 Runner

Rien à installer : les jobs de la CI tournent sur les runners hébergés par GitHub (`runs-on: ubuntu-latest`), qui fournissent déjà Docker, et accèdent à `github.com` pour télécharger les actions, gitleaks et Trivy. Un runner auto-hébergé n'est utile que pour un besoin particulier (réseau privé, matériel spécifique) ; dans ce cas, installe-le dans une VM ou un LXC dédié, car il a accès à toute la machine.

### 1.3 Créer le dépôt modèle

1. Dans GitHub (**New repository**), crée un dépôt vide nommé `modele-projet` (sans README ni licence).
2. Depuis le dossier de ces fichiers :

```bash
git init -b main
git add .
git commit -m "chore: dépôt modèle initial"
git remote add origin https://github.com/<toi>/modele-projet.git
git push -u origin main
```

3. Dans **Settings → General**, coche **Template repository**.
4. Vérifie dans l'onglet **Actions** que la CI passe au vert. Les cibles `make` affichent des avertissements, c'est normal pour le modèle.

Le `README.md` du modèle affiche `$REPO_NAME` : c'est voulu. GitHub ne remplace pas cette variable à la création du projet, remplace-la à la main par le nom du projet.

### 1.4 Installer les outils sur ton poste

```bash
# uv (gestionnaire Python utilisé par Spec Kit)
curl -LsSf https://astral.sh/uv/install.sh | sh
# Spec Kit
uv tool install specify-cli
```

Et ton agent de code (Claude Code ou autre).

## Partie 2 — Créer un nouveau projet

### 2.1 Créer le dépôt à partir du modèle

1. Dans GitHub, ouvre `modele-projet` puis clique sur **Use this template → Create a new repository**.
2. GitHub ne copie ni les étiquettes (*labels*) ni les tickets du modèle : recrée les étiquettes utiles dans le nouveau dépôt.
3. Clone le nouveau dépôt sur ton poste.

### 2.2 Protéger la branche main

Dans **Settings → Branches → Add branch ruleset** (ou *Add classic branch protection rule*) pour `main` :
- exiger une pull request avant fusion (ce qui interdit l'envoi direct) ;
- exiger que les vérifications de statut réussissent (*Require status checks to pass*), en sélectionnant les jobs de la CI ;
- en solo, ne pas exiger d'approbation (tu ne peux pas approuver ta propre demande).

### 2.3 Renseigner le README

Remplace la description, et indique le niveau (N1, N2 ou N3). En cas de doute, choisis N2. Si le projet a vocation à être repris en entreprise, vise N3.

### 2.4 Initialiser Spec Kit

```bash
specify init --here --force --integration claude
git status
```

Vérifie avec `git status` et `git diff` que `AGENTS.md` et `CLAUDE.md` n'ont pas été écrasés. Si c'est le cas : `git restore AGENTS.md CLAUDE.md`. Puis commite :

```bash
git add . && git commit -m "chore: initialise Spec Kit"
```

### 2.5 Créer la constitution du projet

Dans l'agent de code :

```
/speckit.constitution Reprends intégralement docs/standards/constitution.md comme base. Applique les règles du niveau indiqué dans README.md. N'ajoute que les principes propres à ce projet.
```

La syntaxe exacte des commandes peut varier selon l'agent : tape `/speckit` pour voir la liste.

## Partie 3 — Développer une fonctionnalité

1. **Ticket** : crée une issue dans GitHub avec le modèle « Fonctionnalité ». Elle reçoit un numéro, par exemple #1.
2. **Branche** :
   ```bash
   git switch main && git pull
   git switch -c feat/1-nom-court
   ```
3. **Spécification** : `/speckit.specify` en décrivant le besoin du ticket, puis `/speckit.clarify`. Relis le fichier produit dans `specs/`.
4. **Plan** : `/speckit.plan`. Pour la toute première fonctionnalité, demande aussi à l'agent de :
   - rédiger `docs/adr/0001-choix-de-la-stack.md` ;
   - créer les dossiers `backend/` et `frontend/` selon le principe 4, avec un `Dockerfile` dans chacun ;
   - compléter les cibles du `Makefile` ;
   - compléter le job `qualite` de la CI pour le langage choisi.
5. **Tâches** : `/speckit.tasks` puis `/speckit.analyze`.
6. **Implémentation** : `/speckit.implement`, puis `/speckit.converge` jusqu'au statut `Converged`.
7. **Revues** : demande à l'agent une revue de code puis une revue de sécurité (dans Claude Code : `/review` et `/security-review`). Traite les remarques.
8. **Envoi** :
   ```bash
   git add .
   git commit -m "feat: description courte (#1)"
   git push -u origin feat/1-nom-court
   ```
9. **Merge request** : dans GitHub, ouvre la pull request. Le modèle de description s'affiche : coche les points. Attends que la CI soit verte, relis le diff, puis fusionne. Écrire `Closes #1` dans la description ferme automatiquement le ticket.

Préfixes de commit les plus courants : `feat` (fonctionnalité), `fix` (correctif), `docs` (documentation), `refactor` (réorganisation sans changement de comportement), `test`, `chore` (maintenance).

## Partie 4 — Livrer une version

1. Dans `CHANGELOG.md`, renomme la section `[Non publié]` en `[1.0.0] - AAAA-MM-JJ`.
2. Choisis le numéro : le dernier chiffre pour un correctif (1.0.1), celui du milieu pour une fonctionnalité (1.1.0), le premier si la mise à jour casse la compatibilité (2.0.0).
3. Pose le tag :
   ```bash
   git switch main && git pull
   git tag v1.0.0
   git push --tags
   ```
4. Déploie en suivant `docs/exploitation.md`.

## Partie 5 — Faire évoluer le modèle

GitHub copie le modèle une seule fois, à la création du projet. Une amélioration du modèle ne se propage donc pas toute seule :
1. modifie le dépôt `modele-projet` (par merge request), et incrémente la version de `constitution.md` ;
2. dans les projets existants, recopie les fichiers de `docs/standards/` et `.github/` lors de leur prochaine évolution.

## Pour aller plus loin

Une fois ce socle rodé, dans cet ordre :
1. **Renovate** : un robot qui propose automatiquement les mises à jour de dépendances par merge request. Dependabot, intégré à GitHub, remplit le même rôle.
2. **GitHub Container Registry** (`ghcr.io`) : construire l'image en CI à chaque tag et la publier avec le `GITHUB_TOKEN` du workflow (permission `packages: write`), puis déployer par `docker compose pull`.
3. **Revue automatique en CI** : un job qui envoie le diff de chaque merge request à un modèle via le proxy d'IA et poste la revue en commentaire.
4. **Tests de bout en bout** avec Playwright pour les projets N3.
