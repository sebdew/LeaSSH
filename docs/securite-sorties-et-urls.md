# Sorties de commandes et URL sortantes (SEC-07, SEC-08)

Note d'investigation du ticket #10. Source : `docs/revues/2026-11-07-revue.md`. Niveau du projet : N3 (exposé ou destiné à l'entreprise).

| Question | Décision |
|---|---|
| SEC-07 · injection de prompt par une sortie de commande | **Risque retenu** → ticket [#36](https://github.com/sebdew/LeaSSH/issues/36) |
| SEC-08 · webhook, IA, bastion vers des adresses privées | **Accepté** : les adresses privées sont le cas d'usage de l'outil, décision assumée par l'[ADR 0006](adr/0006-adresses-privees-urls-sortantes.md) |
| SEC-08 · test OIDC qui renvoie le corps d'une réponse distante | **Démontré** → ticket [#35](https://github.com/sebdew/LeaSSH/issues/35) |

## 1. SEC-07 : la sortie des commandes est renvoyée à l'agent

**Constat.** `stdout` et `stderr` sont renvoyés tels quels par `run_command` et `get_execution`. Un contenu contrôlé par un tiers (ligne de journal, page, nom de fichier) peut contenir des instructions destinées au modèle (OWASP LLM01).

**Ce que l'attaque ne peut pas faire.** L'agent ne dispose que des outils MCP :
- il n'exécute que des commandes du catalogue, avec des paramètres typés et échappés (invariant 2) ;
- `write` et `destructive` exigent une validation humaine, qui affiche la commande rendue et la raison ;
- une clé MCP ne peut rien modifier de la configuration (invariant 5) ;
- les refus sont journalisés (invariant 6).

**Dommage résiduel.** Il se limite aux commandes que la clé peut lancer sans validation : les commandes `read` autorisées, et les écritures des règles « sans validation ». Une injection peut faire lancer une commande de lecture inutile ou en lire la sortie. Cela ne sort pas du périmètre accordé à la clé. Le risque dépend donc du contenu des commandes `read` activées. Il n'est pas démontrable au-delà.

**Décision : à traiter.** Le périmètre borné limite les dégâts, mais une sortie de log qui contient un prompt reste dangereuse : on ne s'en remet pas à lui. Le ticket #36 prévoit un cadre « sortie non fiable » avec neutralisation des faux délimiteurs, et un indicateur de catalogue pour les commandes à sortie non maîtrisée. En attendant, ne pas activer sans validation une commande `read` qui renvoie du contenu non maîtrisé.

## 2. SEC-08 : URL sortantes vers des adresses privées

| Fonction | Fichier | Ce que reçoit l'administrateur |
|---|---|---|
| Webhook | `internal/webhook/webhook.go` | code HTTP et erreur réseau (aveugle), redirections non suivies |
| Synthèse IA | `internal/auditreport/ai.go` | « le modèle a répondu HTTP n » (aveugle), redirections non suivies |
| Bastion / connecteur | `internal/provisioning/warpgate` | liste de cibles analysée, rien de brut |
| Serveurs SSH | `internal/sshexec` | erreur de connexion (sonde de port) |

**Analyse.** Un administrateur peut déjà déclarer un serveur SSH sur n'importe quelle adresse et port, et lire l'erreur de connexion : il sonde déjà le réseau interne. Le webhook et l'IA ne lui apprennent rien de plus : la réponse n'est pas renvoyée, seulement un code. Aucune de ces fonctions ne suit de redirection.

**Décision : accepté.** L'outil sert à joindre des machines en adresse privée (serveurs SSH, bastion, Mattermost, Ollama ou LiteLLM internes) : un blocage des plages privées le rendrait inutilisable. Le projet est de niveau N3 et peut être exposé derrière un reverse proxy TLS : le risque résiduel est celui d'un compte administrateur compromis, détaillé dans l'[ADR 0006](adr/0006-adresses-privees-urls-sortantes.md) avec les mesures de réseau recommandées. Le rôle administrateur reste réservé à des personnes de confiance.

## 3. Écart démontré : le test OIDC renvoie le corps distant

**Preuve.** `Discover` sur un issuer qui répond `500` avec le corps `SECRET-INTERNAL-BODY` renvoie :

```
découverte impossible: 500 Internal Server Error: SECRET-INTERNAL-BODY
```

`go-oidc` cite le corps de la réponse dans son erreur. Le gestionnaire du test OIDC (`internal/api/handlers_oidc.go`, `res["error"] = err.Error()`) renvoie ce message à l'administrateur et l'écrit dans le diff d'audit. Un administrateur peut donc **lire le corps d'une réponse non 2xx** d'un service interne, ce que ni SSH, ni le webhook, ni l'IA ne permettent. Ce gain est limité (réponses d'erreur), mais il dépasse la sonde aveugle.

**Décision : traité** par le ticket #35 : ne renvoyer et ne journaliser ni le corps distant ni l'erreur brute (code HTTP et message générique), avec un test de non-régression.
