# Tâches — SSO OpenID Connect

- [x] Table `oidc_config` et réglages administrateur (`GET|PUT /settings/oidc`)
- [x] Flux `start` / `callback` avec PKCE, `state`, `nonce`
- [x] Vérification de l'ID token et mapping des rôles
- [x] Liaison par `(issuer, sub)`, création à la première connexion
- [x] Déconnexion fournisseur (RP-Initiated Logout)
- [x] Bouton de connexion, redirection automatique, `/?login` de secours
- [x] SSO obligatoire et commande `enable-local-login` (ticket #1)
- [x] Remplacement du code maison par `go-oidc` et `x/oauth2` (ticket #2)
- [x] Audit : `oidc.update`, `oidc.test`, `user.sso_login`, `user.sso_login_failed`
- [x] Guide Keycloak pas à pas (wiki)
- [ ] Plusieurs fournisseurs, SCIM (hors périmètre actuel)
