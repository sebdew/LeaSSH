# Leassh — Installation et exploitation

De zéro à la première exécution par un agent en moins de 10 minutes.

## 1. Prérequis

- Un hôte Linux (amd64 ou arm64) avec Docker, **ou** le binaire unique `leassh`.
- Un nom de domaine et un **reverse proxy TLS** (Caddy, nginx, Traefik…) : les cookies de l'interface sont `Secure`, et les agents distants doivent joindre `/mcp` en HTTPS.
- Un accès SSH aux serveurs à piloter (vous y ajouterez une clé publique).

## 2. Démarrage rapide (Docker)

```bash
export LEASSH_MASTER_KEY="$(openssl rand -base64 32)"
echo "$LEASSH_MASTER_KEY"   # ⚠️ notez-la dans votre gestionnaire de mots de passe : voir §8

docker run -d --name leassh --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v leassh-data:/data \
  -e LEASSH_MASTER_KEY \
  gitea.dewarlez.fr/sebastien/leassh:latest
```

Avec Compose : copiez [`docker-compose.yml`](../docker-compose.yml), créez un fichier `.env` contenant `LEASSH_MASTER_KEY=…` puis `docker compose up -d`.

> **`LEASSH_MASTER_KEY` chiffre les clés privées SSH et le secret du webhook.** Sans elle, ces secrets sont irrécupérables. Conservez-la **hors** de la sauvegarde de la base (§8).

## 3. Installation du binaire

```bash
curl -L -o leassh https://…/leassh-linux-amd64 && chmod +x leassh && sudo mv leassh /usr/local/bin/
sudo useradd --system --home /var/lib/leassh --create-home --shell /usr/sbin/nologin leassh
```

`/etc/leassh/env` (mode `0600`, propriétaire `root`) :

```
LEASSH_MASTER_KEY=…
LEASSH_DATA_DIR=/var/lib/leassh
LEASSH_LISTEN=127.0.0.1:8080
```

`/etc/systemd/system/leassh.service` :

```ini
[Unit]
Description=Leassh (accès SSH contrôlé pour agents IA)
After=network-online.target

[Service]
User=leassh
EnvironmentFile=/etc/leassh/env
ExecStart=/usr/local/bin/leassh serve
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/leassh
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now leassh
```

## 4. Reverse proxy

### Caddy

```
leassh.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1        # flux en direct du journal (SSE)
    }
}
```

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name leassh.example.com;
    # … certificats …
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    location = /api/v1/executions/stream {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;          # SSE
        proxy_read_timeout 1h;
    }
}
```

Dites ensuite à Leassh quel proxy croire pour l'IP réelle des clients (journal, limitation de connexions) :
dans **Réglages → Général → Reverse proxies de confiance** (ex. `127.0.0.1`, ou le réseau Docker du proxy, ex. `172.16.0.0/12`). Sans cela, les en-têtes `X-Forwarded-*` sont ignorés. Renseignez aussi l'**URL publique** (fixe, recommandée) ; à défaut elle est déduite de `X-Forwarded-Host`/`X-Forwarded-Proto`, que le proxy doit alors transmettre.

## 5. Premiers pas (checklist du tableau de bord)

1. Ouvrez l'URL, saisissez le **jeton d'initialisation** (voir ci-dessous) et **créez le compte administrateur** (10 caractères minimum).

   > **Jeton d'initialisation.** Tant qu'aucun compte n'existe, `POST /api/v1/setup` exige un jeton, sinon la première personne qui atteint l'instance en deviendrait administratrice. Définissez `LEASSH_SETUP_TOKEN` (par exemple `openssl rand -base64 24`) ou, à défaut, relevez le jeton aléatoire affiché **une seule fois** au démarrage (`docker logs leassh`, ou `journalctl -u leassh`). Il n'est jamais écrit dans la base ni dans les logs structurés, et n'est plus utile une fois le compte créé (vous pouvez retirer la variable).
2. **Identités SSH** → *Nouvelle identité* → « Générer une clé ed25519 ». Copiez la ligne affichée.
3. Sur chaque serveur cible, ajoutez la clé publique :
   `echo '<clé publique>' >> ~/.ssh/authorized_keys` (voir §9 pour un utilisateur restreint).
4. **Serveurs** → *Ajouter un serveur*, puis **Tester la connexion** et **Faire confiance à cette clé** (épinglage de l'empreinte).
5. **Packs** → installez *Système Linux* (cochez la création du groupe de commandes).
6. **Serveurs → Groupes** : créez un groupe contenant vos serveurs.
7. **Règles** → *Nouvelle règle* : groupe de serveurs + groupe de commandes + profil `RO`.
8. **Clés MCP** → *Nouvelle clé*, associez la règle, **copiez le jeton** (affiché une seule fois).
9. Collez la configuration proposée dans votre agent, puis demandez-lui de lister les serveurs.

Le **Simulateur d'accès** montre à tout moment, pour une clé, ce qu'elle peut faire et pourquoi.

## 6. Connecter un agent

Endpoint MCP (HTTP streamable) : `https://leassh.example.com/mcp`, en-tête `Authorization: Bearer lsh_…`.

```bash
claude mcp add --transport http leassh https://leassh.example.com/mcp \
  --header "Authorization: Bearer lsh_xxxxxxxx"
```

Cinq outils : `list_servers`, `list_commands`, `run_command`, `get_execution`, `propose_command`. Les commandes qui exigent une validation humaine renvoient `pending_approval` : l'agent suit l'état avec `get_execution(wait_seconds)`, et vous validez depuis **Validations** (utilisable sur mobile).

## 7. Configuration

| Variable | Défaut | Rôle |
|---|---|---|
| `LEASSH_MASTER_KEY` | — (**obligatoire**) | 32 octets en base64 ; chiffre les secrets |
| `LEASSH_LISTEN` | `:8080` | adresse d'écoute (interface + MCP) |
| `LEASSH_DATA_DIR` | `/data` | dossier de la base SQLite (`leassh.db`) |
| `LEASSH_SETUP_TOKEN` | généré au démarrage | jeton d'initialisation (instance neuve uniquement) ; affiché une fois s'il n'est pas fourni |

Tout le reste se règle dans l'interface, **Réglages → Général** (stocké en base, appliqué à chaud) : URL publique, reverse proxies de confiance, durée des sessions, expiration des validations, rétention des journaux, niveau de logs, jeton `/metrics`. Les variables d'environnement (clé maître, répertoire de données, adresse d'écoute, `LLM_*`) sont listées dans [`.env.example`](../.env.example).

## 8. Sauvegarde et restauration

**Deux éléments, à conserver séparément** :

1. la **base** (`leassh.db`) : configuration, journaux, secrets *chiffrés* ;
2. la **clé maître** `LEASSH_MASTER_KEY` : sans elle, les clés privées SSH de la base sont inutilisables.

Une sauvegarde de la base ne suffit donc pas, et la clé maître ne doit pas être stockée au même endroit que la base.

### Sauvegarder (serveur en marche)

```bash
# Docker
docker exec leassh /leassh backup /data/leassh-$(date +%F).db
docker cp leassh:/data/leassh-$(date +%F).db ./backups/

# Binaire
LEASSH_DATA_DIR=/var/lib/leassh leassh backup /var/backups/leassh-$(date +%F).db
```

La commande produit une copie cohérente (`VACUUM INTO`), mode `0600`. Planifiez-la (cron, timer systemd) et chiffrez/déplacez les copies hors de la machine.

### Restaurer

1. Arrêtez Leassh.
2. Remplacez `leassh.db` du dossier de données par la sauvegarde (supprimez `leassh.db-wal` et `leassh.db-shm` s'ils existent).
3. Redémarrez avec la **même** `LEASSH_MASTER_KEY`.
4. Dans **Réglages → Journal**, lancez *Vérifier l'intégrité*.

### Changer la clé maître

Serveur **arrêté**, après une sauvegarde :

```bash
LEASSH_MASTER_KEY="<ancienne>" LEASSH_NEW_MASTER_KEY="$(openssl rand -base64 32)" leassh rotate-master-key
```

Tous les secrets sont rechiffrés en une transaction (rien n'est modifié si l'un d'eux ne se déchiffre pas). Remplacez ensuite `LEASSH_MASTER_KEY` par la nouvelle valeur avant de redémarrer.

### Mot de passe administrateur perdu

Sur la machine qui héberge les données (le serveur peut rester démarré) :

```bash
LEASSH_DATA_DIR=/data leassh reset-password <identifiant>
```

Le nouveau mot de passe est saisi au terminal (10 caractères minimum). Le compte est réactivé, ses sessions sont fermées et l'opération est inscrite au journal d'administration (`user.password_reset_cli`). En Docker : `docker exec -it leassh leassh reset-password <identifiant>`.

## 9. Préparer les serveurs cibles

- Créez un utilisateur dédié plutôt que `root` quand c'est possible, et donnez-lui uniquement ce que le catalogue exige (par ex. `sudoers` limité aux commandes du pack utilisées).
- Les commandes sont exécutées via un canal SSH `exec` : jamais de shell interactif, de PTY ni de redirection de ports. Les packs fournis n'utilisent pas `sudo` ; adaptez-les à votre environnement (les identifiants MySQL/PostgreSQL sont lus sur le serveur cible, jamais transmis par Leassh).
- Pour contraindre davantage, préfixez la clé publique dans `authorized_keys` : `restrict,from="<IP de Leassh>" ssh-ed25519 AAAA…`.
- **Derrière un bastion (Warpgate…)** : plusieurs serveurs partagent le même hôte et le même port; chacun se distingue par l'utilisateur SSH. Renseignez dans la fiche du serveur le champ *Utilisateur SSH* (ex. `ia:reservations-dev - LXC ct145`, soit `utilisateur:nom exact de la cible` pour Warpgate, espaces compris); il remplace l'utilisateur de l'identité. Ajoutez la clé publique de l'identité à l'utilisateur Warpgate, et validez l'empreinte du bastion. Warpgate bloque l'IP après plusieurs échecs d'authentification : si les journaux Warpgate indiquent « SSH auth from blocked IP », débloquez l'IP de Leassh dans Warpgate. De son côté, Leassh suspend 60 s les tentatives sur un serveur dont l'authentification vient d'échouer (statut `ssh_cooldown` dans le journal), pour ne pas aggraver le blocage.
- Si l'empreinte d'un serveur change (réinstallation), Leassh **bloque** le serveur : examinez l'empreinte dans **Serveurs** avant d'« Accepter la nouvelle clé ».

### Synchroniser les serveurs depuis Warpgate

1. Dans Warpgate, créez (ou choisissez) un utilisateur dont le rôle donne accès aux cibles SSH voulues, et générez-lui un **jeton d'API** (profil de l'utilisateur). Il n'a pas besoin d'être administrateur.
2. Dans LeaSSH : **Infra → Provisioning → Ajouter un connecteur** : URL de Warpgate, jeton, hôte et port SSH du bastion, identité SSH (celle dont la clé publique est déclarée sur l'utilisateur Warpgate) et, si besoin, l'utilisateur par défaut (sinon celui de l'identité).
3. **Tester**, comparez l'empreinte de la clé d'hôte du bastion, puis **Faire confiance à cette clé**.
4. Choisissez éventuellement des **règles par défaut** (les nouveaux groupes y sont ajoutés), puis **Synchroniser** : l'aperçu liste les changements avant de les appliquer.

Les serveurs synchronisés portent l'icône ↻ et ne sont pas supprimables; une cible retirée de Warpgate est désactivée, pas supprimée.

### Packs pour pfSense, Proxmox et UniFi

Installez-les depuis **Catalogue → Packs** (aperçu, puis import avec création du groupe de commandes), puis ajoutez le groupe à une règle d'accès.

- **pfSense** : le compte SSH doit accepter les commandes (root, ou un utilisateur dont le shell est `/bin/sh`); l'utilisateur `admin` par défaut ouvre le menu de la console, qui ne convient pas. Aucune commande ne lit `config.xml`.
- **Proxmox** : les commandes agissent sur le nœud auquel Leassh se connecte; déclarez un serveur par nœud. Compte `root@pam` ou un compte avec les droits nécessaires (`qm`, `pct`, `pvesh`).
- **UniFi** : activez le SSH dans les paramètres d'appareil du contrôleur et utilisez ce compte. Les commandes `swctrl` (switch) et `wlanconfig`/`iwconfig` (point d'accès) n'existent que sur les modèles concernés.

### Rapport d'audit (ISO 27001, HDS)

**Audit** (menu de gauche, administrateurs): choisissez une période, **Générer le rapport**, puis **Ouvrir** (page HTML autonome: imprimez-la en PDF depuis le navigateur) ou téléchargez le **JSON signé**. Le rapport reprend l'inventaire, l'activité, la vérification d'intégrité des deux journaux, les changements de configuration, des constats automatiques et une correspondance **indicative** aux contrôles ISO 27001:2022 et aux thèmes HDS. C'est une aide à l'audit, pas une certification.

- **Remettre à l'auditeur** : le HTML (ou le PDF), le JSON signé, et la tête de chaîne des journaux qui figure dans le rapport (elle permet de détecter une troncature ultérieure). Il peut recalculer l'empreinte avec `jq -cj .snapshot rapport.json | sha256sum`; **Vérifier un rapport** confirme en plus que le fichier vient de votre instance.
- **Synthèse rédigée par une IA** (facultatif, désactivée par défaut) : renseignez l'URL d'une API de type OpenAI (OpenAI, LiteLLM, Ollama…), le modèle et la clé, ou définissez `LLM_BASE_URL`, `LLM_MODEL` et `LLM_API_KEY` (voir [`.env.example`](../.env.example)) : ces variables servent de valeurs par défaut, ce qui est saisi dans l'interface les remplace. L'activation reste un choix de l'administrateur. Seuls des chiffres agrégés et des intitulés de constats sont envoyés, jamais de nom ni de sortie de commande; si l'URL est externe, ces chiffres quittent votre réseau. Le texte est étiqueté « générée par IA, à relire » et reste hors de l'empreinte.
- La rétention des journaux (réglage « Rétention des journaux », 365 jours par défaut) limite la période auditable.

## 10. Observabilité

- `GET /health` → `ok` (`/healthz` reste accepté) (utilisé par le `HEALTHCHECK` de l'image via `leassh healthcheck`).
- `GET /metrics` (Prometheus) : **fermé par défaut** (401 tant qu'aucun jeton n'est défini dans **Réglages → Général**). Définissez un jeton et présentez-le avec `Authorization: Bearer <jeton>`. En complément, bloquez `/metrics` au niveau du reverse proxy si Prometheus est sur le même réseau (exemple Caddy : `@metrics path /metrics` puis `respond @metrics 403` pour les clients hors `10.0.0.0/8`, ou n'exposer `/metrics` que sur un port interne). Exposées : `leassh_executions_total{status}`, `leassh_execution_duration_seconds`, `leassh_ssh_errors_total{kind}`, `leassh_pending_approvals`, `leassh_servers{host_key_status}`, `leassh_mcp_keys{state}`, `leassh_outbound_duration_seconds{target,result}` (durée des appels sortants : `database` pour les transactions d'écriture, `webhook`, `ai`, `idp`)…
- **Mise à jour des images de base** : le `Dockerfile` fige Node 24 LTS, Go et distroless par condensat `@sha256:`. Pour les mettre à jour : `docker buildx imagetools inspect <image>:<tag>`, remplacer le condensat, reconstruire et tester.
- **Publication** : `docker login gitea.dewarlez.fr` puis `docker buildx build --platform linux/amd64,linux/arm64 -t gitea.dewarlez.fr/sebastien/leassh:<version> --push .`

```yaml
scrape_configs:
  - job_name: leassh
    authorization: { credentials: "<jeton /metrics>" }
    static_configs: [{ targets: ["leassh.example.com"] }]
    scheme: https
```

Alertes utiles : `leassh_servers{host_key_status="mismatch"} > 0`, `leassh_pending_approvals > 0` depuis plus de 10 min, `rate(leassh_ssh_errors_total[5m]) > 0`.

## 11. Envoyer les journaux à un serveur syslog (Wazuh)

**Réglages → Syslog** envoie les exécutions (demandes, refus, validations, résultats), les propositions et le journal d'administration. Jamais de sortie de commande ni de secret. Voir SPEC §11bis pour le détail.

1. Côté serveur, ouvrez un récepteur syslog. Exemple Wazuh (`/var/ossec/etc/ossec.conf` du manager) : TCP + TLS n'est pas pris en charge nativement par `<remote>`; utilisez TCP ou UDP, ou placez `rsyslog`/`stunnel` devant pour le TLS.

```xml
<remote>
  <connection>syslog</connection>
  <port>514</port>
  <protocol>tcp</protocol>
  <allowed-ips>192.168.6.0/24</allowed-ips>
</remote>
```

2. Dans Leassh : protocole `TCP`, serveur et port ci-dessus, **format JSON**, facility `local0`, niveau minimal `information`. **Envoyer un message de test**, puis vérifiez dans `/var/ossec/logs/archives/archives.log` (activez `<logall_json>yes</logall_json>` le temps du test).

3. Décodeur et règles (`/var/ossec/etc/decoders/leassh_decoder.xml` et `/var/ossec/etc/rules/leassh_rules.xml`), à valider avec `wazuh-logtest` : l'en-tête RFC 5424 peut être retiré ou non par le prédécodeur selon votre version, ajustez alors le `prematch`.

```xml
<decoder name="leassh">
  <prematch>"app":"leassh"</prematch>
</decoder>
<decoder name="leassh-json">
  <parent>leassh</parent>
  <regex type="pcre2">"event":"([^"]+)".*?"severity":"(\w+)"</regex>
  <order>leassh.event,leassh.severity</order>
</decoder>
```

```xml
<group name="leassh,">
  <rule id="100500" level="3">
    <decoded_as>leassh</decoded_as>
    <description>Leassh: $(leassh.event)</description>
  </rule>
  <rule id="100501" level="8">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^execution.denied$</field>
    <description>Leassh: commande refusée à un agent</description>
  </rule>
  <rule id="100502" level="12">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^audit.host_key.mismatch$</field>
    <description>Leassh: divergence de clé d'hôte SSH (serveur bloqué)</description>
  </rule>
  <rule id="100503" level="6">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^execution.pending_approval$</field>
    <description>Leassh: commande en attente de validation humaine</description>
  </rule>
  <rule id="100504" level="5">
    <if_sid>100500</if_sid>
    <field name="leassh.event">^audit\.</field>
    <description>Leassh: modification d'administration</description>
  </rule>
</group>
```

Avec le format **CEF**, utilisez le décodeur CEF de votre SIEM; avec **texte**, un décodeur `regex` sur `^execution\.\w+ id=`. Pour rsyslog : `module(load="imtcp") input(type="imtcp" port="514")` suffit; en TLS, `StreamDriver.Mode="1"` avec l'autorité fournie à Leassh. Si le serveur est injoignable, Leassh garde jusqu'à 10 000 messages en mémoire et reprend seul; l'état et la dernière erreur sont dans l'écran.

## 12. Connexion SSO (OpenID Connect, Keycloak pas à pas)

Le SSO permet de se connecter à l'**interface** avec Keycloak (ou Entra ID, Authentik, Google…). Le compte administrateur créé à l'initialisation sert à la première connexion et à configurer le SSO ; une fois le SSO validé, vous pouvez désactiver le mot de passe local (étape 8, [ADR 0001](adr/0001-authentification-sso-et-compte-local.md)). Les clés MCP ne passent pas par le SSO.

**Avant de commencer** : Leassh doit être joignable par l'URL que vos utilisateurs saisissent (Réglages → Général → URL publique, par ex. `https://leassh.example.com`, ou à défaut l'adresse utilisée dans le navigateur). Le serveur Leassh doit pouvoir joindre Keycloak (découverte, jetons, JWKS) et l'horloge des deux machines doit être à l'heure (NTP; tolérance de 60 s).

### Côté Keycloak

1. Choisissez le realm (ou créez-en un, par ex. `leassh`). L'**issuer** sera `https://<hôte-keycloak>/realms/leassh` (Keycloak ≥ 17; avant : `/auth/realms/leassh`).
2. **Clients → Create client** : *Client type* `OpenID Connect`, *Client ID* `leassh`.
3. *Capability config* : **Client authentication = On** (client confidentiel), *Standard flow* coché, le reste décoché.
4. *Login settings* — saisissez les deux URL affichées dans **Réglages → SSO** (colonne de droite, bouton Copier) :
   - *Valid redirect URIs* : `https://leassh.example.com/api/v1/auth/oidc/callback` (exactement);
   - *Valid post logout redirect URIs* : `https://leassh.example.com/` ;
   - *Web origins* peut rester vide.
5. Onglet **Advanced → Advanced settings** : *Proof Key for Code Exchange Code Challenge Method* = `S256`.
6. Onglet **Credentials** : copiez le *Client secret*.
7. **Groupes** : créez par ex. `leassh-admins`, `leassh-approvers`, `leassh-readers` (Groups) et placez-y vos utilisateurs.
8. **Mapper des groupes** : *Clients → leassh → Client scopes → leassh-dedicated → Add mapper → By configuration → Group Membership* : *Name* `groups`, *Token Claim Name* `groups`, **Full group path = On** (valeurs `/leassh-admins`) ou Off (valeurs `leassh-admins`), *Add to ID token = On*, *Add to userinfo = On*. (Pour utiliser les rôles du realm à la place, la revendication est `realm_access.roles`, ajoutée à l'access token par défaut : activez-la aussi dans l'ID token ou le userinfo via un mapper *User Realm Role*.)

### Côté Leassh (Réglages → SSO)

1. *Nom du fournisseur* : `Keycloak` (texte du bouton « Se connecter avec Keycloak »).
2. *Issuer* : `https://<hôte-keycloak>/realms/leassh`, puis **Tester la découverte** : vous devez voir les endpoints, le nombre de clés de signature et d'éventuels avertissements (issuer en HTTP, pas de déconnexion fournisseur…).
3. *Client ID* `leassh`, *Secret client* (collez celui de l'étape 6; il est chiffré et plus jamais affiché). *Scopes* : `openid profile email` (ajoutez `groups` ou `roles` si votre fournisseur l'exige).
4. *Rôles* : *Revendication* `groups`; une valeur par ligne dans chaque colonne, par ex. `/leassh-admins` → Administrateur, `/leassh-approvers` → Validateur, `/leassh-readers` → Lecteur. Le rôle le plus élevé gagne. *Sans correspondance* : « Lecteur » ou « Refuser la connexion » (recommandé en production).
5. *Comptes* : laissez « créer automatiquement le compte » pour que chaque nouvelle personne autorisée obtienne un compte à sa première connexion. Les comptes sont liés à l'identifiant `sub` du fournisseur, **jamais à l'e-mail ni au nom** : un compte local portant le même nom n'est pas repris, le compte SSO reçoit un nom suffixé.
6. *Réserver le mot de passe local aux administrateurs locaux* : à cocher une fois le SSO validé (les admins locaux gardent le secours). *Fermer aussi la session chez le fournisseur* : utilise l'URL de déconnexion de l'étape 4.
7. Cochez **Activer le SSO**, enregistrez, puis testez dans une fenêtre privée en gardant votre session admin ouverte. Chaque connexion et chaque échec sont dans le journal d'administration (`user.sso_login`, `user.sso_login_failed` avec le motif).
8. *Désactiver le mot de passe local (SSO obligatoire)* : une fois qu'un administrateur SSO s'est connecté avec succès, cochez cette option et enregistrez. Plus aucun compte local ne peut se connecter par mot de passe, administrateur compris. Si le SSO tombe, réactivez le local sur le serveur : `LEASSH_DATA_DIR=/data leassh enable-local-login` (Docker : `docker exec leassh leassh enable-local-login`).

### Autres fournisseurs

| Fournisseur | Issuer | Notes |
|---|---|---|
| Entra ID | `https://login.microsoftonline.com/<tenant>/v2.0` | Enregistrement d'application, secret client, redirection *Web*; revendication `groups` (ID de groupes) via *Token configuration* ou `roles` via *App roles*. |
| Authentik | `https://<hôte>/application/o/<slug>/` | Fournisseur OAuth2/OpenID, scope `groups` (ajouté au mapping *profile*). |
| Google | `https://accounts.google.com` | Pas de groupes : laissez la revendication vide et utilisez le rôle par défaut (ou refusez), le rôle admin se gère ensuite par le secours local. |

### Problèmes courants

| Symptôme (`?sso_error=`) | Cause probable |
|---|---|
| `discovery_failed` | Issuer erroné (slash final, `/auth` en trop), Keycloak injoignable depuis Leassh, ou l'`issuer` annoncé diffère (mauvais nom d'hôte / `KC_HOSTNAME`). |
| `token_exchange_failed` | Secret client faux, URL de redirection non déclarée à l'identique, PKCE non accepté. |
| `token_expired` / `token_not_yet_valid` | Horloges désynchronisées. |
| `token_audience` | Le client ID ne correspond pas (ou mapper d'audience manquant). |
| `role_not_mapped` | La revendication est absente du jeton (mapper non ajouté à l'ID token / userinfo) ou aucune valeur ne correspond (chemin complet `/groupe` ou non). |
| `state_missing` | Cookie perdu : retour par un autre navigateur ou une autre adresse que celle du départ, ou lien déjà utilisé. Utilisez toujours la même URL. |

Un issuer en `http://` fonctionne (réseau local) mais le code d'autorisation et le secret circulent en clair : préférez HTTPS.

## 13. Mise à jour

Sauvegardez (§8), puis `docker pull` + recréation du conteneur (ou remplacement du binaire). Les migrations de base s'appliquent automatiquement au démarrage ; il n'y a pas de retour arrière automatique, d'où la sauvegarde.

## 14. Dépannage

| Symptôme | Piste |
|---|---|
| Démarrage refusé : `LEASSH_MASTER_KEY est obligatoire` | Variable absente, pas du base64, ou pas 32 octets (`openssl rand -base64 32`). |
| Connexion à l'interface impossible derrière un proxy | Le proxy doit servir en HTTPS et transmettre `X-Forwarded-Proto: https` (ou renseignez l'URL publique `https://…` dans Réglages → Général). |
| « Trop de tentatives » à la connexion | Limite de 5 échecs / 15 min par IP et par compte ; vérifiez les reverse proxies de confiance (Réglages → Général) si tous les clients semblent avoir la même IP. |
| Le journal en direct ne se met pas à jour | Le proxy met en mémoire tampon : désactivez-la pour `/api/v1/executions/stream` (§4). |
| Le bouton SSO renvoie une erreur ou un compte SSO est refusé | Voir §12 (« Problèmes courants »); le motif exact est dans le journal d'administration (`user.sso_login_failed`). Le compte admin local reste utilisable. |
| Serveur « clé d'hôte divergente » | Réinstallation ou attaque : vérifiez l'empreinte puis *Accepter la nouvelle clé*. |
| Un agent reçoit « serveur ou commande indisponible » | Volontairement générique ; utilisez le **Simulateur d'accès** et consultez le journal (la raison exacte du refus y figure). |
