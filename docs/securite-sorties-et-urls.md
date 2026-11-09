# Sorties de commandes et URL sortantes (SEC-07, SEC-08)

Note d'investigation du ticket #10. Source : `docs/revues/2026-11-07-revue.md`. Niveau du projet : N2 (usage interne).

| Question | Décision |
|---|---|
| SEC-07 · injection de prompt par une sortie de commande | **Accepté**, avec documentation |
| SEC-08 · webhook, IA, bastion vers des adresses privées | **Accepté** (aveugle, aucun gain pour un administrateur) |
| SEC-08 · test OIDC qui renvoie le corps d'une réponse distante | **Démontré** → ticket de correction (voir §3) |

## 1. SEC-07 : la sortie des commandes est renvoyée à l'agent

**Constat.** `stdout` et `stderr` sont renvoyés tels quels par `run_command` et `get_execution`. Un contenu contrôlé par un tiers (ligne de journal, page, nom de fichier) peut contenir des instructions destinées au modèle (OWASP LLM01).

**Ce que l'attaque ne peut pas faire.** L'agent ne dispose que des outils MCP :
- il n'exécute que des commandes du catalogue, avec des paramètres typés et échappés (invariant 2) ;
- `write` et `destructive` exigent une validation humaine, qui affiche la commande rendue et la raison ;
- une clé MCP ne peut rien modifier de la configuration (invariant 5) ;
- les refus sont journalisés (invariant 6).

**Dommage résiduel.** Il se limite aux commandes que la clé peut lancer sans validation : les commandes `read` autorisées, et les écritures des règles « sans validation ». Une injection peut faire lancer une commande de lecture inutile ou en lire la sortie. Cela ne sort pas du périmètre accordé à la clé. Le risque dépend donc du contenu des commandes `read` activées. Il n'est pas démontrable au-delà.

**Décision : accepté.** Le périmètre est déjà borné par le catalogue, les droits de la clé et la validation humaine. Un cadre « sortie non fiable » dans le résultat MCP n'est pas une barrière : il aide le modèle mais ne garantit rien, et change le format de sortie des outils. Il n'est pas retenu pour l'instant.

**Mesures.** Le risque est documenté dans `SPEC.md` (§ sécurité) et `INSTALL.md` : ne pas activer, sur une clé en lecture sans validation, une commande qui renvoie du contenu non maîtrisé sans en mesurer l'effet ; préférer la validation humaine pour ces cas.

## 2. SEC-08 : URL sortantes vers des adresses privées

| Fonction | Fichier | Ce que reçoit l'administrateur |
|---|---|---|
| Webhook | `internal/webhook/webhook.go` | code HTTP et erreur réseau (aveugle), redirections non suivies |
| Synthèse IA | `internal/auditreport/ai.go` | « le modèle a répondu HTTP n » (aveugle), redirections non suivies |
| Bastion / connecteur | `internal/provisioning/warpgate` | liste de cibles analysée, rien de brut |
| Serveurs SSH | `internal/sshexec` | erreur de connexion (sonde de port) |

**Analyse.** Un administrateur peut déjà déclarer un serveur SSH sur n'importe quelle adresse et port, et lire l'erreur de connexion : il sonde déjà le réseau interne. Le webhook et l'IA ne lui apprennent rien de plus : la réponse n'est pas renvoyée, seulement un code. Aucune de ces fonctions ne suit de redirection.

**Décision : accepté** pour un usage interne (N2). Un filtre de plages privées casserait des cas légitimes (webhook Mattermost interne, Ollama ou LiteLLM sur le réseau local, Warpgate interne). Si l'outil est exposé ou repris en entreprise (N3), une option de blocage des plages privées sera à étudier.

## 3. Écart démontré : le test OIDC renvoie le corps distant

**Preuve.** `Discover` sur un issuer qui répond `500` avec le corps `SECRET-INTERNAL-BODY` renvoie :

```
découverte impossible: 500 Internal Server Error: SECRET-INTERNAL-BODY
```

`go-oidc` cite le corps de la réponse dans son erreur. Le gestionnaire du test OIDC (`internal/api/handlers_oidc.go`, `res["error"] = err.Error()`) renvoie ce message à l'administrateur et l'écrit dans le diff d'audit. Un administrateur peut donc **lire le corps d'une réponse non 2xx** d'un service interne, ce que ni SSH, ni le webhook, ni l'IA ne permettent. Ce gain est limité (réponses d'erreur), mais il dépasse la sonde aveugle.

**Décision : traité** par un ticket de correction : ne renvoyer et ne journaliser ni le corps distant ni l'erreur brute (code HTTP et message générique), avec un test de non-régression. Voir le ticket lié depuis #10.
