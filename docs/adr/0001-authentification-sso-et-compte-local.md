# ADR 0001 — Authentification : SSO obligatoire, compte local de première connexion

- Statut : accepté (2026-11-07)
- Ticket : sebastien/Leassh#1 (constat ARC-01 de la revue du 2026-11-07)

## Contexte

La constitution (principe 1) impose OpenID Connect via Keycloak, sans stockage local de mots de passe ni écran de login maison. Leassh garde des comptes locaux (argon2id) : le premier compte est créé à l'initialisation, avant qu'un fournisseur d'identité existe.

## Décision

1. **Le SSO OIDC est le mode de connexion cible.** Les droits viennent des groupes du fournisseur (mapping de rôles).
2. **Le compte local est toléré uniquement pour la première connexion** : il permet de créer l'administrateur initial et de configurer le SSO.
3. **Une fois le SSO validé, le mot de passe local se désactive par réglage** (Réglages → SSO → « Désactiver le mot de passe local »). Le réglage vaut pour tous les comptes locaux, y compris l'administrateur.
4. **Garde-fous contre le verrouillage** :
   - activation refusée si le SSO n'est pas actif ;
   - activation refusée sans au moins un administrateur SSO actif ayant déjà ouvert une session (preuve que le SSO fonctionne) ;
   - le SSO ne peut pas être désactivé tant que le réglage est posé (il faut d'abord réactiver le mot de passe local).
5. **Secours si le SSO tombe** : la commande `leassh enable-local-login`, exécutée sur la machine qui héberge les données, réactive le mot de passe local. Elle est inscrite au journal d'administration (`oidc.local_login_enabled_cli`).

## Conséquences

- Pour un déploiement exposé ou d'entreprise (N3), désactiver le mot de passe local dès que le SSO fonctionne.
- Par défaut (réglage non posé), l'instance reste utilisable sans fournisseur d'identité : c'est la limite assumée pour un usage interne ou une première installation.
- L'écran de connexion et l'écran d'initialisation restent dans le code : ils servent tant que le SSO n'est pas configuré. Aucun mot de passe local n'est créé pour un compte SSO.
- Les refus de connexion locale sont journalisés (`user.login_failed`, motif `local_login_disabled`). Ils comptent dans le plafond par IP (50 échecs par 15 minutes), le nom saisi est refusé au-delà de 64 caractères et tronqué à 128 caractères en audit (SEC-09, #47).
- Écart assumé (SEC-10) : le verrou par compte a pour clé `ip|identifiant` (5 échecs). Une attaque répartie sur plusieurs adresses n'est limitée que par le plafond de chaque IP. Pas de compteur par compte seul, pour ne pas permettre à un tiers de verrouiller un administrateur depuis Internet ; la parade est de désactiver le mot de passe local dès que le SSO fonctionne (décision 3). L'affichage des comptes bloqués relève de #28.
- Reste hors périmètre : le remplacement du code JWT/OIDC maison par `coreos/go-oidc` (ticket #2).
