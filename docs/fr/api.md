**Français** · [English](../en/api-reference.md)

# Référence de l'API

L'interface d'administration utilise une **API JSON** (`/api/v1/…`). La spécification machine est [`docs/openapi.json`](../openapi.json) (OpenAPI 3.0.3) ; chaque corps de requête et de réponse y a son schéma par ressource ; un test échoue si une route du routeur n'y figure pas, si un champ d'un schéma diffère du type Go correspondant, ou si une réponse réelle de l'API ne respecte pas la spécification. Cette page la décrit route par route (105 opérations). Le transport MCP (`/mcp`) suit le protocole MCP : voir [Agents et outils MCP](agents-mcp.md).

## Conventions

- **Authentification** : cookie de session `leassh_session` (obtenu par `POST /api/v1/auth/login`, le SSO ou l'initialisation) et, pour toute mutation (`POST`, `PUT`, `DELETE`), en-tête **`X-CSRF-Token`** (valeur `csrf_token` renvoyée à la connexion). `/metrics` utilise un jeton `Authorization: Bearer`.
- **Rôles** : `admin` (tout), `approver` (décisions), lecture pour tout compte connecté. Les droits sont toujours vérifiés côté serveur (403 sinon). Les routes « public » n'exigent aucune session.
- **Format d'erreur** : `{ "error": { "code": "…", "message": "…" } }`. Le `code` ne change jamais ; `message` suit `Accept-Language` (fr ou en).
- **Pagination** par curseur : `GET` de liste avec `cursor` ; la réponse porte `next_cursor`.
- **Dates** en ISO 8601 UTC ; **identifiants** ULID.
- **Secrets** : jamais renvoyés (`has_client_secret`, etc.) ; un champ vide à l'enregistrement conserve la valeur existante.
- **Journal** : filtres de `GET /executions` : `period|from|to`, `status` (multiple), `key_id`, `server`, `server_group`, `command`, `risk`, `decided_by`, `q`. Même principe pour `/audit` (`action` = préfixe, ex. `server.`). Exports `?format=csv|ndjson`.

### Exemple

```bash
# Connexion, puis liste des serveurs
curl -c cj -H 'Content-Type: application/json' -d '{"username":"admin","password":"…"}' https://leassh.example.com/api/v1/auth/login
curl -b cj https://leassh.example.com/api/v1/servers

# Mutation : jeton CSRF obligatoire
curl -b cj -H "X-CSRF-Token: <csrf_token>" -H 'Content-Type: application/json' \
  -d '{"decision":"approve","comment":"OK"}' https://leassh.example.com/api/v1/executions/<id>/decision
```

## Routes

### Authentification et session

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | public | Ouvre une session locale |
| `POST` | `/api/v1/auth/logout` | tout rôle connecté | Ferme la session |
| `GET` | `/api/v1/auth/me` | tout rôle connecté | Utilisateur courant |
| `GET` | `/api/v1/auth/oidc/callback` | public | Retour du fournisseur SSO (redirection) |
| `GET` | `/api/v1/auth/oidc/start` | public | Démarre la connexion SSO (redirection) |
| `POST` | `/api/v1/auth/password` | tout rôle connecté | Change son mot de passe local |
| `GET` | `/api/v1/auth/providers` | public | Modes de connexion disponibles |
| `PUT` | `/api/v1/auth/theme` | tout rôle connecté | Enregistre le thème de l'utilisateur |

### Journal d'administration

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/audit` | tout rôle connecté | Journal d'administration |
| `GET` | `/api/v1/audit/export` | tout rôle connecté | Exporte le journal d'administration |

### Rapports d'audit

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/audit-reports` | admin | Liste les rapports d'audit |
| `POST` | `/api/v1/audit-reports` | admin | Génère un rapport d'audit |
| `POST` | `/api/v1/audit-reports/verify` | admin | Vérifie l'intégrité d'un rapport |
| `GET` | `/api/v1/audit-reports/{id}` | admin | Détail d'un rapport d'audit |
| `GET` | `/api/v1/audit-reports/{id}/html` | admin | Rapport d'audit en HTML |
| `GET` | `/api/v1/audit-reports/{id}/json` | admin | Rapport d'audit en JSON |
| `POST` | `/api/v1/audit-reports/{id}/summary` | admin | Génère la synthèse IA d'un rapport |

### Exécutions et validations

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/executions` | tout rôle connecté | Journal des exécutions |
| `GET` | `/api/v1/executions/export` | tout rôle connecté | Exporte le journal des exécutions |
| `GET` | `/api/v1/executions/stream` | tout rôle connecté | Flux temps réel des exécutions (SSE) |
| `GET` | `/api/v1/executions/{id}` | tout rôle connecté | Détail d'une exécution |
| `POST` | `/api/v1/executions/{id}/decision` | admin, approver | Approuve ou refuse une exécution |

### Serveurs

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/servers` | tout rôle connecté | Liste les serveurs |
| `POST` | `/api/v1/servers` | admin | Crée un serveur |
| `DELETE` | `/api/v1/servers/{id}` | admin | Supprime un serveur |
| `GET` | `/api/v1/servers/{id}` | tout rôle connecté | Détail d'un serveur |
| `PUT` | `/api/v1/servers/{id}` | admin | Modifie un serveur |
| `POST` | `/api/v1/servers/{id}/host-key` | admin | Épingle la clé d'hôte |
| `POST` | `/api/v1/servers/{id}/test-connection` | admin | Teste la connexion SSH |

### Groupes de serveurs

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/server-groups` | tout rôle connecté | Liste les groupes de serveurs |
| `POST` | `/api/v1/server-groups` | admin | Crée un groupe de serveurs |
| `DELETE` | `/api/v1/server-groups/{id}` | admin | Supprime un groupe de serveurs |
| `PUT` | `/api/v1/server-groups/{id}` | admin | Modifie un groupe de serveurs |
| `PUT` | `/api/v1/server-groups/{id}/members` | admin | Remplace les membres d'un groupe de serveurs |

### Identités SSH

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/identities` | tout rôle connecté | Liste les identités SSH |
| `POST` | `/api/v1/identities` | admin | Crée une identité SSH |
| `DELETE` | `/api/v1/identities/{id}` | admin | Supprime une identité SSH |

### Provisioning

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/provisioners` | tout rôle connecté | Liste les provisionneurs |
| `POST` | `/api/v1/provisioners` | admin | Crée un provisionneur |
| `GET` | `/api/v1/provisioners/types` | tout rôle connecté | Types de provisionneurs |
| `DELETE` | `/api/v1/provisioners/{id}` | admin | Supprime un provisionneur |
| `PUT` | `/api/v1/provisioners/{id}` | admin | Modifie un provisionneur |
| `POST` | `/api/v1/provisioners/{id}/host-key` | admin | Épingle la clé d'hôte d'un provisionneur |
| `POST` | `/api/v1/provisioners/{id}/preview` | admin | Aperçu d'une synchronisation |
| `POST` | `/api/v1/provisioners/{id}/sync` | admin | Synchronise les serveurs |
| `POST` | `/api/v1/provisioners/{id}/test` | admin | Teste un provisionneur |

### Commandes

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/commands` | tout rôle connecté | Liste les commandes |
| `POST` | `/api/v1/commands` | admin | Crée une commande |
| `POST` | `/api/v1/commands/lint` | admin | Analyse une commande (linter) |
| `POST` | `/api/v1/commands/preview` | admin | Aperçu du rendu d'une commande |
| `DELETE` | `/api/v1/commands/{id}` | admin | Supprime une commande |
| `GET` | `/api/v1/commands/{id}` | tout rôle connecté | Détail d'une commande |
| `PUT` | `/api/v1/commands/{id}` | admin | Modifie une commande |
| `POST` | `/api/v1/commands/{id}/test` | admin | Teste une commande sur un serveur |

### Groupes de commandes

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/command-groups` | tout rôle connecté | Liste les groupes de commandes |
| `POST` | `/api/v1/command-groups` | admin | Crée un groupe de commandes |
| `DELETE` | `/api/v1/command-groups/{id}` | admin | Supprime un groupe de commandes |
| `PUT` | `/api/v1/command-groups/{id}` | admin | Modifie un groupe de commandes |
| `PUT` | `/api/v1/command-groups/{id}/members` | admin | Remplace les membres d'un groupe de commandes |

### Packs

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/packs` | tout rôle connecté | Liste les packs |
| `GET` | `/api/v1/packs/export` | tout rôle connecté | Exporte des commandes en YAML |
| `POST` | `/api/v1/packs/import` | admin | Importe un pack |
| `POST` | `/api/v1/packs/preview` | admin | Aperçu d'un import de pack |

### Propositions

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/proposals` | tout rôle connecté | Liste les propositions |
| `POST` | `/api/v1/proposals/lint` | admin | Analyse une proposition |
| `GET` | `/api/v1/proposals/{id}` | tout rôle connecté | Détail d'une proposition |
| `POST` | `/api/v1/proposals/{id}/approve` | admin | Accepte une proposition |
| `POST` | `/api/v1/proposals/{id}/reject` | admin | Rejette une proposition |

### Règles d'accès

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/rules` | tout rôle connecté | Liste les règles d'accès |
| `POST` | `/api/v1/rules` | admin | Crée une règle d'accès |
| `DELETE` | `/api/v1/rules/{id}` | admin | Supprime une règle d'accès |
| `PUT` | `/api/v1/rules/{id}` | admin | Modifie une règle d'accès |

### Clés MCP

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/keys` | tout rôle connecté | Liste les clés MCP |
| `POST` | `/api/v1/keys` | admin | Crée une clé MCP (jeton affiché une seule fois) |
| `DELETE` | `/api/v1/keys/{id}` | admin | Supprime une clé MCP |
| `PUT` | `/api/v1/keys/{id}` | admin | Modifie une clé MCP |
| `POST` | `/api/v1/keys/{id}/revoke` | admin | Révoque une clé MCP |

### Simulateur et tableau de bord

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/dashboard` | tout rôle connecté | Tableau de bord |
| `GET` | `/api/v1/dashboard/insights` | tout rôle connecté | Statistiques d'usage |
| `GET` | `/api/v1/meta` | tout rôle connecté | Paramètres publics pour l'interface |
| `GET` | `/api/v1/simulator` | tout rôle connecté | Simule les droits d'une clé |

### Réglages

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/settings/audit-ai` | admin | Lit la configuration (synthèse IA) |
| `PUT` | `/api/v1/settings/audit-ai` | admin | Modifie la configuration (synthèse IA) |
| `GET` | `/api/v1/settings/general` | admin | Lit la configuration (réglages généraux) |
| `PUT` | `/api/v1/settings/general` | admin | Modifie la configuration (réglages généraux) |
| `GET` | `/api/v1/settings/oidc` | admin | Lit la configuration (SSO) |
| `PUT` | `/api/v1/settings/oidc` | admin | Modifie la configuration (SSO) |
| `POST` | `/api/v1/settings/oidc/test` | admin | Teste la configuration SSO |
| `GET` | `/api/v1/settings/syslog` | admin | Lit la configuration (syslog) |
| `PUT` | `/api/v1/settings/syslog` | admin | Modifie la configuration (syslog) |
| `POST` | `/api/v1/settings/syslog/test` | admin | Envoie un message de test (syslog) |
| `GET` | `/api/v1/settings/webhook` | admin | Lit la configuration (webhook) |
| `PUT` | `/api/v1/settings/webhook` | admin | Modifie la configuration (webhook) |
| `GET` | `/api/v1/settings/webhook/deliveries` | admin | Historique des envois du webhook |
| `POST` | `/api/v1/settings/webhook/test` | admin | Envoie un événement de test (webhook) |

### Utilisateurs

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `GET` | `/api/v1/users` | admin | Liste les utilisateurs |
| `POST` | `/api/v1/users` | admin | Crée un utilisateur |
| `DELETE` | `/api/v1/users/{id}` | admin | Supprime un utilisateur |
| `PUT` | `/api/v1/users/{id}` | admin | Modifie un utilisateur |

### Initialisation et sondes

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `POST` | `/api/v1/setup` | public | Crée le premier administrateur (jeton d'initialisation requis) |
| `GET` | `/api/v1/setup/status` | public | Indique si l'instance est initialisée |

### Autres

| Méthode | Chemin | Rôle | Description |
|---|---|---|---|
| `POST` | `/api/v1/integrity/verify` | admin | Vérifie la chaîne d'intégrité des journaux |
| `GET` | `/health` | public | Sonde de vie |
| `GET` | `/healthz` | public | Sonde de vie (ancien nom de /health) |
| `GET` | `/metrics` | tout rôle connecté | Métriques au format Prometheus |

---

[← Précédent : Architecture](../architecture.md) · [Accueil](README.md) · [Suivant : Dépannage et FAQ →](depannage.md)
