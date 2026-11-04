# Leassh

> **Donnez à vos agents IA un accès SSH… en laisse.**
> Un catalogue de commandes validées, des droits clairs, et un journal de tout ce qui a été lancé, par qui et où.

Leassh est un serveur **MCP** (Model Context Protocol) open source qui permet à des agents IA d'agir sur vos serveurs **sans jamais leur donner de shell**. L'agent ne tape pas de commandes : il **choisit** dans un catalogue que vous avez écrit ou approuvé, avec des paramètres typés et validés. Chaque demande — autorisée, refusée ou en attente de validation — est journalisée.

> ⚠️ *Leassh* (interface : *LeaSSH*) est un nom de travail. Les jalons M1 à M8 sont implémentés, suivis du provisioning, de l'export syslog, du SSO et de l'interface bilingue ; voir [`CLAUDE.md`](CLAUDE.md) pour les consignes de développement.

---

## Pourquoi Leassh ?

Donner un accès SSH à un agent IA, c'est lui donner un shell. Filtrer les commandes après coup (listes noires, regex sur la ligne de commande) est contournable : `rm` bloqué, mais `find -delete`, `python -c`, `truncate` ou `$( )` passent.

Leassh inverse la logique :

| Approche classique | Leassh |
|---|---|
| L'agent écrit une commande, on essaie de la filtrer | L'agent choisit une commande dans un catalogue |
| Liste noire (tout ce qui n'est pas interdit passe) | Liste blanche (rien ne passe s'il n'est pas prévu) |
| Paramètres libres | Paramètres typés, validés puis échappés |
| Logs SSH bruts | Journal métier : agent, serveur, commande, paramètres, raison, résultat, validateur |

## Fonctionnalités

- 🗂️ **Catalogue de commandes** avec templates, paramètres typés (`enum`, `int`, `service`, `path`, `identifier`…), niveau de risque (`read` / `write` / `destructive`) et description destinée à l'IA.
- 🖥️ **Serveurs et identités SSH** : génération de clés depuis l'interface, épinglage de la clé d'hôte.
- 👥 **Groupes** de serveurs et de commandes.
- 🔑 **Clés MCP** par agent (N1, N2, assistant perso…), jamais stockées en clair.
- 🛡️ **Règles d'accès** : un groupe de serveurs + un groupe de commandes + des clés + un profil (`RO`, `RW`).
- ✋ **Validation humaine** des commandes sensibles, depuis l'interface (y compris sur mobile).
- 💡 **Propositions** : l'agent peut proposer une nouvelle commande ; un linter rejette les templates dangereux, vous validez.
- 📜 **Journal complet** : qui a lancé quoi, où, quand, pourquoi, avec quel résultat — y compris les tentatives refusées.
- 📦 **Packs de commandes** en YAML (système, MySQL, PostgreSQL, nginx, Docker…), importables et partageables.
- 🔐 **SSO OpenID Connect** (Keycloak, Entra ID, Authentik, Google…) : Authorization Code + PKCE, rôles issus des groupes, comptes locaux conservés en secours. Guide pas à pas : [`docs/INSTALL.md`](docs/INSTALL.md) §12.
- 🔄 **Provisioning** : synchronisation automatique des serveurs depuis un bastion (Warpgate).
- 📡 **Export syslog** (UDP/TCP/TLS, RFC 5424, formats texte/JSON/CEF) et **webhook** signé HMAC.
- 🧾 **Rapport d'audit** (ISO 27001, HDS) généré à partir de la base, avec synthèse IA facultative.
- 🌍 **Interface en français et en anglais**, claire et sombre, simple et moderne.
- 📦 **Un seul binaire** (ou une image Docker), SQLite par défaut.

## Démarrage rapide

```bash
docker run -d --name leassh \
  -p 8080:8080 \
  -v leassh-data:/data \
  -e LEASSH_MASTER_KEY="$(openssl rand -base64 32)" \
  ghcr.io/<org>/leassh:latest
```

Ouvrez `http://localhost:8080`, créez le compte administrateur, puis suivez la checklist d'accueil :

1. Créer une **identité SSH** (ou générer une paire de clés et copier la clé publique sur vos serveurs).
2. Ajouter un **serveur** et valider son empreinte de clé d'hôte.
3. Importer un **pack** de commandes (ex. `system`).
4. Créer une **règle d'accès** (groupe de serveurs + groupe de commandes + profil).
5. Créer une **clé MCP** et l'associer à la règle.
6. Copier la configuration MCP dans votre agent.

> **Conservez `LEASSH_MASTER_KEY`** : elle chiffre les secrets SSH. Sans elle, ils sont irrécupérables.
>
> Installation complète (binaire, systemd, reverse proxy TLS, sauvegarde, métriques) : [`docs/INSTALL.md`](docs/INSTALL.md).

## Connecter un agent

Leassh expose un endpoint MCP en HTTP streamable : `https://leassh.example.com/mcp`, authentifié par `Authorization: Bearer <clé MCP>`.

```json
{
  "mcpServers": {
    "leassh": {
      "type": "http",
      "url": "https://leassh.example.com/mcp",
      "headers": { "Authorization": "Bearer lsh_xxxxxxxxxxxxxxxx" }
    }
  }
}
```

### Outils exposés

| Outil | Rôle |
|---|---|
| `list_servers` | Serveurs visibles pour cette clé |
| `list_commands` | Commandes autorisées sur un serveur, avec leurs paramètres |
| `run_command` | Exécute une commande du catalogue (ou la met en attente de validation) |
| `get_execution` | Statut et résultat d'une exécution |
| `propose_command` | Propose une nouvelle commande au catalogue |

## Concepts

```
 Identité SSH ──┐
                ▼
            Serveur ──► Groupe de serveurs ──┐
                                             ├──► Règle d'accès (profil RO/RW) ◄── Clés MCP
            Commande ─► Groupe de commandes ─┘
```

- **Profil `RO`** : commandes `read` uniquement.
- **Profil `RW`** : `read` + `write` (validation requise par défaut pour `write`).
- **`destructive`** : validation humaine **toujours** requise, quel que soit le profil.

## Exemple de commande

```yaml
- id: systemd.restart
  name: Redémarrer un service
  description: >
    Redémarre un service systemd. Provoque une coupure de quelques secondes.
    À utiliser après avoir vérifié l'état du service avec systemd.status.
  risk: write
  template: systemctl restart {{service}}
  params:
    service: { type: service, description: "Nom de l'unité systemd" }
```

## Sécurité

- Pas de PTY, pas de shell interactif : uniquement des canaux `exec`.
- Aucun paramètre en texte libre ; chaque valeur est validée puis échappée.
- Les clés MCP ne peuvent ni modifier la configuration, ni approuver quoi que ce soit.
- Secrets chiffrés au repos (AES-256-GCM), jamais journalisés.
- Clés d'hôte épinglées ; toute divergence bloque le serveur.

Voir [`docs/SPEC.md`](docs/SPEC.md) pour le détail.

## Développement

Go ≥ 1.23, Node ≥ 20.

```bash
make dev     # backend avec rechargement + Vite (proxy API)
make test    # tests Go (dont corpus d'injection) + tests Vitest
make lint    # go vet, gofmt, tsc --noEmit
make build   # build du frontend puis binaire unique (bin/leassh)
make docker  # image Docker
```

## Documentation

- [`docs/SPEC.md`](docs/SPEC.md) — cahier des charges fonctionnel et technique
- [`docs/INSTALL.md`](docs/INSTALL.md) — installation, exploitation, sauvegarde
- [`docs/UX.md`](docs/UX.md) — guide UX/UI et design system
- [`CLAUDE.md`](CLAUDE.md) — consignes de développement
- [`packs/`](packs/) — packs de commandes fournis

## Licence

[Apache License 2.0](LICENSE). Vous pouvez reprendre, modifier et redistribuer ce projet, y compris dans un cadre commercial, à condition de **conserver la licence et le fichier [`NOTICE`](NOTICE)** et de **citer le créateur** (Sébastien Dewarlez) ainsi que les modifications apportées (§4).
