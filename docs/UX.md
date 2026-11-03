# Leash — Guide UX / UI

## 1. Intention

Leash manipule un sujet anxiogène (des IA qui agissent sur des serveurs). L'interface doit **rassurer par la clarté** :
- on comprend en un coup d'œil ce que chaque agent peut faire ;
- on voit immédiatement ce qui s'est passé, et ce qui attend une décision ;
- rien n'est caché derrière trois clics.

**Inspirations** :
- **Warpgate** pour la sobriété : peu d'écrans, des listes nettes, des formulaires courts, pas de fioritures.
- **Securo** pour la modernité : cartes aérées, typographie soignée, couleurs douces, micro-interactions discrètes.

Résultat visé : un outil d'administration **calme, dense quand il le faut (journal), aéré ailleurs**.

## 2. Principes

1. **Le journal est le cœur** : c'est l'écran le plus consulté, il doit être rapide, filtrable et lisible.
2. **Ce qui attend une décision se voit partout** : badge dans la barre latérale, bandeau sur le tableau de bord, notification navigateur optionnelle.
3. **Le risque a une couleur**, toujours la même : `read` vert, `write` ambre, `destructive` rouge. Jamais de couleur seule : toujours icône + libellé.
4. **Montrer la commande réelle** : partout où une exécution apparaît, la commande rendue est visible en police monospace.
5. **Expliquer les droits** : le simulateur d'accès et les infobulles « pourquoi ? » évitent de deviner.
6. **Actions dangereuses = confirmation explicite** (révoquer une clé, supprimer un serveur, approuver un `destructive` : saisir le slug du serveur).
7. **Aucun écran vide sans guide** : chaque état vide explique quoi faire et propose le bouton correspondant.

## 3. Structure

```
┌───────────────┬──────────────────────────────────────────────────────────┐
│  ◆ Leash      │  ⌘K Rechercher…                      ◐ thème   👤 admin ▾ │
│               ├──────────────────────────────────────────────────────────┤
│  Tableau de   │                                                          │
│  bord         │   Titre de page                         [Action primaire] │
│  Journal      │   Sous-titre court                                       │
│  Validations ③│                                                          │
│  Propositions①│   ┌─────────── contenu ───────────────────────────────┐  │
│ ─ Catalogue ─ │   │                                                    │  │
│  Commandes    │   │                                                    │  │
│  Packs        │   └────────────────────────────────────────────────────┘  │
│ ─ Infra ───── │                                                          │
│  Serveurs     │                                                          │
│  Identités SSH│                                                          │
│ ─ Accès ───── │                                                          │
│  Règles       │                                                          │
│  Clés MCP     │                                                          │
│  Simulateur   │                                                          │
│ ───────────── │                                                          │
│  Réglages     │                                                          │
└───────────────┴──────────────────────────────────────────────────────────┘
```

- Barre latérale fixe (240 px), repliable en icônes (64 px), en tiroir sur mobile.
- Barre supérieure : palette de commandes (`Ctrl/⌘+K`) pour aller à n'importe quel serveur, commande, clé ou exécution ; sélecteur de thème (Système / Clair / Sombre) ; menu utilisateur.
- Les groupes (serveurs, commandes) sont des **onglets** dans leurs pages respectives (« Serveurs | Groupes »), pas des entrées de menu supplémentaires.
- Largeur de contenu max 1280 px, sauf le Journal (pleine largeur).

## 4. Design system

### 4.1 Typographie
- Interface : **Inter** (variable), 14 px de base, 13 px dans les tableaux denses.
- Code, commandes, slugs, empreintes : **JetBrains Mono**, 13 px.
- Titres : 24 / 18 / 15 px, graisse 600. Chiffres tabulaires (`font-variant-numeric: tabular-nums`) dans les tableaux.

### 4.2 Couleurs (tokens CSS)

Les composants n'utilisent **que** les tokens. Valeurs indicatives, à ajuster en vérifiant les contrastes AA.

| Token | Clair | Sombre |
|---|---|---|
| `--bg` | `#F7F8FA` | `#0E1116` |
| `--surface` | `#FFFFFF` | `#161A21` |
| `--surface-2` | `#F1F3F6` | `#1D222B` |
| `--border` | `#E3E6EB` | `#2A303A` |
| `--text` | `#14181F` | `#E8EBF0` |
| `--text-muted` | `#5B6472` | `#9AA3B2` |
| `--primary` | `#4F46E5` | `#818CF8` |
| `--primary-fg` | `#FFFFFF` | `#0E1116` |
| `--read` | `#0F9F6E` | `#34D399` |
| `--write` | `#B7791F` | `#FBBF24` |
| `--destructive` | `#DC2626` | `#F87171` |
| `--info` | `#2563EB` | `#60A5FA` |
| `--focus-ring` | `#4F46E5` 40 % | `#818CF8` 50 % |

Chaque couleur sémantique a une variante `-soft` (fond de badge, ~12 % d'opacité).

Thème : suit le système par défaut, choix mémorisé par utilisateur (côté serveur) et en `localStorage` pour éviter le flash au chargement.

### 4.3 Formes et espacement
- Grille de 4 px ; espacements usuels 8 / 12 / 16 / 24 / 32.
- Rayon : 10 px (cartes, modales), 8 px (boutons, champs), 999 px (badges).
- Ombres très légères en clair, remplacées par des bordures en sombre.
- Transitions 150 ms, `prefers-reduced-motion` respecté.

### 4.4 Composants clés
- **Badge de risque** : icône + libellé (`◉ lecture`, `▲ écriture`, `⬣ destructif`).
- **Badge de statut d'exécution** : `réussie`, `échec`, `refusée`, `en attente`, `rejetée`, `expirée`, `délai dépassé`, `en cours` (animation discrète).
- **Bloc commande** : fond `--surface-2`, monospace, bouton copier, placeholders colorés dans l'éditeur.
- **Avatar d'acteur** : agent (icône robot + nom de clé) vs humain (initiales) — partout dans le journal pour distinguer d'un coup d'œil.
- **Tiroir de détail** (drawer à droite, 560 px) : ouverture d'une ligne sans quitter la liste, URL mise à jour pour partage.
- **Puces de filtre** combinables, avec « Effacer ».
- **Toasts** pour les confirmations, jamais pour les erreurs importantes (elles restent affichées dans la page).

## 5. Écrans

### 5.1 Initialisation
Écran centré, une seule carte : nom d'utilisateur, mot de passe (indicateur de robustesse), confirmation. Rappel de sauvegarder `LEASH_MASTER_KEY`.

### 5.2 Tableau de bord
- **Checklist d'accueil** tant qu'elle n'est pas terminée (6 étapes, cases cochées automatiquement, chaque étape est un lien).
- Bandeau **« 3 validations en attente »** (ambre) si applicable, avec bouton « Examiner ».
- Cartes chiffrées : exécutions sur 24 h (réussies / échecs / refusées), agents actifs, serveurs en anomalie (clé d'hôte, injoignables), propositions en attente.
- Liste « Activité récente » (10 dernières exécutions, même format que le journal).

### 5.3 Journal ⭐
Onglets : **Exécutions** | **Administration**.

**Exécutions** — tableau dense, pleine largeur :

| Date | Agent | Serveur | Commande | Risque | Statut | Code | Durée | Validé par |
|---|---|---|---|---|---|---|---|---|
| 18/10 13:42:07 | 🤖 Agent N1 | web-01 | `systemd.status` | ◉ lecture | ✔ réussie | 0 | 0,4 s | — |
| 18/10 13:40:51 | 🤖 Agent N2 | db-02 | `systemd.restart` | ▲ écriture | ✔ réussie | 0 | 2,1 s | SD |
| 18/10 13:38:12 | 🤖 Agent N1 | db-02 | `mysql.kill_query` | ⬣ destructif | ⛔ refusée | — | — | — |

- Barre de filtres au-dessus : période (raccourcis 1 h / 24 h / 7 j / personnalisé), agent, serveur, commande, risque, statut, validateur, recherche texte.
- Les filtres sont dans l'URL (partageables, persistants au rafraîchissement).
- Mise à jour en direct : les nouvelles lignes apparaissent en haut avec un léger surlignage ; bouton « Pause » du direct.
- Ligne refusée : texte légèrement atténué + raison au survol.
- Clic sur une ligne → **tiroir de détail** :
  - en-tête : statut, commande, serveur, agent ;
  - **« Pourquoi »** : la raison fournie par l'agent, mise en avant (citation) ;
  - **commande rendue** exacte (bloc commande) + paramètres (tableau clé / valeur) ;
  - **chronologie** verticale : demandée par Agent N2 → en attente → validée par Sébastien D. (« OK, redémarrage autorisé ») → exécution démarrée → terminée (exit 0, 2,1 s) ;
  - **sortie** : onglets stdout / stderr, monospace, numéros de ligne, recherche, copier, indicateur « tronquée » ;
  - métadonnées repliées : règle appliquée, version et hash de la commande, IP, session MCP ;
  - liens : « Voir toutes les exécutions de cet agent / sur ce serveur / de cette commande ».
- Export CSV / NDJSON du résultat filtré.

**Administration** — même présentation : date, acteur (humain ou clé), action (libellé lisible : « a modifié la commande `systemd.restart` »), cible, IP. Tiroir avec **diff avant / après** coloré.

### 5.4 Validations
- Liste de cartes, la plus ancienne en haut, avec compte à rebours avant expiration.
- Chaque carte : agent, serveur, badge de risque, **raison** en grand, commande rendue, paramètres, « autorisée par la règle … ».
- Actions : **Approuver** (primaire) / **Refuser** (secondaire), commentaire optionnel (obligatoire pour refuser).
- `destructive` : la confirmation demande de saisir le slug du serveur.
- Optimisé mobile : carte pleine largeur, boutons en bas d'écran, utilisable depuis un lien de notification.
- Après décision, la carte se replie avec le résultat (« approuvée par vous — en cours… → réussie »).

### 5.5 Propositions
- Liste : agent, slug proposé, risque proposé, statut (`en attente`, `invalide`, `approuvée`, `refusée`), date.
- Détail en deux colonnes : à gauche la proposition (justification de l'agent, template, paramètres), à droite le **résultat du linter** (erreurs en rouge, avertissements en ambre, chaque problème pointé dans le template).
- Bouton « Modifier puis approuver » qui ouvre l'éditeur de commande prérempli ; choix du niveau de risque définitif et du ou des groupes de commandes.

### 5.6 Commandes
- Liste groupée par préfixe (`mysql.*`, `system.*`…) ou à plat ; colonnes : slug, nom, risque, groupes, statut, source (manuel / pack / proposition), dernière utilisation.
- **Éditeur** (page dédiée) :
  - champs : slug, nom, description (avec compteur et conseil « Décrivez quand l'utiliser et ce qu'elle renvoie — c'est ce que lit l'IA »), risque (segmented control coloré), délai, sortie max ;
  - **template** avec coloration des placeholders et linter en direct sous le champ ;
  - **paramètres** : détectés automatiquement depuis le template, chaque ligne propose type + options ;
  - **aperçu** : saisir des valeurs de test → commande rendue affichée en direct (avec l'échappement réel) ;
  - **tester** : choisir un serveur → exécution réelle (journalisée), sortie affichée sous l'éditeur ;
  - panneau « Où est-elle utilisée ? » : groupes, règles et clés qui y ont accès.

### 5.7 Serveurs
- Liste : nom, slug, hôte:port, identité, groupes, **statut de clé d'hôte** (épinglée ✔ / inconnue ? / divergente ⚠), dernière exécution.
- Ajout : formulaire court (nom, hôte, port, identité, description pour l'IA, tags) → bouton « Tester la connexion » → affichage de l'empreinte → « Faire confiance à cette clé ».
- Divergence de clé d'hôte : bandeau rouge sur la fiche, ancienne vs nouvelle empreinte, action explicite « Accepter la nouvelle clé » (journalisée).
- Fiche serveur : infos, commandes accessibles par agent (lien vers le simulateur), dernières exécutions.

### 5.8 Identités SSH
- Création : « Générer une clé ed25519 » (par défaut) ou « Importer une clé » ou « Mot de passe ».
- Après génération : clé publique affichée avec bouton copier et la ligne prête à coller (`echo '…' >> ~/.ssh/authorized_keys`).
- La clé privée n'est jamais réaffichée.

### 5.9 Règles d'accès
- Chaque règle est une carte lisible comme une phrase :
  > **Agent N1**, **Agent N2** peuvent lancer les commandes **Diag MySQL** sur les serveurs **BDD prod** en **lecture seule**.
- Édition dans une modale avec les quatre éléments (clés, groupe de serveurs, groupe de commandes, profil) et l'option « Les commandes d'écriture nécessitent une validation ».
- Compteurs : n serveurs × m commandes.

### 5.10 Clés MCP
- Liste : nom, préfixe (`lsh_a1b2…`), règles associées, dernière utilisation (relative), expiration, statut.
- Création : nom, description, expiration (jamais / 30 j / 90 j / date), autorisation de proposer, règles.
- **Modale de révélation** du jeton : affiché une seule fois, bouton copier, avertissement clair, et onglets d'exemples de configuration (Claude Code, LiteLLM, JSON générique) avec le jeton déjà inséré.
- Révocation : confirmation, effet immédiat.

### 5.11 Simulateur d'accès
- Sélecteurs : clé MCP, serveur (optionnel).
- Résultat : matrice serveurs × commandes, ou liste des commandes pour un serveur, chaque case indiquant ✔ direct / ✋ validation / — refusé, avec infobulle « autorisé par la règle X » ou « refusé : profil RO ».

### 5.12 Packs
- Liste des packs installés (version, nombre de commandes).
- Import : glisser-déposer un YAML → **aperçu des changements** (nouvelles / modifiées avec diff / inchangées) → options (créer un groupe) → confirmer.
- Export : choisir un groupe ou une sélection → téléchargement YAML.

### 5.13 Réglages
Onglets : Utilisateurs, Webhook (URL, secret, test d'envoi, derniers échecs), Syslog (protocole UDP/TCP/TLS, format texte/JSON/CEF, facility, niveau, types, certificats, état, dernière erreur, message de test), Journal (rétention, vérification d'intégrité), Apparence (thème par défaut), À propos (version).

## 6. États et feedback
- **Chargement** : squelettes (skeletons), jamais d'écran blanc.
- **Vide** : illustration minimaliste + phrase + action (« Aucun serveur. Ajoutez-en un pour commencer. [Ajouter un serveur] »).
- **Erreur** : message en langage clair + détail technique repliable.
- **Optimiste** pour les actions simples (activer / désactiver), avec annulation dans le toast.

## 7. Accessibilité et responsive
- Contrastes AA vérifiés dans les deux thèmes, y compris les badges.
- Navigation complète au clavier, focus visible, raccourcis : `g j` journal, `g v` validations, `/` recherche dans la page, `Ctrl/⌘+K` palette.
- Tableaux : en dessous de 768 px, transformation en liste de cartes.
- Libellés ARIA sur les icônes ; statuts jamais exprimés uniquement par la couleur.

## 8. Ton et microcopie
- Français par défaut, anglais disponible ; tutoiement évité, phrases courtes et factuelles.
- Parler d'« agent » (pas « IA » partout), de « commande » (pas « job »), de « règle d'accès ».
- Messages de refus explicites : « Refusée : le paramètre `lines` doit être compris entre 1 et 1000 (reçu : 50000). »
