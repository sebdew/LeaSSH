# Spécification — Connexion unique (SSO) OpenID Connect

Statut : livrée (écrite a posteriori). Détail normatif : `docs/specs/SPEC.md` §9bis. Décision : [ADR 0001](../../adr/0001-authentification-sso-et-compte-local.md).

## Quoi et pourquoi

Permettre aux administrateurs, validateurs et lecteurs de se connecter à l'**interface** avec leur fournisseur d'identité d'entreprise (Keycloak, Entra ID, Authentik, Google…), plutôt que par des mots de passe propres à Leassh : un seul endroit pour créer et retirer des accès, et des rôles issus des groupes de l'annuaire.

## Utilisateurs et scénarios

1. **Administrateur** : configure le fournisseur (Réglages → SSO), teste la découverte, enregistre, active.
2. **Utilisateur** : clique « Se connecter avec <fournisseur> » (ou est redirigé automatiquement) et arrive connecté avec le rôle de ses groupes.
3. **Exploitant** : en cas de panne du fournisseur, réactive la connexion locale en ligne de commande (`leassh enable-local-login`).

## Exigences

- Authorization Code + PKCE S256, `state` et `nonce` ; ID token vérifié (signature, `iss`, `aud`, `exp`, `nonce`).
- Rôle recalculé à chaque connexion à partir d'une revendication configurable ; le plus élevé gagne ; refus ou rôle `viewer` sans correspondance.
- Compte lié à `(issuer, sub)`, jamais à l'e-mail ; création à la première connexion (ou refus).
- Compte local : conservé pour la première connexion et le secours ; désactivable une fois un administrateur SSO connecté.
- Les clés MCP ne passent jamais par le SSO.
- Chaque connexion et chaque échec sont audités, sans jeton.

## Hors périmètre

Plusieurs fournisseurs, SCIM, déprovisionnement automatique, back-channel logout.

## Critères d'acceptation

- Une connexion réussie ouvre la session existante (cookies habituels) avec le bon rôle.
- Un jeton falsifié, expiré, d'un autre émetteur ou d'une autre audience est refusé (tests `internal/oidc`).
- Impossible d'activer « SSO obligatoire » sans administrateur SSO déjà connecté.
