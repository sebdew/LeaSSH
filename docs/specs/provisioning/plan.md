# Plan technique — Provisioning

## Composants

- `internal/provisioning` : registre de connecteurs compilés (`Discover` en lecture seule), moteur de synchronisation, planificateur.
- Tables `provisioners`, `provisioner_rules` (migration 00006) ; colonnes `managed_by` et `external_id` sur serveurs et groupes ; `ssh_user_custom` (00007).
- API : `/api/v1/provisioners` (CRUD, test, aperçu, synchronisation, épinglage de clé d'hôte).
- Interface : écran **Provisioning** (`web/src/pages/ProvisioningPage.tsx`).

## Réutilisation (principe 2)

Client HTTP de la bibliothèque standard pour l'API Warpgate (`GET /@warpgate/api/targets`, jeton dans `X-Warpgate-Token`). Chiffrement du jeton par `internal/crypto` existant. Aucune nouvelle dépendance.

## Choix et alternatives écartées

- Connecteurs compilés plutôt que greffons : un greffon exécuterait du code tiers dans le processus (surface d'attaque, invariant 10).
- Désactivation plutôt que suppression : conserve les références du journal.
- Aperçu = exécution réelle puis annulation de la transaction : garantit que l'aperçu dit vrai.
