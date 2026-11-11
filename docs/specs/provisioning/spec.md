# Spécification — Provisioning des serveurs depuis un bastion

Statut : livrée (écrite a posteriori). Détail normatif : `docs/specs/SPEC.md`, annexe « Règles multi-groupes et auto-provisioning ».

## Quoi et pourquoi

Quand les serveurs sont déjà déclarés dans un bastion SSH (Warpgate), les ressaisir dans Leassh est fastidieux et se désynchronise. Un **connecteur** lit la liste des cibles et crée, met à jour et désactive les serveurs et groupes de serveurs correspondants.

## Scénarios

1. L'administrateur ajoute un connecteur (URL, jeton d'API, hôte et port du bastion, identité), le teste, épingle la clé d'hôte du bastion.
2. Il prévisualise la synchronisation, puis l'applique ; elle se répète ensuite à intervalle régulier.
3. Une cible retirée du bastion est **désactivée**, jamais supprimée (le journal y fait référence).

## Exigences

- Un connecteur ne fait que **lire** la source ; le moteur écrit. Aucun sous-processus ni chargement dynamique (invariant 10).
- Propriété : les objets synchronisés sont verrouillés (nom, hôte, port) ; l'identité, l'utilisateur SSH, les étiquettes et l'état restent modifiables.
- Garde-fou : une synchronisation qui désactiverait au moins 3 machines et plus de 30 % du parc (ou tout le parc si la source est vide) est refusée sans `force`.
- La clé d'hôte du bastion est épinglée une fois ; une divergence n'est jamais effacée par une synchronisation.
- Le jeton d'API est chiffré (clé maître) et jamais renvoyé.
- Chaque synchronisation qui change quelque chose écrit un événement d'audit `provisioner.sync`.

## Hors périmètre

Autres sources que Warpgate (le registre de types est prévu pour en ajouter), suppression automatique de serveurs.

## Critères d'acceptation

- L'aperçu et l'application produisent le même calcul (l'aperçu est annulé).
- Une divergence de clé d'hôte survit à une synchronisation.
- Une source vide ne désactive pas le parc sans confirmation.
