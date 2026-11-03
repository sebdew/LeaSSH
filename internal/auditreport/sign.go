package auditreport

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/leash-dev/leash/internal/store"
)

const EnvelopeFormat = "leash-audit-report/1"

// Signature accompagne l'instantané dans le fichier JSON exporté.
//
//   - Digest: SHA-256 du JSON compact de « snapshot ». N'importe qui peut le recalculer
//     (jq -cj .snapshot | sha256sum): il prouve que le contenu n'a pas changé depuis l'export.
//   - Tag: HMAC-SHA256 dérivé de la clé maître de l'instance. Seule l'instance émettrice peut le calculer ou le vérifier;
//     il prouve que ce rapport a été produit par elle (pas fabriqué ensuite avec un digest recalculé).
type Signature struct {
	Algorithm    string `json:"algorithm"`
	Digest       string `json:"digest"`
	ReportID     string `json:"report_id"`
	GeneratedAt  string `json:"generated_at"`
	LeashVersion string `json:"leash_version"`
	TagAlgorithm string `json:"tag_algorithm"`
	Tag          string `json:"tag"`
	HowToVerify  string `json:"how_to_verify"`
}

type Envelope struct {
	Format    string          `json:"format"`
	Signature Signature       `json:"signature"`
	Snapshot  json.RawMessage `json:"snapshot"`
}

// Tag calcule l'étiquette d'authenticité (key = Box.Derive("audit-report")).
func Tag(key []byte, digest, reportID, createdAt string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(digest + "\n" + reportID + "\n" + createdAt))
	return hex.EncodeToString(m.Sum(nil))
}

// NewEnvelope assemble le fichier exportable d'un rapport enregistré.
func NewEnvelope(r store.AuditReport) Envelope {
	return Envelope{Format: EnvelopeFormat, Snapshot: json.RawMessage(r.Snapshot), Signature: Signature{
		Algorithm: "sha256", Digest: r.SHA256, ReportID: r.ID, GeneratedAt: r.CreatedAt, LeashVersion: r.LeashVersion, TagAlgorithm: "hmac-sha256", Tag: r.HMAC,
		HowToVerify: "Digest: jq -cj .snapshot rapport.json | sha256sum. Authenticité: POST /api/v1/audit-reports/verify sur l'instance émettrice (le tag dépend de sa clé maître).",
	}}
}

type VerifyResult struct {
	DigestOK bool   `json:"digest_ok"`
	TagOK    bool   `json:"tag_ok"` // vrai seulement si l'instance courante est celle qui a produit le fichier
	Digest   string `json:"digest"`
	ReportID string `json:"report_id"`
	Message  string `json:"message"`
}

// Verify contrôle un fichier JSON exporté: empreinte du contenu et étiquette d'authenticité.
func Verify(file []byte, key []byte) (VerifyResult, error) {
	var env Envelope
	if err := json.Unmarshal(file, &env); err != nil || env.Format != EnvelopeFormat || len(env.Snapshot) == 0 {
		return VerifyResult{}, errors.New("fichier illisible: ce n'est pas un rapport d'audit LeaSSH")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, env.Snapshot); err != nil {
		return VerifyResult{}, errors.New("instantané illisible")
	}
	res := VerifyResult{Digest: Digest(compact.Bytes()), ReportID: env.Signature.ReportID}
	res.DigestOK = subtle.ConstantTimeCompare([]byte(res.Digest), []byte(env.Signature.Digest)) == 1
	want := Tag(key, env.Signature.Digest, env.Signature.ReportID, env.Signature.GeneratedAt)
	res.TagOK = res.DigestOK && subtle.ConstantTimeCompare([]byte(want), []byte(env.Signature.Tag)) == 1
	switch {
	case !res.DigestOK:
		res.Message = "Le contenu a été modifié: son empreinte ne correspond pas à celle du rapport d'origine."
	case !res.TagOK:
		res.Message = "Le contenu est intact, mais l'étiquette d'authenticité n'est pas reconnue par cette instance (rapport d'une autre instance, ou étiquette altérée)."
	default:
		res.Message = "Rapport authentique et intact: émis par cette instance, contenu inchangé."
	}
	return res, nil
}
