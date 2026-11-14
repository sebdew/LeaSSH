**Français** · [English](../en/troubleshooting.md)

# Dépannage et FAQ

| Symptôme | Piste |
|---|---|
| Démarrage refusé : `LEASSH_MASTER_KEY est obligatoire` | Variable absente, pas du base64, ou pas 32 octets : `openssl rand -base64 32`. |
| Connexion impossible derrière un proxy | Le proxy doit servir en HTTPS et transmettre `X-Forwarded-Proto: https`, ou renseignez l'URL publique `https://…` dans Réglages → Général (cookies `Secure`). |
| « Trop de tentatives » à la connexion | 5 échecs par IP + compte en 15 minutes. Vérifiez les **reverse proxies de confiance** si tous les clients semblent avoir la même IP. |
| Le journal en direct ne se met pas à jour | Le proxy met en mémoire tampon : désactivez-la pour `/api/v1/executions/stream` ([Installation](installation.md)). |
| Bouton SSO en erreur, ou compte SSO refusé | Voir [SSO](sso.md) (« Problèmes courants ») ; le motif exact est dans le journal d'administration (`user.sso_login_failed`). Le compte local reste un secours (`?login`). |
| Serveur « clé d'hôte divergente » | Réinstallation ou attaque : vérifiez l'empreinte, puis *Accepter la nouvelle clé* ([Serveurs](serveurs.md)). |
| Serveur « inconnue » | Testez la connexion et épinglez la clé d'hôte. |
| Un agent reçoit « serveur ou commande indisponible pour cette clé » | Message **volontairement générique**. Utilisez le [Simulateur](simulateur.md) et le [Journal](journal.md) : la raison exacte du refus y figure. |
| `rate_limited` (HTTP 429) | Plus de 60 appels d'outils par minute avec cette clé : attendre `Retry-After`. |
| Une exécution reste `pending_approval` | Elle attend une décision ([Validations](validations.md)) ; elle expire après le délai réglé (30 min par défaut). |
| `ssh_cooldown` dans le journal | Pause de 60 s après un échec d'authentification, pour ne pas déclencher un blocage d'IP par un bastion. Toute modification du serveur la lève. |
| Warpgate : « SSH auth from blocked IP » | Débloquez l'IP de Leassh dans Warpgate. |
| Une commande approuvée n'est lançable par personne | Ajoutez-la à un groupe de commandes présent dans une règle ([Règles](regles-d-acces.md)). |
| Mot de passe administrateur perdu | `leassh reset-password <identifiant>` ([Exploitation](../exploitation.md)). |
| `/metrics` répond 401 | Normal tant qu'aucun jeton n'est défini (Réglages → Général) ; présentez-le en `Authorization: Bearer`. |
| Après restauration, les serveurs ne se connectent plus | La `LEASSH_MASTER_KEY` n'est pas celle de la sauvegarde : les clés privées ne se déchiffrent pas. |

## FAQ

**Un agent peut-il obtenir un shell ?** Non : canal `exec` seulement, jamais de PTY ni de shell interactif, et seulement des commandes du catalogue.

**Un agent peut-il valider sa propre demande ?** Non : une clé MCP ne peut rien modifier ni valider ; seul un humain `admin` ou `approver` décide depuis l'interface.

**Peut-on écrire une commande libre ?** Non : il n'existe aucun type de paramètre « texte libre ». Un administrateur écrit le modèle ; l'agent ne fournit que des valeurs typées.

**Que se passe-t-il si la base est copiée par un tiers ?** Les secrets sont chiffrés avec la clé maître, qui n'est pas dans la base ; les jetons MCP et les mots de passe sont des hash. Les journaux, eux, sont lisibles.

**Peut-on utiliser PostgreSQL ?** Pas aujourd'hui : SQLite, une instance ([ADR 0003](../adr/0003-choix-de-la-stack-go-sqlite.md)).

---

[← Précédent : Référence de l'API](api.md) · [Accueil](README.md) · [Suivant : Structure du dépôt et décisions →](structure-du-depot.md)
