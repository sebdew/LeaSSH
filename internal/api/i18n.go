package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// Messages d'erreur de l'API traduits à la demande (en-tête Accept-Language: en). Le code d'erreur ne change jamais;
// un message absent du catalogue reste en français. Les clés sont les messages français exacts, ou un préfixe suivi d'une partie dynamique.
var errEN = map[string]string{
	"l'instance est déjà initialisée":                   "this instance is already initialized",
	"origine refusée":                                   "origin refused",
	"corps JSON invalide":                               "invalid JSON body",
	"un objet avec ce slug ou ce nom existe déjà":       "an object with this slug or name already exists",
	"jeton CSRF invalide":                               "invalid CSRF token",
	"droits insuffisants":                               "insufficient rights",
	"l'objet est encore référencé par un autre élément": "the object is still referenced by another item",
	"erreur interne":                                    "internal error",
	"identifiants incorrects":                           "invalid credentials",
	"mot de passe actuel incorrect":                     "current password is incorrect",
	"nom d'utilisateur requis (64 caractères max)":      "username required (64 characters max)",
	"la connexion par mot de passe est réservée aux administrateurs: utilisez le SSO": "password sign-in is reserved for administrators: use SSO",
	"streaming indisponible": "streaming unavailable",
	"ressource introuvable":  "resource not found",
	"ce compte est géré par le SSO: pas de mot de passe local": "this account is managed by SSO: no local password",
	"trop de tentatives, réessayez dans quelques minutes":      "too many attempts, try again in a few minutes",
	"compte indisponible": "account unavailable",
	"connexion requise":   "sign-in required",
	"session expirée":     "session expired",
	"le mot de passe doit faire au moins 10 caractères":                                                   "the password must be at least 10 characters",
	"le groupe ne contient aucune commande":                                                               "the group contains no commands",
	"l'empreinte présentée par l'hôte ne correspond pas à celle confirmée; relancez le test de connexion": "the fingerprint presented by the host does not match the confirmed one; run the connection test again",
	"l'empreinte présentée par l'hôte ne correspond pas à celle confirmée; relancez le test":              "the fingerprint presented by the host does not match the confirmed one; run the test again",
	"une URL et un modèle sont requis pour activer la synthèse IA":                                        "a URL and a model are required to enable the AI summary",
	"décision: approve ou reject":                                                                         "decision: approve or reject",
	"description trop longue (4000 caractères max)":                                                       "description too long (4000 characters max)",
	"date d'expiration invalide (RFC 3339 ou AAAA-MM-JJ)":                                                 "invalid expiration date (RFC 3339 or YYYY-MM-DD)",
	"la date d'expiration est dans le passé":                                                              "the expiration date is in the past",
	"format: csv ou ndjson":                                                                               "format: csv or ndjson",
	"hôte invalide":                                                                                       "invalid host",
	"identité SSH inconnue":                                                                               "unknown SSH identity",
	"identité SSH requise":                                                                                "SSH identity required",
	"mot de passe requis":                                                                                 "password required",
	"nom et utilisateur SSH requis":                                                                       "name and SSH user required",
	"intervalle: 0 (manuel) à 1440 minutes":                                                               "interval: 0 (manual) to 1440 minutes",
	"sortie max entre 1 et 4096 Ko":                                                                       "max output between 1 and 4096 KB",
	"mode inconnu (generate, import, password)":                                                           "unknown mode (generate, import, password)",
	"nom requis (120 caractères max)":                                                                     "name required (120 characters max)",
	"nom requis":                                                                                          "name required",
	"issuer et client ID sont requis pour activer le SSO":                                                 "issuer and client ID are required to enable SSO",
	"l'issuer doit être une URL http(s) sans paramètre ni fragment":                                       "the issuer must be an http(s) URL without parameters or fragment",
	"le scope « openid » est obligatoire":                                                                 "the “openid” scope is required",
	"nom du fournisseur: 64 caractères maximum":                                                           "provider name: 64 characters maximum",
	"rôle par défaut: viewer ou refus":                                                                    "default role: viewer or refuse",
	"trop de valeurs de mapping":                                                                          "too many mapping values",
	"valeur de mapping trop longue":                                                                       "mapping value too long",
	"valeur trop longue":                                                                                  "value too long",
	"dates attendues au format AAAA-MM-JJ":                                                                "dates expected in YYYY-MM-DD format",
	"la date de début doit précéder la date de fin":                                                       "the start date must precede the end date",
	"la date de fin ne peut pas être dans le futur":                                                       "the end date cannot be in the future",
	"période inconnue (1h, 24h, 7d, 30d)":                                                                 "unknown period (1h, 24h, 7d, 30d)",
	"période: 7, 30 ou 90 jours":                                                                          "period: 7, 30 or 90 days",
	"port invalide":                                                                                       "invalid port",
	"profil: RO ou RW":                                                                                    "profile: RO or RW",
	"risque invalide":                                                                                     "invalid risk",
	"rôle: admin, approver ou viewer":                                                                     "role: admin, approver or viewer",
	"au moins un groupe de serveurs et un groupe de commandes requis":                                     "at least one server group and one command group required",
	"slug invalide: minuscules, chiffres, . _ - (64 caractères max)":                                      "invalid slug: lowercase letters, digits, . _ - (64 characters max)",
	"utilisateur SSH invalide (128 caractères max, sans caractère de contrôle)":                           "invalid SSH user (128 characters max, no control characters)",
	"statut invalide":                                                                                     "invalid status",
	"thème: system, light ou dark":                                                                        "theme: system, light or dark",
	"délai entre 1 et 600 secondes":                                                                       "timeout between 1 and 600 seconds",
	"il doit rester au moins un administrateur actif":                                                     "at least one active administrator must remain",
	"ce connecteur n'a pas de point d'entrée SSH unique":                                                  "this connector has no single SSH entry point",
	"il faut au moins un administrateur local actif (compte de secours) pour restreindre le mot de passe local": "at least one active local administrator (break-glass account) is required to restrict the local password",
	"renseignez d'abord l'issuer":                                                                   "enter the issuer first",
	"renseignez une URL et un secret avant de tester":                                               "enter a URL and a secret before testing",
	"un secret de signature est requis pour activer le webhook":                                     "a signing secret is required to enable the webhook",
	"vous ne pouvez pas supprimer votre propre compte":                                              "you cannot delete your own account",
	"vous ne pouvez pas désactiver votre propre compte":                                             "you cannot disable your own account",
	"fichier illisible ou trop volumineux":                                                          "unreadable or too large file",
	"une URL est requise pour activer le webhook":                                                   "a URL is required to enable the webhook",
	"la synthèse IA est désactivée: activez-la d'abord dans les réglages de l'audit":                "the AI summary is disabled: enable it first in the audit settings",
	"compte SSO: le rôle vient du mapping des revendications et il n'y a pas de mot de passe local": "SSO account: the role comes from the claims mapping and there is no local password",
	"la demande n'est plus en attente":                                                              "the request is no longer pending",
	"confirmation requise: saisir le slug du serveur":                                               "confirmation required: type the server slug",
	"un commentaire est obligatoire pour refuser":                                                   "a comment is required to reject",
	"cette clé n'est pas autorisée à proposer des commandes":                                        "this key is not allowed to propose commands",
	"trop de propositions en attente pour cette clé (10 maximum)":                                   "too many pending proposals for this key (10 maximum)",
	"proposition déjà traitée":                                                                      "proposal already processed",
	"la proposition ne passe pas le linter":                                                         "the proposal does not pass the linter",
	"pack invalide":                                                                                 "invalid pack",
	"import refusé":                                                                                 "import refused",
}

// Messages dont la fin est dynamique: le préfixe français est remplacé, la suite est conservée.
var errENPrefix = [][2]string{
	{"statut invalide: ", "invalid status: "},
	{"scope invalide: ", "invalid scope: "},
	{"date invalide (RFC 3339 attendu): ", "invalid date (RFC 3339 expected): "},
	{"hôte injoignable: ", "host unreachable: "},
	{"pack invalide: fichier trop volumineux", "invalid pack: file too large"},
	{"pack invalide: pack.name et pack.version sont requis", "invalid pack: pack.name and pack.version are required"},
	{"pack invalide: id en double: ", "invalid pack: duplicate id: "},
	{"pack invalide: id de commande invalide: ", "invalid pack: invalid command id: "},
	{"pack invalide: pack.id invalide (minuscules, chiffres, - _)", "invalid pack: invalid pack.id (lowercase letters, digits, - _)"},
	{"modèle de commande invalide: ", "invalid command template: "},
}

func translateMsg(msg string) string {
	if en, ok := errEN[msg]; ok {
		return en
	}
	for _, p := range errENPrefix {
		if strings.HasPrefix(msg, p[0]) {
			return p[1] + strings.TrimPrefix(msg, p[0])
		}
	}
	return msg
}

func wantsEnglish(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language"))), "en")
}

// langMiddleware traduit le champ error.message des réponses d'erreur quand le client demande l'anglais.
// Les réponses de succès (dont les flux SSE) passent sans copie.
func langMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !wantsEnglish(r) {
			next.ServeHTTP(w, r)
			return
		}
		lw := &langWriter{ResponseWriter: w}
		next.ServeHTTP(lw, r)
		lw.finish()
	})
}

type langWriter struct {
	http.ResponseWriter
	code int
	buf  bytes.Buffer
}

func (l *langWriter) Unwrap() http.ResponseWriter { return l.ResponseWriter }

func (l *langWriter) WriteHeader(code int) {
	if l.code != 0 {
		return
	}
	l.code = code
	if code < 400 {
		l.ResponseWriter.WriteHeader(code)
	}
}

func (l *langWriter) Write(b []byte) (int, error) {
	if l.code == 0 {
		l.WriteHeader(http.StatusOK)
	}
	if l.code < 400 {
		return l.ResponseWriter.Write(b)
	}
	return l.buf.Write(b)
}

func (l *langWriter) Flush() {
	if f, ok := l.ResponseWriter.(http.Flusher); ok && l.code < 400 {
		f.Flush()
	}
}

// finish réécrit une réponse d'erreur JSON bufferisée avec le message traduit.
func (l *langWriter) finish() {
	if l.code < 400 {
		return
	}
	body := l.buf.Bytes()
	var env struct {
		Error map[string]any `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error != nil {
		if m, ok := env.Error["message"].(string); ok {
			env.Error["message"] = translateMsg(m)
			if out, err := json.Marshal(env); err == nil {
				body = append(out, '\n')
			}
		}
	}
	h := l.ResponseWriter.Header()
	h.Del("Content-Length")
	l.ResponseWriter.WriteHeader(l.code)
	_, _ = l.ResponseWriter.Write(body)
}
