# Plan technique — SSO OpenID Connect

## Composants

- `internal/oidc` : découverte, vérification des ID tokens, PKCE, appel userinfo.
- `internal/api` : routes `/api/v1/auth/oidc/start|callback`, `/api/v1/auth/providers`, `/api/v1/settings/oidc[/test]`.
- Table `oidc_config` (migration 00010, 00012, 00015) ; secret client chiffré (AES-256-GCM, clé maître).
- Interface : `web/src/pages/OidcPanel.tsx`, bouton sur la page de connexion.

## Réutilisation (constitution, principe 2)

| Besoin | Choix | Écarté |
|---|---|---|
| Découverte, JWKS, vérification des ID tokens | `github.com/coreos/go-oidc/v3` | Analyse de JWT maison (retirée par le ticket #2) |
| Échange de code, PKCE | `golang.org/x/oauth2` | Client HTTP maison |
| Scellement du cookie d'état | AES-256-GCM déjà présent (`internal/crypto`) | Bibliothèque de sessions |

## Sécurité

CSRF de connexion : `state` comparé en temps constant, cookie scellé à usage unique, `SameSite=Lax` (retour inter-sites). Limiteur d'échecs partagé avec le mot de passe. URL sortantes : voir `docs/securite-sorties-et-urls.md`. Le corps des réponses distantes n'est jamais relayé dans les erreurs (ticket #35).
