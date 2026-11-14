**Français** · [English](../en/security.md)

# Modèle de sécurité

Cette page décrit ce que Leassh garantit, comment, et ce qu'il **ne** garantit **pas**.

## Invariants (jamais enfreints par le code)

1. **Un seul chemin d'exécution** : aucune exécution SSH ne contourne `executions.Run()`, qui applique dans l'ordre : droits → validation des paramètres → rendu → vérification de l'empreinte → décision de validation → exécution → journal.
2. **Aucun type de paramètre « texte libre »** : toute valeur est validée selon son type puis échappée en POSIX avant insertion.
3. **Canal `exec` uniquement** : jamais de PTY, de shell, d'agent forwarding, de redirection de ports ni de X11.
4. **Clé d'hôte strictement vérifiée** ; une divergence bloque le serveur.
5. **Une clé MCP ne peut rien modifier** : ni configuration, ni validation, ni lecture des exécutions d'une autre clé.
6. **Toute demande `run_command` est journalisée**, refus compris.
7. **Les secrets ne sont jamais journalisés ni renvoyés** par l'API (clé privée, mots de passe, jetons, secrets webhook et SSO).
8. Les messages d'erreur MCP **ne révèlent pas l'existence** de serveurs ou de commandes non visibles pour la clé.
9. Jetons MCP stockés **uniquement sous forme de hash**, comparés à temps constant.
10. **Aucune exécution de commande locale** (`os/exec`) dans le backend.

## Défenses par couche

| Sujet | Mesure |
|---|---|
| Injection de commande | Liste blanche, paramètres typés, échappement POSIX, linter, corpus de plus de 300 combinaisons type × charge exécutées dans un vrai `sh -c` |
| Secrets au repos | AES-256-GCM avec `LEASSH_MASTER_KEY` (hors base) pour clés privées, secrets webhook, SSO, syslog, IA et connecteurs ; rotation par `leassh rotate-master-key` |
| Mots de passe | argon2id |
| Jetons MCP | stockés en hash, préfixe seul affiché |
| Sessions | cookie `HttpOnly`, `Secure`, `SameSite=Strict` ; jeton CSRF `X-CSRF-Token` sur les mutations ; expiration après 30 minutes d'inactivité |
| Connexion | 5 échecs par IP + compte en 15 minutes, plafond de 50 par IP, compteurs en base |
| Initialisation | `/setup` protégé par un jeton |
| En-têtes | CSP stricte (`style-src 'self'`, sans `unsafe-inline`), `Strict-Transport-Security` (laissé au proxy de confiance s'il est déclaré) |
| Observabilité | `/metrics` fermé tant qu'aucun jeton n'est défini |
| Débit MCP | 60 appels d'outils par minute et par clé |
| SSH | Clé d'hôte épinglée, pause de 60 s après un échec d'authentification |
| Journal | Chaîne de hash (voir ci-dessous), non modifiable depuis l'interface |
| SSO | Authorization Code + PKCE S256, `state`/`nonce`, ID token vérifié, compte lié à `(issuer, sub)` |
| Chaîne d'approvisionnement | CI : gitleaks, Trivy, govulncheck, `npm audit` ; images figées par condensat ; Go 1.26.11 |

## Chaîne d'intégrité du journal

Chaque ligne des journaux d'exécutions et d'administration stocke le hash de la précédente. **Vérifier l'intégrité** (Réglages → Général) détecte un contenu altéré, une ligne supprimée au milieu ou une rupture de chaînage. **Limite** : la troncature de la fin de chaîne n'est détectée que si la **tête de chaîne** a été consignée ailleurs (elle figure dans le rapport d'audit) ; un attaquant qui contrôle la base **et** la clé maître pourrait tout recalculer. Pour une preuve plus forte, envoyez les journaux à un SIEM (syslog).

## Limites connues, à connaître

- **Sortie de commande non fiable** : la sortie est renvoyée telle quelle à l'agent. Si elle contient du texte contrôlé par un tiers (journaux d'une application publique, pages web), elle peut porter une **injection de prompt**. L'effet reste borné au catalogue, aux droits de la clé et à la validation humaine des écritures. Ne rendez pas une telle commande `read` accessible sans réflexion.
- **URL sortantes** (webhook, IA, connecteurs, issuer OIDC) : `http(s)` seulement, adresses privées acceptées (usage interne), redirections non suivies pour le webhook et l'IA. Réservé aux administrateurs : ne donnez ce rôle qu'à des personnes de confiance.
- **Mono-instance** : une instance, pas de haute disponibilité.
- Un compte SSO retiré du fournisseur ne peut plus se connecter mais **garde sa session jusqu'à expiration** : désactivez-le dans Leassh pour un effet immédiat.
- Les packs réseau et virtualisation n'ont pas été exécutés sur du matériel réel.

## Recommandations de déploiement

1. Derrière un **reverse proxy TLS**, port 8080 non exposé (`127.0.0.1`).
2. Déclarez vos **reverse proxies de confiance** et l'**URL publique**.
3. **SSO** puis désactivation du mot de passe local.
4. Un **utilisateur SSH dédié** par cible, droits minimaux, `restrict,from=` dans `authorized_keys`.
5. Une clé MCP **par agent**, avec **expiration** ; profil `RO` par défaut.
6. **Sauvegardez** base et clé maître **séparément**, testez la restauration ([Exploitation](../exploitation.md)).
7. Envoyez les journaux à un **SIEM**, et alertez sur `host_key.mismatch`.

## Signaler une vulnérabilité

Ne publiez pas de vulnérabilité dans un ticket public : contactez directement le mainteneur (dépôt d'origine indiqué dans le fichier `NOTICE`).

---

[← Précédent : Agents et outils MCP](agents-mcp.md) · [Accueil](README.md) · [Suivant : Connexion unique (SSO, OpenID Connect) →](sso.md)
