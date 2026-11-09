# Leassh — Cahier des charges

Version 0.1 — octobre 2026

---

## 1. Contexte et objectif

Les agents IA (assistants de supervision, agents N1/N2, assistants personnels) ont besoin d'agir sur des serveurs : diagnostiquer, lire des logs, redémarrer un service. Leur donner un accès SSH revient à leur donner un shell, souvent root. Les approches par filtrage de commandes (listes noires, regex) sont contournables.

**Objectif** : fournir un produit open source, simple à installer et à utiliser, qui permet à des agents IA d'exécuter **uniquement des commandes prédéfinies** sur des serveurs, via MCP, avec des droits fins, une validation humaine des actions sensibles et une **traçabilité complète**.

### Principe fondateur

> L'IA ne tape jamais une commande : elle choisit dans un catalogue validé par un humain.

### Ce que Leassh n'est pas

- Pas un bastion SSH pour humains (pas de session interactive).
- Pas un orchestrateur de workflows (pas d'enchaînements, de cron, de conditions).
- Pas un outil de gestion de configuration (pas d'Ansible-like).

---

## 2. Utilisateurs

| Persona | Besoin |
|---|---|
| **Administrateur** | Configure serveurs, commandes, accès, clés. Approuve les propositions. |
| **Validateur** | Approuve ou refuse les exécutions en attente. Consulte le journal. |
| **Lecteur** | Consulte le journal et la configuration (audit, conformité). |
| **Agent IA** | Via MCP : liste ce qu'il peut faire, exécute, propose. |

Rôles humains v0.1 : `admin`, `approver`, `viewer`.

---

## 3. Périmètre

### 3.1 v0.1 (MVP)

- Comptes utilisateurs locaux (3 rôles), écran d'initialisation au premier lancement.
- Identités SSH (clé générée ou importée, mot de passe en option).
- Serveurs, avec épinglage de la clé d'hôte.
- Groupes de serveurs.
- Commandes (templates + paramètres typés + risque), groupes de commandes.
- Clés MCP.
- Règles d'accès avec profils `RO` / `RW`.
- Serveur MCP (HTTP streamable) avec les 5 outils.
- Validation humaine des exécutions.
- Propositions de commandes par l'IA, avec linter.
- Import / export YAML (packs), 5 packs fournis.
- Journal des exécutions et journal d'administration, avec filtres et export.
- Un webhook sortant générique (signé HMAC).
- Interface web claire / sombre, responsive.
- Binaire unique + image Docker, SQLite.

### 3.2 Versions ultérieures (hors v0.1)

- SSO OIDC pour l'interface.
- Plusieurs webhooks configurables par type d'événement ; notifications (Telegram, ntfy, e-mail).
- Connexion via bastion (ProxyJump), inventaires externes (Proxmox, phpIPAM, NetBox).
- Postgres comme base alternative.
- Transfert des journaux en syslog / OpenTelemetry.
- Plages horaires sur les règles d'accès, quotas par clé.
- Internationalisation complète (FR + EN dès la v0.1 dans la structure, traductions complètes ensuite).

---

## 4. Modèle de données

Identifiants : ULID (triables chronologiquement). Horodatages en UTC (RFC 3339).

### 4.1 `users`
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| username | text unique | |
| display_name | text | |
| password_hash | text | argon2id |
| role | enum `admin` \| `approver` \| `viewer` | |
| disabled | bool | |
| auth_provider | enum `local` \| `oidc` | `oidc` = compte créé/lié par le SSO (§9bis); pas de mot de passe local (`password_hash` vide) |
| oidc_issuer, oidc_sub | text | identité SSO; index unique partiel sur `(oidc_issuer, oidc_sub)` |
| created_at, last_login_at | timestamp | |

### 4.2 `identities` (identités SSH, réutilisables par plusieurs serveurs)
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| name | text unique | ex. « root homelab » |
| username | text | utilisateur SSH distant |
| auth_type | enum `key` \| `password` | |
| private_key_enc | blob | chiffré AES-256-GCM |
| public_key | text | affichée pour copie |
| password_enc | blob | chiffré, optionnel |
| created_at | timestamp | |

### 4.3 `servers`
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| slug | text unique | identifiant exposé à l'IA (`web-01`) |
| name | text | libellé humain |
| host, port | text, int | |
| identity_id | ref | |
| host_key_fingerprint | text | SHA256 |
| host_key_status | enum `unknown` \| `pinned` \| `mismatch` | |
| description | text | **lue par l'IA** (rôle du serveur) |
| tags | json | libres |
| enabled | bool | |
| created_at, updated_at | timestamp | |

### 4.4 `server_groups` / `server_group_members`
Groupe : id, slug, name, description. Membres : (group_id, server_id).

### 4.5 `commands`
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| slug | text unique | `mysql.processlist` |
| name | text | |
| description | text | **lue par l'IA** — quand l'utiliser, ce qu'elle renvoie, effets de bord |
| risk | enum `read` \| `write` \| `destructive` | |
| template | text | `journalctl -u {{service}} -n {{lines}} --no-pager` |
| params | json | définition des paramètres (§6) |
| timeout_s | int | défaut 30, max 600 |
| max_output_kb | int | défaut 64 |
| status | enum `pending` \| `enabled` \| `disabled` | |
| hash | text | SHA-256 de (template + params + risk) |
| version | int | incrémentée à chaque modification |
| source | enum `manual` \| `pack` \| `proposal` | |
| pack_id | text | si importée d'un pack |
| created_by, approved_by | ref users | |
| approved_at, created_at, updated_at | timestamp | |

Toute modification de `template`, `params` ou `risk` recalcule le hash et incrémente la version. Une commande issue d'une proposition repasse en `pending` si elle est modifiée par autre chose qu'un admin.

### 4.6 `command_groups` / `command_group_members`
Idem serveurs.

### 4.7 `mcp_keys`
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| name | text | « Agent N1 » |
| description | text | |
| prefix | text | 8 premiers caractères, affichés pour identification |
| hash | text | SHA-256 du jeton complet |
| expires_at | timestamp nullable | |
| last_used_at, last_used_ip | | |
| can_propose | bool | autorise `propose_command` (défaut true) |
| disabled | bool | |
| created_by, created_at | | |

Format du jeton : `lsh_` + 32 octets aléatoires en base62. Affiché **une seule fois** à la création.

### 4.8 `access_rules` / `access_rule_keys`
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| name | text | « N1 — diagnostic BDD » |
| server_group_id | ref | |
| command_group_id | ref | |
| profile | enum `RO` \| `RW` | |
| write_requires_approval | bool | défaut true (sans effet en RO) |
| enabled | bool | |

`access_rule_keys` : (rule_id, key_id).

### 4.9 `executions` — journal des exécutions

**Toute** demande `run_command` crée une ligne, y compris les refus. Les noms sont dénormalisés pour que le journal reste lisible après suppression d'un objet.

| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| key_id, key_name | | qui (agent) |
| server_id, server_slug | | où |
| command_id, command_slug, command_version, command_hash, risk | | quoi |
| params | json | valeurs fournies |
| rendered | text | commande exacte envoyée (null si refus avant rendu) |
| reason | text | raison donnée par l'agent (obligatoire) |
| rule_id, rule_name | | règle ayant autorisé |
| status | enum | `denied`, `pending_approval`, `rejected`, `expired`, `running`, `succeeded`, `failed`, `timeout`, `error` |
| denial_reason | text | ex. `server_not_visible`, `param_invalid:lines` |
| exit_code | int | |
| stdout, stderr | text | tronqués à `max_output_kb` |
| output_truncated | bool | |
| duration_ms | int | |
| requested_at, started_at, finished_at | timestamp | |
| decided_by, decided_at, decision_comment | | validation humaine |
| client_ip, mcp_session_id, user_agent | | |

### 4.10 `proposals`
| Champ | Type | Notes |
|---|---|---|
| id | ulid | |
| key_id, key_name | | |
| payload | json | slug, name, description, template, params, risk proposés |
| justification | text | |
| lint | json | résultat du linter (erreurs, avertissements) |
| status | enum `pending` \| `approved` \| `rejected` \| `invalid` | `invalid` si le lint échoue |
| reviewed_by, reviewed_at, review_comment | | |
| command_id | ref | commande créée si approuvée |
| created_at | | |

### 4.11 `audit_events` — journal d'administration
| Champ | Type | Notes |
|---|---|---|
| id, ts | | |
| actor_type | enum `user` \| `key` \| `system` | |
| actor_id, actor_name | | |
| action | text | `server.create`, `command.update`, `key.revoke`, `rule.update`, `user.login`, `user.login_failed`, `host_key.mismatch`… |
| target_type, target_id, target_name | | |
| diff | json | avant / après (secrets masqués) |
| ip | text | |

---

## 5. Règles d'autorisation

Une clé `K` peut exécuter la commande `C` sur le serveur `S` si et seulement si :

1. `K` est active, non expirée ;
2. `S` est actif et `host_key_status = pinned` ;
3. `C` est en statut `enabled` ;
4. il existe une règle `R` active telle que :
   - `K ∈ R.keys`,
   - `S ∈ R.server_group`,
   - `C ∈ R.command_group`,
   - `C.risk` est compatible avec `R.profile` :
     - `RO` → `read` uniquement,
     - `RW` → `read`, `write`, `destructive`.

**Validation humaine requise** si :
- `C.risk = destructive` (toujours), ou
- `C.risk = write` et `R.write_requires_approval = true`.

Si plusieurs règles autorisent la même combinaison, on retient la **moins contraignante** pour l'exécution, et on journalise son identifiant.

`list_servers` renvoie les serveurs pour lesquels au moins une commande est autorisée. `list_commands(server)` renvoie les commandes autorisées sur ce serveur, avec l'indication `requires_approval`.

L'écran « Simulateur d'accès » (§9) utilise exactement la même fonction d'évaluation.

---

## 6. Paramètres typés et rendu des templates

### 6.1 Syntaxe des templates
- Placeholders : `{{nom}}` (`[a-z][a-z0-9_]*`).
- **Un placeholder doit être un mot shell autonome**, jamais entre guillemets. Seule exception : il peut suivre un préfixe littéral se terminant par `=` (forme `--option=valeur`), car la valeur échappée reste un seul mot shell. ✅ `journalctl -u {{service}}` ✅ `--unit={{service}}` ❌ `echo "{{x}}"` ❌ `-c "SELECT {{x}}"` ❌ `/var/log/{{name}}.log` (utiliser un paramètre `path`).
- Conséquence assumée : un paramètre ne peut pas être injecté dans une chaîne SQL ou un script. Les besoins de ce type (ex. annuler une requête PostgreSQL par PID) relèvent d'une évolution ultérieure : un mode où les valeurs typées sont transmises sur stdin en variables (`\set` psql, `SET @x` MySQL).
- Tous les placeholders doivent être déclarés dans `params`, et tous les `params` utilisés.

### 6.2 Types
| Type | Validation | Options |
|---|---|---|
| `enum` | valeur ∈ `values` | `values` (obligatoire) |
| `int` | entier dans les bornes | `min`, `max` (obligatoires) |
| `identifier` | `^[A-Za-z0-9._@-]{1,64}$`, ne commence pas par `-` | `max_length` |
| `service` | `^[A-Za-z0-9@._:-]{1,128}$`, ne commence pas par `-` | |
| `path` | absolu, normalisé, sans `..`, préfixé par un des `allowed_prefixes` | `allowed_prefixes` (obligatoire) |
| `regex` | correspond à `pattern` ancré automatiquement, ne commence pas par `-` | `pattern`, `max_length` |

Champs communs : `description` (lue par l'IA), `required` (défaut true), `default`.

Il n'existe **aucun** type texte libre.

### 6.3 Rendu
1. Valider chaque valeur selon son type (refus → exécution `denied`, `denial_reason = param_invalid:<nom>`).
2. Remplacer chaque placeholder par la valeur **échappée en POSIX** (guillemets simples, `'` → `'\''`).
3. Vérifier que le hash courant de la commande correspond à celui de la version autorisée.
4. Le résultat (`rendered`) est journalisé tel qu'envoyé.

### 6.4 Linter (propositions et éditeur)
Erreurs bloquantes pour une **proposition** (avertissements pour un admin dans l'éditeur) :
- métacaractères shell hors placeholders : `;` `&` `|` `` ` `` `$(` `>` `<` `\n`, `${` ;
- placeholder non autonome ou entre guillemets ;
- placeholder non déclaré / paramètre inutilisé ;
- type inconnu, `enum` vide, `int` sans bornes, `path` sans préfixes ;
- template réduit à un placeholder (`{{cmd}}`) ou dont le premier mot est un placeholder ;
- premier mot dans une liste d'interpréteurs (`sh`, `bash`, `zsh`, `python*`, `perl`, `ruby`, `node`, `php`, `eval`, `exec`, `xargs`, `env`, `nohup`, `sudo`, `su`) ;
- `risk` absent.

Les commandes d'admin peuvent contenir des pipes (`ps aux | sort …`) avec avertissement ; une proposition, jamais.

---

## 7. Serveur MCP

- Transport : **HTTP streamable**, endpoint `/mcp`.
- Authentification : `Authorization: Bearer <clé MCP>`. Échec → 401, événement `key.auth_failed` journalisé (préfixe seulement).
- Limite de débit par clé (défaut 60 requêtes / minute) et nombre d'exécutions simultanées par serveur (défaut 2).
- Les descriptions d'outils doivent expliquer clairement le fonctionnement aux modèles.

### 7.1 `list_servers`
Entrée : aucune.
Sortie :
```json
{ "servers": [ { "slug": "web-01", "name": "Web 01", "description": "Frontal nginx du site public", "tags": ["web"] } ] }
```

### 7.2 `list_commands`
Entrée : `{ "server": "web-01" }`
Sortie :
```json
{
  "server": "web-01",
  "commands": [
    {
      "slug": "systemd.status",
      "name": "État d'un service",
      "description": "…",
      "risk": "read",
      "requires_approval": false,
      "params": { "service": { "type": "service", "description": "Nom de l'unité", "required": true } }
    }
  ]
}
```

### 7.3 `run_command`
Entrée :
```json
{ "server": "web-01", "command": "systemd.status", "params": { "service": "nginx" }, "reason": "Alarme Zabbix #4521 : nginx ne répond plus" }
```
`reason` est **obligatoire** (1–500 caractères) : il apparaît dans le journal et dans l'écran de validation.

Sortie (exécution directe) :
```json
{ "execution_id": "01J…", "status": "succeeded", "exit_code": 0, "stdout": "…", "stderr": "", "truncated": false, "duration_ms": 412 }
```
Sortie (validation requise) :
```json
{ "execution_id": "01J…", "status": "pending_approval", "message": "Cette commande nécessite une validation humaine. Utilisez get_execution pour suivre son statut." }
```
Sortie (refus) : erreur MCP explicite, sans révéler l'existence de serveurs ou commandes non visibles (message générique « serveur ou commande indisponible pour cette clé » + raison de validation de paramètre si applicable).

### 7.4 `get_execution`
Entrée : `{ "execution_id": "01J…", "wait_seconds": 20 }` — `wait_seconds` (0–30) permet une attente longue jusqu'à changement de statut.
Une clé ne peut lire que ses propres exécutions.

### 7.5 `propose_command`
Entrée :
```json
{
  "slug": "mysql.replica_status",
  "name": "État de la réplication",
  "description": "…",
  "template": "mysql --batch -e \"SHOW REPLICA STATUS\\G\"",
  "params": {},
  "risk": "read",
  "justification": "Nécessaire pour diagnostiquer les alarmes de retard de réplication"
}
```
Sortie : `{ "proposal_id": "01J…", "status": "pending" | "invalid", "lint": { "errors": [], "warnings": [] } }`
Disponible uniquement si `can_propose = true`. Limite : 10 propositions en attente par clé.

---

## 8. Exécution SSH

1. Connexion SSH avec l'identité du serveur ; vérification stricte de la clé d'hôte épinglée (divergence → statut `mismatch`, serveur bloqué, événement d'audit, webhook).
2. Ouverture d'un canal **`exec`** uniquement. Jamais de PTY, de shell, de forwarding (agent, X11, ports).
3. Délai `timeout_s` : à expiration, envoi de signal puis fermeture du canal, statut `timeout`.
4. Capture stdout/stderr tronquée à `max_output_kb` (indicateur `truncated`).
5. Pool de connexions avec durée de vie courte (configurable), reconnexion transparente.
6. Les exécutions en attente expirent après le délai réglé (Réglages → Général, défaut 30 min) → `expired`.
7. Au moment de l'exécution d'une demande validée, on revérifie : clé active, règle toujours valide, hash identique. Sinon → `error` avec raison.

---

## 9. Interface d'administration

Détails visuels et parcours : voir [`UX.md`](UX.md).

Écrans :
1. **Initialisation** (premier lancement) : création du compte admin, protégée par un jeton d'initialisation (`LEASSH_SETUP_TOKEN`, ou jeton aléatoire affiché une fois au démarrage).
2. **Tableau de bord** : checklist d'accueil, exécutions récentes, en attente de validation, propositions, serveurs en anomalie.
3. **Journal** : exécutions (onglet principal) et événements d'administration.
4. **Validations** : demandes en attente, approuver / refuser avec commentaire.
5. **Propositions** : revue, édition, approbation.
6. **Serveurs** et **groupes de serveurs**.
7. **Identités SSH**.
8. **Commandes** et **groupes de commandes**, éditeur avec prévisualisation et test.
9. **Règles d'accès**.
10. **Clés MCP**.
11. **Simulateur d'accès** : choisir une clé (+ un serveur) → liste des commandes autorisées, avec la règle qui les autorise et si une validation est requise.
12. **Packs** : import / export YAML, aperçu des changements avant import.
13. **Réglages** : utilisateurs, webhook, syslog, SSO (OpenID Connect), rétention, thème par défaut.

Droits :
| Action | admin | approver | viewer |
|---|---|---|---|
| Lire configuration et journaux | ✅ | ✅ | ✅ |
| Approuver / refuser une exécution | ✅ | ✅ | ❌ |
| Approuver une proposition, modifier la configuration | ✅ | ❌ | ❌ |
| Lancer un test de commande depuis l'éditeur | ✅ | ❌ | ❌ |

Un admin qui teste une commande depuis l'éditeur crée une exécution journalisée avec `actor = user`.

---

## 9bis. Connexion unique (SSO) OpenID Connect

Authentification de l'**interface d'administration** par un fournisseur OpenID Connect standard (Keycloak, Entra ID, Authentik, Google…). Configuration dans **Réglages → SSO** (admin), table `oidc_config` (ligne unique, migration 00010), paquet `internal/oidc` (découverte, JWKS, signature des ID tokens et PKCE délégués à `github.com/coreos/go-oidc/v3` et `golang.org/x/oauth2` ; algorithmes RS/PS/ES/EdDSA seulement, jamais `none` ni HMAC ; rechargement du JWKS limité à 1 par 30 s ; aucune exécution de commande).

- **Le SSO s'ajoute aux comptes locaux.** Le compte administrateur local reste utilisable en secours (formulaire mot de passe toujours présent). Le réglage « réserver le mot de passe local aux administrateurs locaux » refuse le mot de passe aux comptes locaux non admin (403 `local_login_disabled`, audité `user.login_failed` avec `reason: local_password_disabled`), seulement tant que le SSO est activé; il est refusé à l'enregistrement s'il n'existe aucun admin local actif. Les clés MCP ne passent jamais par le SSO (leurs routes sont séparées et ne lisent que `Authorization: Bearer`).
- **Flux** : Authorization Code + **PKCE S256**, `state` et `nonce` aléatoires. Découverte `<issuer>/.well-known/openid-configuration` (cache 15 min); l'`issuer` du document doit être **identique** à celui configuré; `authorization_endpoint`, `token_endpoint` et `jwks_uri` obligatoires. `GET /api/v1/auth/oidc/start` génère state/nonce/verifier, les scelle (AES-256-GCM, clé maître) dans un cookie `leassh_oidc` (HttpOnly, `SameSite=Lax` car le retour est une navigation inter-sites, `Path=/api/v1/auth/oidc`, 10 min, **à usage unique**) et redirige; `GET /api/v1/auth/oidc/callback` compare `state` en temps constant, échange le code (client_secret_basic, ou _post si seul annoncé) avec le verifier, vérifie l'ID token, résout le compte, ouvre la **session existante** (cookie `leassh_session`, jeton CSRF habituels) et redirige vers `/`. Toute erreur redirige vers `/?sso_error=<motif>` (affiché sur la page de connexion). Limitation : 5 échecs par IP / 15 min (même limiteur que le mot de passe).
- **Vérification de l'ID token** : signature par le JWKS du fournisseur (RS256/384/512, PS256/384/512, ES256/384/512, EdDSA; `none` et HMAC refusés; clé choisie par `kid` et compatible avec l'algorithme; rechargement du JWKS si `kid` inconnu, au plus toutes les 30 s), `iss` identique, `aud` contient le client ID (`azp` obligatoire et égal si plusieurs audiences), `exp` (tolérance d'horloge 60 s), `nbf`/`iat` non futurs, `nonce` égal, `sub` non vide.
- **Rôles** : revendication configurable (`groups`, `roles`, chemin pointé `realm_access.roles`), lue dans l'ID token, sinon dans **userinfo** (le `sub` doit concorder). Table de mapping valeurs → `admin`/`approver`/`viewer`; le rôle **le plus élevé** correspondant gagne. Sans correspondance : rôle par défaut `viewer`, ou **refus** (`role_not_mapped`). Le rôle est **recalculé à chaque connexion** (changement ⇒ sessions existantes fermées, événement `user.update`); un admin SSO ne peut pas être rétrogradé s'il est le dernier admin actif. Le rôle d'un compte SSO n'est pas modifiable à la main (409 `managed`).
- **Comptes** : liaison par **(issuer, sub)**, jamais par e-mail ni par nom. Première connexion : création automatique (JIT, événement `user.create`) ou refus `not_provisioned` selon le réglage. Nom d'utilisateur = `preferred_username`, sinon e-mail, sinon `sub`; s'il est déjà pris (compte local compris) on suffixe `@<hôte de l'issuer>` — le compte existant n'est **jamais** repris. Un compte SSO n'a pas de mot de passe local (pas de connexion ni de changement de mot de passe); désactivé ⇒ `account_disabled`.
- **Déconnexion** : `POST /auth/logout` détruit toujours la session locale; pour un compte SSO et si l'option est activée, la réponse porte `{end_session_url}` (RP-Initiated Logout : `end_session_endpoint?client_id=…&post_logout_redirect_uri=<base>/`) et l'interface y navigue.
- **Secret client** : chiffré AES-256-GCM avec la clé maître (réécrit par `rotate-master-key`), jamais renvoyé (`has_client_secret`), jamais écrit dans l'audit ni les logs. Vide à l'enregistrement = conservé; `clear_client_secret` l'efface (client public + PKCE).
- **API** (admin) : `GET|PUT /api/v1/settings/oidc` (renvoie aussi `redirect_url` = URL publique + `/api/v1/auth/oidc/callback`; URL publique = celle des réglages généraux si elle est définie, sinon `X-Forwarded-Host`/`X-Forwarded-Proto` d'un proxy de confiance, sinon l'hôte de la requête), `POST /settings/oidc/test` (découverte à jour de l'issuer saisi ou enregistré : endpoints, nombre de clés JWKS, avertissements `issuer_http`, `no_userinfo`, `no_end_session`, `no_pkce`). Option `local_login_disabled` (SSO obligatoire) : refuse le mot de passe local pour tous (403 `local_login_disabled`, audité); exige le SSO actif et au moins un administrateur SSO déjà connecté (400 `no_sso_admin`), et empêche de désactiver le SSO tant qu'elle est posée; secours hors interface : `leassh enable-local-login`. Public : `GET /auth/providers` → `{oidc: {name, start_url} | null, local_password_admins_only, local_login_disabled, auto_redirect}` (bouton « Se connecter avec <nom> »). Option `auto_redirect` : la page de connexion redirige d'elle-même vers le fournisseur; `/?login` (ou un échec SSO) affiche le formulaire local de secours. Un issuer en `http://` est accepté (LAN) mais signalé.
- **Audit** : `oidc.update` (avant/après sans secret, `credential_changed`), `oidc.test`, `user.sso_login` (acteur = l'utilisateur), `user.sso_login_failed` (acteur `system`; `reason` parmi `state_missing`, `state_mismatch`, `provider_error`, `token_exchange_failed`, `token_signature`, `token_issuer`, `token_audience`, `token_expired`, `token_nonce`, `role_not_mapped`, `not_provisioned`, `account_disabled`…, jamais de jeton), `user.create` (JIT), `user.update` (synchronisation de rôle).
- **Limites** : un seul fournisseur; pas de SCIM ni de déprovisionnement automatique (un utilisateur retiré du fournisseur ne peut plus se connecter mais garde sa session jusqu'à expiration — désactivez le compte pour un effet immédiat); pas de back-channel logout; avec le JIT désactivé, seuls les comptes déjà liés se connectent (pas de pré-création).

---

## 9ter. Langue de l'interface

Français (défaut) et anglais, choix dans **Réglages → Apparence → Langue**, mémorisé dans le navigateur (`localStorage`, `leassh-lang`). Les textes sont dans `web/src/locales/{fr,en}.json` (mêmes clés, mêmes variables `{{…}}`: un test Vitest l'impose). Dates et nombres suivent la langue (`lib/lang.ts`).

- **Messages d'erreur de l'API** : le client envoie `Accept-Language`; avec `en`, le middleware `langMiddleware` (`internal/api/i18n.go`) traduit `error.message` des réponses ≥ 400 à partir d'un catalogue (messages exacts ou préfixe + partie dynamique). Le `code` ne change jamais; un message absent du catalogue reste en français (messages dynamiques du linter, de la validation syslog, erreurs réseau…).
- **Synthèse IA du rapport d'audit** : `POST /audit-reports/{id}/summary` accepte `{lang: "fr"|"en"}` (sinon `Accept-Language`); le prompt système est rédigé dans la langue choisie. Le texte généré est conservé tel quel (il n'est pas retraduit si l'on change de langue ensuite).
- **Non traduit** : le contenu du rapport d'audit HTML/JSON (faits signés, intitulés des constats et des contrôles, gabarit), les détails libres stockés dans les événements d'audit, les motifs saisis par les agents, la documentation.

---

## 10. Journalisation — exigences

**Exigence centrale : on doit pouvoir répondre à « qui a lancé quoi, où, quand, pourquoi, avec quel résultat, et qui l'a validé ».**

- Chaque appel `run_command` produit une ligne `executions`, **y compris les refus** (paramètre invalide, serveur non autorisé, clé expirée…).
- Chaque appel `list_*` et `get_execution` met à jour `last_used_at` de la clé ; pas de ligne d'exécution (pour éviter le bruit), mais compteur par clé visible.
- Chaque modification de configuration produit un `audit_event` avec diff (secrets masqués).
- Connexions à l'interface (réussies / échouées) journalisées.
- Les journaux ne sont **pas modifiables** depuis l'interface ; seule la rétention automatique les purge (réglage de rétention, défaut 365, 0 = illimité).
- Chaîne d'intégrité (v0.1 recommandé) : chaque ligne d'exécution et d'audit stocke le hash de la ligne précédente, pour détecter une altération de la base. Un bouton « Vérifier l'intégrité » dans Réglages.
- Export CSV et JSON (NDJSON) des résultats filtrés.
- Mise à jour en direct du journal (Server-Sent Events).

Filtres : période, clé/agent, serveur, groupe de serveurs, commande, risque, statut, validateur, texte (raison, commande rendue).

---

## 11. Webhook

Un webhook sortant configurable (URL + secret). Corps JSON, en-tête `X-Leassh-Signature: sha256=<HMAC>`.
Événements : `execution.pending_approval`, `execution.finished`, `execution.denied`, `proposal.created`, `host_key.mismatch`.
Le payload contient un lien direct vers l'élément dans l'interface (URL publique des réglages). Trois tentatives avec backoff ; échecs visibles dans Réglages.

---

## 11bis. Export syslog

Envoi continu des journaux vers un serveur syslog (Wazuh, rsyslog, syslog-ng…). Configuration dans **Réglages → Syslog** (admin), table `syslog_config` (ligne unique, migration 00009). Paquet `internal/syslog`.

- **Contenu** : (1) les **exécutions** : `execution.denied` (tout refus, y compris authz, validation de paramètres, hash), `execution.pending_approval`, `execution.finished` (réussie, échec, délai dépassé, erreur), `execution.rejected`, `execution.expired`; (2) les **propositions** : `proposal.created`; (3) tous les événements du **journal d'administration**, sous le type `audit.<action>` (validations et refus humains `audit.execution.approve|reject`, `audit.host_key.mismatch`, `audit.syslog.update`, etc.). Aucune de ces demandes n'échappe à `executions.Run()` : l'export observe le service (`executions.Service.Observe`, en plus de `Notify` du webhook), il n'exécute rien.
- **Jamais envoyé** (invariant 7) : stdout/stderr (seulement `stdout_bytes`/`stderr_bytes`), secrets, jetons, clé privée TLS. Les champs libres (motif, commande rendue, justification) sont sur une ligne, tronqués à 256 caractères et passent par `Redact` (`password=…`, `token: …` → `***`); les diffs d'audit sont déjà masqués à l'écriture (`audit.Mask`) et tronqués à 1024 caractères.
- **Transports** : `udp` (un datagramme par message), `tcp`, `tls` (TLS ≥ 1.2; autorité de certification propre ou celles du système; nom attendu (SNI) configurable; `tls_skip_verify` possible mais signalé; certificat client facultatif pour le mTLS, sa **clé privée est chiffrée AES-256-GCM** avec la clé maître et réécrite par `rotate-master-key`). Cadrage TCP/TLS : `lf` (un message par ligne, défaut) ou `octet` (compteur d'octets, RFC 6587).
- **Format** : RFC 5424 — `<PRI>1 horodatage hôte app-name pid msgid - message`; PRI = facility × 8 + gravité; `msgid` = type de l'événement. Le message est au choix : `text` (`type clé=valeur …`), `json` (objet à plat avec `app`, `event`, `category`, `severity`, `time` + champs) ou `cef` (`CEF:0|Leassh|Leassh|<version>|<type>|<type>|<0-10>|rt=… suser=… src=… dvchost=… act=… outcome=… csN=…`). Facility (`kern`…`local7`, défaut `local0`), app-name (défaut `leassh`) et nom d'hôte émetteur sont configurables.
- **Gravité** : refus = `warning`; demande en attente, expirée, rejetée, proposition, suppression/rotation/désactivation = `notice`; exécution échouée ou en délai dépassé = `warning`, erreur interne = `err`; `host_key.mismatch` = `crit`; le reste = `info`. **Filtres** : niveau minimal (les événements moins graves sont ignorés) et types (`execution`, `proposal`, `audit`).
- **File et reprise** : file **en mémoire** bornée (10 000), non bloquante pour les appelants (`Push` ne fait qu'un verrou court). File pleine : le plus ancien est abandonné et compté (`dropped`). Reconnexion automatique avec attente exponentielle (1 s → 60 s); une connexion TCP fermée par le serveur est détectée avant l'écriture. Les événements d'audit sont lus en base **après commit** par un suiveur (toutes les 2 s) à partir d'un **curseur de livraison** enregistré (`audit_cursor`) : un redémarrage de Leassh ne perd pas le journal d'administration (au moins une fois; doublons possibles). À l'activation, l'historique existant n'est pas rejoué. Les exécutions encore en file lors d'un arrêt de Leassh sont perdues (le journal des exécutions, lui, est complet en base).
- **API** (admin) : `GET /api/v1/settings/syslog` → `{config, status, options}`; `PUT` (clé privée en écriture seule : `tls_client_key_pem`, `clear_tls_client_key`; vide = inchangée) ; `POST /settings/syslog/test` envoie un message `syslog.test` immédiatement, hors file, avec la configuration enregistrée (même désactivée) et renvoie `{ok, error}`. UDP ne peut pas confirmer la réception. `status` : état (`disabled|idle|connected|retrying`), file, envoyés, abandonnés, dernière erreur et dernier succès. Événements d'audit `syslog.update` (avant/après, sans PEM ni clé) et `syslog.test`.

---

## 12. Packs (import / export)

Format YAML :
```yaml
pack:
  id: system
  name: Système Linux
  version: 1.0.0
  description: Diagnostic système de base
commands:
  - id: system.disk_usage
    name: Occupation disque
    description: …
    risk: read
    template: df -h -x tmpfs -x devtmpfs
    params: {}
```

- L'import affiche un aperçu : commandes nouvelles, modifiées (diff), inchangées.
- Option : créer automatiquement un groupe de commandes du nom du pack.
- Les commandes importées sont en `enabled` si l'admin valide l'import ; leur `source = pack`.
- Export : un groupe, une sélection, ou tout le catalogue.

Packs fournis en v0.1 : `system`, `systemd`, `mysql`, `postgres`, `nginx`, `docker`.

---

## 13. Configuration

| Variable | Défaut | Rôle |
|---|---|---|
| `LEASSH_LISTEN` | `:8080` | adresse d'écoute (UI + MCP) |
| `LEASSH_DATA_DIR` | `/data` | base SQLite |
| `LEASSH_MASTER_KEY` | — (obligatoire) | 32 octets en base64, chiffrement des secrets |

Les autres paramètres sont en base (table `app_settings`), éditables par un administrateur dans Réglages → Général (`GET|PUT /api/v1/settings/general`, audités ; le jeton `/metrics` n'est conservé que sous forme d'empreinte) : URL publique, reverse proxies de confiance, durée de session (défaut 12 h), expiration des validations (30 min), rétention des journaux (365 j, 0 = illimité), niveau de logs, jeton `/metrics`.

Démarrage refusé si `LEASSH_MASTER_KEY` est absente ou invalide. Rotation de la clé maître via une sous-commande `leassh rotate-master-key`.

---

## 14. Exigences non fonctionnelles

- **Installation** : `docker run` avec un volume, ou binaire unique Linux amd64/arm64.
- **Performances** : 200 serveurs, 1 000 commandes, 1 million de lignes de journal sans dégradation perceptible de l'interface (pagination par curseur, index).
- **Sécurité** : cookies `HttpOnly`, `Secure`, `SameSite=Strict` ; protection CSRF ; en-têtes CSP stricts ; limite de tentatives de connexion (5 échecs par couple IP + compte / 15 min, plafond de 50 par IP) ; session expirée après 30 min d'inactivité ; `Strict-Transport-Security` (`max-age=31536000; includeSubDomains`) posé par Leassh dès que la requête est en HTTPS, **sauf si elle arrive d'un reverse proxy de confiance** (Réglages › Général): le proxy est alors seul responsable de HSTS, pour éviter un doublon d'en-têtes — déclarer son proxy dans « Reverse proxies de confiance » ; logs MCP sans contenu d'arguments (nom d'outil, clé, taille seulement) ; argon2id.
- **Accessibilité** : WCAG 2.1 AA (contrastes dans les deux thèmes, navigation clavier).
- **Observabilité** : `/health` (`/healthz` conservé), `/metrics` (Prometheus) : exécutions par statut, durée, erreurs SSH.
- **Sauvegarde** : documentation de la sauvegarde de la base + clé maître.

---

## 15. Critères d'acceptation v0.1

1. Une clé `RO` ne peut ni voir ni lancer une commande `write`.
2. Une commande `destructive` passe toujours par la validation, même avec `RW` et `write_requires_approval = false`.
3. Un paramètre contenant `; rm -rf /`, `$(id)`, `` `id` ``, `-rf`, `../../etc` est refusé ou rendu inoffensif — couvert par un corpus de tests d'injection automatisés (au moins 300 combinaisons type × charge).
4. Une proposition dont le template contient un métacaractère est enregistrée en `invalid` et ne peut pas être approuvée sans correction.
5. Une tentative refusée apparaît dans le journal avec la clé, le serveur, la commande demandée et la raison du refus.
6. Le journal permet de retrouver, pour une exécution donnée : agent, serveur, commande exacte rendue, paramètres, raison, validateur, sortie, code retour, durée.
7. Un changement de clé d'hôte bloque le serveur et apparaît dans l'audit.
8. Toutes les vues sont utilisables en thème clair et sombre, et la page Validations est utilisable sur mobile.
9. De l'installation à la première exécution par un agent : moins de 10 minutes en suivant la checklist.

---

## Annexe — Notes d'implémentation (M1–M5)

- **MCP sans état** : `/mcp` utilise le mode `Stateless` du SDK; la clé est authentifiée à chaque requête et les outils sont construits par requête (`propose_command` n'existe que si `can_propose`). Pas de `Mcp-Session-Id`.
- **Clé désactivée / expirée** : le jeton reste reconnu (pour journaliser les refus `key_inactive` / `key_expired`) mais n'a plus aucun droit; `list_*` renvoie des listes vides ou un refus générique.
- **Linter** : les erreurs de *structure* (placeholder entre guillemets, collé à du texte, premier mot placeholder, non déclaré, type invalide) sont bloquantes pour tous, y compris l'admin, car elles garantissent la sûreté de l'échappement POSIX. Métacaractères et interpréteurs sont des avertissements pour l'admin, des erreurs pour une proposition.
- **Chaîne d'intégrité des exécutions** : couvre les champs immuables de la demande (id, séquence, clé, acteur, serveur, commande, paramètres, raison, horodatage); statut et sortie évoluent après coup par conception.
- **Base** : une seule connexion SQLite; dans une transaction, n'utiliser que `tx` (jamais `DB`) sous peine d'interblocage.
- **Requêtes SQL** écrites à la main (`database/sql`) à la place de `sqlc`.
- **Test d'une commande** (admin, éditeur) : passe par `executions.Run` avec `actor = user`, sans règle d'accès ni validation, mais sur un serveur actif à clé d'hôte épinglée.
- **Épinglage de clé d'hôte** : `POST /servers/{id}/host-key` re-scanne l'hôte et exige que l'empreinte confirmée soit toujours celle présentée.

### Annexe — Journal, intégrité, utilisateurs (M6)

- **API journal** : `GET /api/v1/executions` (filtres `period|from|to`, `status` multiple, `key_id`, `server`, `server_group`, `command`, `risk`, `decided_by`, `q`; pagination `cursor`/`next_cursor`), `/executions/stream` (SSE, résumés sans stdout/stderr), `/executions/export?format=csv|ndjson` (mêmes filtres; CSV neutralise les formules tableur et n'inclut pas les sorties). Le journal d'administration a les mêmes routes sous `/audit` (`action` = préfixe, ex. `server.`).
- **Intégrité** : `POST /integrity/verify` (admin) contrôle les deux chaînes. La première ligne restante sert d'ancre (la rétention purge les plus anciennes); contenu altéré, ligne supprimée au milieu et rupture de chaînage sont détectés. La troncature de la fin de chaîne ne l'est pas (il faudrait un ancrage externe).
- **Rétention** : la rétention (Réglages → Général) purge une fois par jour, hors demandes en attente ou en cours; la purge produit un événement `logs.purge`.
- **Utilisateurs** : `/users` (admin), `/auth/password`, `/auth/theme`. Garde-fous : dernier admin actif, auto-désactivation/suppression refusées. Désactivation, changement de rôle ou de mot de passe ferment les sessions concernées.
- Non mesuré : les performances à 1 million de lignes (recherche texte `LIKE` sans index plein texte).

### Annexe — Packs, propositions, webhook (M7)

- **Packs** : les 6 packs de `packs/*.yaml` sont embarqués dans le binaire (`packs/embed.go`). `POST /packs/preview` ne modifie rien; `POST /packs/import` est atomique. Un pack fourni est de confiance (avertissements acceptés); un fichier importé doit faire accepter explicitement les avertissements (`allow_warnings`). Toute erreur de lint (placeholder entre guillemets, type inconnu…) bloque l'import, même avec avertissements acceptés. Une commande déjà présente et modifiée par le pack est mise à jour (version incrémentée, source = pack) en conservant son statut activé/désactivé. Le groupe créé porte l'id du pack.
- **Propositions** : « Modifier puis approuver » réutilise l'éditeur de commande en mode proposition (linter strict `POST /proposals/lint`, risque définitif, groupes à la création).
- **Webhook** : un seul webhook (`/settings/webhook`, admin). Secret chiffré au repos, jamais renvoyé. Enveloppe `{text, id, event, timestamp, url, data}` (`text` : résumé Markdown, requis par Mattermost/Slack ; le secret est facultatif, sans secret pas d’en-tête de signature), en-têtes `X-Leassh-Event` et `X-Leassh-Signature: sha256=<HMAC-SHA256 du corps>`. 3 tentatives (1 s puis 5 s), redirections non suivies, sorties de commandes jamais incluses. Les 200 dernières livraisons sont consultables dans Réglages. `url` pointe vers l'écran concerné si l'URL publique des réglages est définie.

### Annexe — Finitions (M8)

- **Tableau de bord** : `GET /api/v1/dashboard` (checklist à 6 étapes cochées automatiquement, chiffres 24 h, anomalies de serveurs, 10 dernières exécutions). « Agent connecté » = une clé MCP a déjà servi.
- **`/metrics`** : compteurs en mémoire (`leassh_executions_total{status}` compte les transitions vers un statut depuis le démarrage, `leassh_ssh_errors_total{kind}`, histogramme de durée) + jauges lues en base. fermée par défaut : sans jeton défini dans Réglages → Général, la route répond 401.
- **CLI** : `leassh serve | backup <fichier> | rotate-master-key | reset-password <identifiant> | healthcheck | version`. `backup` utilise `VACUUM INTO` (cohérent, serveur démarré); la clé maître n'est jamais incluse.
- **Image Docker** : multi-étapes (Node → Go → distroless non-root), `HEALTHCHECK` via `leassh healthcheck`. Non construite dans l'environnement de développement (pas de Docker disponible).

### Annexe — Bastions et robustesse MCP (post-M8)

- **Utilisateur SSH par serveur** (`servers.ssh_user`) : surcharge l'utilisateur de l'identité (Warpgate : `utilisateur:cible`).
- **Pause après échec d'authentification** : 60 s sans nouvelle tentative sur le serveur (`denial_reason = ssh_cooldown`), levée par toute modification du serveur; évite le blocage d'IP par un bastion ou fail2ban.
- **`run_command` tolérant** : enregistré en bas niveau, il journalise toute demande, même mal formée (`reason_invalid`, `target_not_found`, `param_invalid:params`); `params` accepte un objet ou un texte JSON. Les sorties `list_servers` / `list_commands` incluent `next_step`.
- Les appels MCP (méthode, outil, arguments, refus) sont consignés dans les logs applicatifs.

### Annexe — Règles multi-groupes et auto-provisioning

- **Règles** : une règle vise un **ensemble** de groupes de serveurs et un **ensemble** de groupes de commandes (tables `access_rule_server_groups` / `access_rule_command_groups`, migration 00005). Les serveurs (et commandes) couverts sont l'union des groupes. L'API prend `server_group_ids` / `command_group_ids` (au moins un de chaque).
- **Connecteurs** (`internal/provisioning`) : registre de types compilés dans le binaire (pas de chargement dynamique ni de sous-processus, invariant 10). Un connecteur ne fait que **lire** la source (`Discover`) et décrire des machines; le moteur écrit. Premier connecteur : **Warpgate** (`GET /@warpgate/api/targets`, jeton d'API utilisateur envoyé dans `X-Warpgate-Token`; seules les cibles `Ssh` sont retenues; l'utilisateur n'a pas besoin d'être admin du bastion).
- **Correspondance Warpgate** : nom de cible → nom du serveur; description → description; `ssh_user` = `<utilisateur par défaut ou celui de l'identité>:<nom exact de la cible>`; hôte/port = ceux du bastion; identité = celle du connecteur (posée à la création du serveur, ensuite modifiable). Slug dérivé du nom, stable ensuite. Les groupes Warpgate deviennent des groupes de serveurs; une cible sans groupe est importée sans groupe.
- **Propriété et verrous** : `managed_by` / `external_id` sur serveurs et groupes, `server_group_members.managed` pour les appartenances posées par la synchro. Un serveur synchronisé n'est pas supprimable et ses nom, slug, hôte, port et description sont verrouillés (409 `managed`); identité, **utilisateur SSH**, étiquettes et état restent modifiables. Un utilisateur SSH modifié à la main (`ssh_user_custom`) n'est plus écrasé par la synchro (sinon il suit `<utilisateur>:<nom de la cible>`). La case « Suivre la synchro » (`ssh_user_follow_sync`) rend la main à la synchro; la valeur est recalculée à la synchronisation suivante. Un groupe synchronisé n'est ni modifiable ni supprimable, mais accepte des membres hors synchro; ses membres synchronisés ne peuvent pas être retirés. Un serveur synchronisé peut rejoindre d'autres groupes; la synchro ne touche jamais aux groupes, appartenances ou règles posés par un admin.
- **Cycle de vie** : machine disparue de la source → serveur **désactivé** (`sync_removed`), jamais supprimé (le journal y fait référence); réapparue → réactivé seulement si c'est la synchro qui l'avait désactivé. Groupe disparu → supprimé s'il est vide, sinon rendu à la gestion manuelle. Suppression d'un connecteur → tout est conservé et redevient manuel.
- **Règles par défaut** : chaque *nouveau* groupe synchronisé est ajouté une fois aux règles choisies sur le connecteur (donc accès immédiat des clés de ces règles). Rien n'est rétabli si un admin retire ensuite le groupe d'une règle. Sans règle choisie, aucune machine n'est accessible tant qu'un admin n'a pas créé de règle.
- **Clé d'hôte** : les machines d'un bastion partagent sa clé; l'admin l'épingle une fois sur le connecteur (test → empreinte → « Faire confiance », `POST /provisioners/{id}/host-key`), les serveurs la reçoivent. Sans empreinte épinglée, ils restent `unknown` et inutilisables. Une divergence détectée (`mismatch`) n'est jamais effacée par une synchro; seul un nouvel épinglage de l'admin la lève.
- **Garde-fous** : aperçu (`POST /provisioners/{id}/preview`) = même calcul que l'application, exécuté puis annulé. Une synchro qui désactiverait ≥ 3 machines et plus de 30 % du parc, ou toutes si la source est vide, est refusée (409 `sync_blocked`) sauf `force`. Synchro périodique (`interval_min`, 0 = manuelle) et manuelle; chaque synchro qui change quelque chose écrit un événement `provisioner.sync` (détail des changements). Le jeton d'API est chiffré avec la clé maître et jamais renvoyé.

### Annexe — Packs réseau et virtualisation

- **Packs fournis** (9) : `system` (1.1.0), `systemd`, `mysql`, `postgres`, `nginx`, `docker`, **`pfsense`** (75 commandes), **`proxmox`** (60), **`unifi`** (27, switch et point d'accès). Chaque pack porte une `category` (champ optionnel de `pack:`) qui sert au regroupement dans l'écran Packs.
- **Reprise des propositions validées** : les commandes `pve.*` (6) et `system.*` (5) proposées par l'IA et approuvées en production sont reprises **à l'identique** dans `proxmox` et `system`; l'aperçu d'import les signale « inchangées » (elles restent de source `proposal`).
- **Prudence** : aucun pack ne lit de secret (`/conf/config.xml` pfSense, `/etc/pve/priv`, `system.cfg` et `/etc/persistent` UniFi). Pas de suppression de VM ou de conteneur Proxmox, pas de réinitialisation d'usine UniFi: à proposer au cas par cas. Les actions qui coupent du trafic ou des données (`qm stop`, `pct stop`, rollback et suppression de snapshot, `pfctl -F states`, redémarrages, `set-inform`) sont `destructive` (validation toujours humaine).
- **Test** : les packs `pfsense`, `proxmox` et `unifi` n'ont aucun avertissement de lint (aucun pipe, redirection ni interpréteur) et sont validés par le linter et l'import atomique, **mais n'ont pas été exécutés sur du matériel réel**: les commandes absentes d'un firmware échouent sans effet.
- **Écran Packs** : recherche (nom, description, identifiants de commandes), filtres par catégorie et par état, sections par catégorie, une ligne compacte par pack (nombre de commandes, répartition lecture/écriture/destructive, état). L'API `GET /packs` renvoie pour chaque pack fourni `category`, `risks` et `command_ids`.

### Annexe — Statistiques d'usage du tableau de bord

- `GET /api/v1/dashboard/insights?days=7|30|90` (défaut 30) : demandes, taux de succès (réussies / terminées), durée moyenne, médiane, p95 et maximum des exécutions lancées, délai moyen de décision humaine, activité **horaire en UTC** (le client regroupe par jour et par jour de semaine × heure dans son fuseau), top 10 des commandes, serveurs et clés, 5 commandes les plus lentes (≥ 3 exécutions), principales causes de refus, répartition par risque (hors refus).
- Écran : bascule 7/30/90 jours, cinq indicateurs, barres empilées par jour (réussies, échecs, refusées, autres) avec tableau équivalent, heatmap semaine × heure (une teinte, intensité = volume), trois classements à barres. Aucune bibliothèque de graphiques: HTML/CSS et jetons de couleur, légende textuelle et icônes en plus des couleurs.

### Annexe — Rapport d'audit (ISO 27001 / HDS)

- **Principe** : les **faits** sont calculés de façon déterministe par `internal/auditreport` à partir de la base, sans IA. Une synthèse rédigée par une IA est facultative, stockée à part, **hors de l'empreinte**, et n'utilise que des agrégats.
- **Instantané** (`audit_reports.snapshot`, JSON compact, `schema_version` 1, migration 00008) : configuration (rétention, TTL, HTTPS, webhook), périmètre à la date de génération (serveurs, groupes, commandes par risque et origine, clés MCP avec état/expiration/dernière utilisation, règles RO/RW avec ou sans validation, comptes, identités, connecteurs), activité de la période (issues, refus par cause, validations humaines avec délai et décideur, écritures sans validation, demandes destructives détaillées, événements de sécurité), intégrité (vérification complète des deux chaînes de hash, plage de séquences, **tête de chaîne**, rétention, purges), changements d'administration (≤ 1000 listés, totaux exacts), constats et correspondance aux contrôles. Un second calcul du même état donne le même JSON à l'octet près.
- **Constats** (règles fixes, gravité élevée/moyenne/faible/information) : règle RW sans validation, clés actives sans expiration ou inutilisées depuis 90 jours, comptes admin inactifs depuis 90 jours, serveur sans clé d'hôte épinglée ou divergente, rupture d'intégrité d'un journal, rétention < 365 jours, interface sans HTTPS, pas de webhook, un seul admin, identités par mot de passe, échecs de synchro, nombreux échecs d'authentification, commandes issues de propositions. Les seuils sont indicatifs.
- **Correspondance** : 14 contrôles de l'annexe A de l'ISO/IEC 27001:2022 (5.15, 5.16, 5.17, 5.18, 5.28, 5.33, 8.2, 8.3, 8.5, 8.9, 8.15, 8.16, 8.24, 8.32) et 4 thèmes HDS **sans numéro de clause** (traçabilité, habilitations, conservation, cloisonnement). Statut « éléments de preuve », « points mineurs » ou « points d'attention » (constat de gravité moyenne ou haute): jamais un verdict de conformité.
- **Export** : page HTML autonome (CSS intégré, aucun script ni ressource externe, imprimable en PDF); JSON signé `{format: "leassh-audit-report/1", signature, snapshot}`. `signature.digest` = SHA-256 du JSON compact de `snapshot` (`jq -cj .snapshot f.json | sha256sum`); `signature.tag` = HMAC-SHA256 dérivé de la clé maître (`Box.Derive("audit-report")`) sur digest + id + date: seule l'instance émettrice peut le recalculer. `POST /audit-reports/verify` (corps = fichier) répond `digest_ok` et `tag_ok`.
- **API** (admin) : `GET|POST /audit-reports` (`{from,to}` en AAAA-MM-JJ UTC, défaut 30 jours, max 400, pas de futur), `GET /audit-reports/{id}` (`/html`, `/html?download=1`, `/json`), `POST /audit-reports/{id}/summary`, `GET|PUT /settings/audit-ai`. Événements d'audit `audit_report.generate`, `audit_report.summary`, `audit_ai.update` (jamais le contenu ni la clé d'API). Les rapports ne se suppriment pas.
- **Synthèse IA** : désactivée par défaut. API de type OpenAI (`POST {url}/chat/completions`, `Authorization: Bearer`), URL, modèle et clé configurables (clé chiffrée avec la clé maître, réécrite par `rotate-master-key`, jamais renvoyée). `AIInput` est une liste blanche de **comptes et d'intitulés fixes** (nombre de clés, de règles, de demandes, constats): aucun nom de serveur, clé, utilisateur ou commande, aucun motif, aucune sortie, aucune adresse; un test échoue si un nom seedé en sort. Le texte est étiqueté « générée par IA, à relire ».
- **Correctif lié** : `rotate-master-key` ne rechiffrait pas le jeton d'API des connecteurs de provisioning (il devenait illisible après rotation); il est désormais rechiffré, comme la clé d'API de la synthèse.

### Annexe — Sorties non fiables et URL sortantes (SEC-07, SEC-08)

- **Sortie de commande** : renvoyée telle quelle à l'agent. Une sortie contrôlée par un tiers peut porter une injection de prompt; l'effet reste borné au catalogue, aux droits de la clé et à la validation humaine des écritures. Traitement suivi dans le ticket #36; détail dans [securite-sorties-et-urls.md](securite-sorties-et-urls.md).
- **URL sortantes** (webhook, IA, connecteurs, issuer OIDC) : `http(s)` seulement, adresses privées autorisées (usage interne), redirections non suivies pour le webhook et l'IA. Réservé aux administrateurs.
