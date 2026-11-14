# ADR 0006 — Adresses privées acceptées pour les URL sortantes

- Statut : accepté (2026-11-11)
- Ticket : sebastien/Leassh#49 (constat DOC-10 de la revue du 2026-11-11, décision SEC-08)

## Contexte

Leassh est de niveau **N3** (exposé ou destiné à l'entreprise) : la documentation recommande un reverse proxy TLS pour que des agents distants joignent `/mcp`. La note `docs/securite-sorties-et-urls.md` justifiait pourtant l'acceptation des adresses privées par « N2, jamais exposé à Internet ». Cette justification était fausse ; la décision doit être reprise sur de bonnes bases.

Le webhook, la synthèse IA, le connecteur de bastion et les serveurs SSH sont configurés par un administrateur et peuvent viser une adresse privée, de boucle locale ou de lien local (risque SSRF, OWASP A10).

## Décision

On **conserve** l'acceptation des adresses privées, sans option de blocage, pour ces raisons :

1. **C'est le cas d'usage.** Les cibles légitimes sont internes : serveurs SSH, bastion, Mattermost, Ollama ou LiteLLM. Un blocage par défaut rendrait l'outil inutilisable ; une liste d'exceptions à maintenir déplacerait le risque sans le réduire.
2. **Seul un administrateur configure ces URL.** Ce rôle est réservé à des personnes de confiance et passe par le SSO (ADR 0001). Un administrateur déclare déjà un serveur SSH sur n'importe quelle adresse et lit l'erreur de connexion : il sonde déjà le réseau.
3. **Les fonctions sont aveugles.** Webhook et IA ne renvoient que le code HTTP ou une erreur réseau, jamais le corps de la réponse. Aucune ne suit de redirection. Le test OIDC ne relaie plus le corps distant (ticket #35).
4. **L'exposition à Internet ne change pas cette surface.** Elle ne s'ouvre pas à un visiteur anonyme : elle suppose un compte administrateur.

## Conséquences

- Le risque résiduel est celui d'un **compte administrateur compromis** : il peut faire émettre des requêtes depuis le réseau où tourne Leassh. Les mesures qui le bornent sont le SSO (avec double authentification côté fournisseur d'identité), le journal d'administration à chaîne d'intégrité et l'absence de relais du corps des réponses.
- Pour un déploiement où cela est inacceptable (service exposé, cloud avec point de métadonnées), isoler Leassh au niveau réseau : règles de sortie du pare-feu ou du conteneur limitées aux cibles attendues, et blocage de `169.254.169.254`. À documenter dans `docs/exploitation.md`.
- **Réexamen** : si un rôle non administrateur peut un jour saisir une URL sortante, ou si Leassh est proposé en service multi-organisations, ajouter une option de blocage des plages privées (désactivée par défaut) avant de l'autoriser.
