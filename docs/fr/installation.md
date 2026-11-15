**Français** · [English](../en/installation.md)

# Installation

De zéro à la première exécution par un agent en moins de 10 minutes.

## 1. Prérequis

- Un hôte Linux (amd64 ou arm64) avec Docker, **ou** le binaire unique `leassh`.
- Un nom de domaine et un **reverse proxy TLS** (Caddy, nginx, Traefik…) : les cookies de l'interface sont `Secure` et les agents distants doivent joindre `/mcp` en HTTPS.
- Un accès SSH aux serveurs à piloter (vous y déposerez une clé publique).

## 2. Variables d'environnement

| Variable | Défaut | Rôle |
|---|---|---|
| `LEASSH_MASTER_KEY` | — (**obligatoire**) | 32 octets en base64 (`openssl rand -base64 32`). Chiffre les clés privées SSH et tous les secrets. |
| `LEASSH_LISTEN` | `:8080` | Adresse d'écoute (interface et MCP). |
| `LEASSH_DATA_DIR` | `/data` | Dossier de la base SQLite (`leassh.db`). |
| `LEASSH_SETUP_TOKEN` | généré au démarrage | Jeton d'initialisation d'une instance neuve ; affiché une fois dans les logs s'il n'est pas fourni. |
| `LEASSH_UPDATE_CHECK` | `true` | Interroge l'API publique des releases GitHub (une fois par jour, sans donnée d'instance) pour signaler une nouvelle version dans l'interface. Mettre `false` hors ligne. |
| `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` | vides | Valeurs par défaut de la synthèse IA du rapport d'audit (facultatif). |

Tout le reste (URL publique, reverse proxies de confiance, durée de session, rétention des journaux…) se règle dans l'interface, **Réglages → Général**.

> **Conservez `LEASSH_MASTER_KEY` hors de la sauvegarde de la base.** Sans elle, les clés privées SSH et les secrets sont irrécupérables.

## 3. Docker

```bash
export LEASSH_MASTER_KEY="$(openssl rand -base64 32)"
echo "$LEASSH_MASTER_KEY"   # notez-la dans votre gestionnaire de mots de passe

docker run -d --name leassh --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v leassh-data:/data \
  -e LEASSH_MASTER_KEY \
  ghcr.io/sebdew/leassh:latest
```

L'image est basée sur distroless, tourne en utilisateur non-root (65532) et expose le port 8080. Le port n'est publié que sur `127.0.0.1` : l'accès public passe par le reverse proxy.

### Docker Compose

Créez un fichier `.env` à côté (il ne doit **jamais** être commité) :

```
LEASSH_MASTER_KEY=<sortie de openssl rand -base64 32>
LEASSH_SETUP_TOKEN=<sortie de openssl rand -base64 24>
```

```yaml
services:
  leassh:
    image: ghcr.io/sebdew/leassh:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - leassh-data:/data
    environment:
      LEASSH_MASTER_KEY: ${LEASSH_MASTER_KEY:?définissez LEASSH_MASTER_KEY dans .env}
      LEASSH_SETUP_TOKEN: ${LEASSH_SETUP_TOKEN:-}
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
volumes:
  leassh-data:
```

```bash
docker compose up -d
docker compose logs leassh   # jeton d'initialisation si vous ne l'avez pas fixé
```

## 4. Binaire et systemd

```bash
# aucune release publiée : construire le binaire (make build), puis l'installer
sudo install -m 0755 bin/leassh /usr/local/bin/leassh
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
journalctl -u leassh | head      # jeton d'initialisation
```

Construire soi-même : `make build` (Go ≥ 1.26, Node ≥ 24) produit `bin/leassh`, qui embarque l'interface.

## 5. Reverse proxy

Leassh doit être servi en **HTTPS**. Deux points importants : désactiver la mise en mémoire tampon pour le flux en direct du journal (SSE) et transmettre `X-Forwarded-Proto`.

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

Dites ensuite à Leassh quel proxy croire pour l'adresse IP réelle des clients, dans **Réglages → Général → Reverse proxies de confiance** (ex. `127.0.0.1`, ou le réseau Docker du proxy). Sans cela, les en-têtes `X-Forwarded-*` sont ignorés. Renseignez aussi l'**URL publique** (ex. `https://leassh.example.com`) : elle sert aux liens des notifications et à l'URL de retour du SSO.

## 6. Premier démarrage

Ouvrez l'URL : l'écran d'initialisation demande le **jeton d'initialisation**, un nom d'utilisateur et un mot de passe (10 caractères minimum). Voir [Premiers pas](premiers-pas.md).

## 7. Mise à jour et suppression

Voir [Exploitation](../exploitation.md) : sauvegardez toujours avant de mettre à jour.

## 8. Préparer les serveurs cibles

- Créez un utilisateur dédié plutôt que `root`, avec uniquement les droits nécessaires aux commandes du catalogue (par ex. un `sudoers` limité).
- Les commandes passent par un canal SSH `exec` : jamais de shell interactif, de PTY ni de redirection de ports. Les packs fournis n'utilisent pas `sudo` ; adaptez-les à votre environnement. Les identifiants MySQL/PostgreSQL sont lus sur le serveur cible, jamais transmis par Leassh.
- Pour contraindre davantage, préfixez la clé publique dans `authorized_keys` : `restrict,from="<IP de Leassh>" ssh-ed25519 AAAA…`.
- **Derrière un bastion (Warpgate…)** : plusieurs serveurs partagent le même hôte et port, distingués par l'utilisateur SSH. Renseignez le champ *Utilisateur SSH* du serveur (ex. `ia:nom exact de la cible` pour Warpgate). Warpgate bloque l'IP après plusieurs échecs : Leassh suspend donc 60 s les tentatives sur un serveur dont l'authentification vient d'échouer (`ssh_cooldown`).
- Si l'empreinte d'un serveur change (réinstallation), Leassh **bloque** le serveur : vérifiez l'empreinte avant d'« Accepter la nouvelle clé ».

---

[← Précédent : Présentation et concepts](presentation.md) · [Accueil](README.md) · [Suivant : Premiers pas →](premiers-pas.md)
