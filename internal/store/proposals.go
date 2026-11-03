package store

import (
	"context"
	"database/sql"
)

type Proposal struct {
	ID            string `json:"id"`
	KeyID         string `json:"key_id"`
	KeyName       string `json:"key_name"`
	Payload       string `json:"payload"` // JSON
	Justification string `json:"justification"`
	Lint          string `json:"lint"` // JSON
	Status        string `json:"status"`
	ReviewedBy    string `json:"reviewed_by"`
	ReviewedAt    string `json:"reviewed_at"`
	ReviewComment string `json:"review_comment"`
	CommandID     string `json:"command_id"`
	CreatedAt     string `json:"created_at"`
}

const propCols = `id,key_id,key_name,payload,justification,lint,status,coalesce(reviewed_by,''),coalesce(reviewed_at,''),review_comment,coalesce(command_id,''),created_at`

func scanProposal(r scanner) (Proposal, error) {
	var p Proposal
	err := r.Scan(&p.ID, &p.KeyID, &p.KeyName, &p.Payload, &p.Justification, &p.Lint, &p.Status, &p.ReviewedBy, &p.ReviewedAt, &p.ReviewComment, &p.CommandID, &p.CreatedAt)
	return p, err
}

func InsertProposal(ctx context.Context, tx Q, p Proposal) (Proposal, error) {
	p.ID, p.CreatedAt = NewID(), Now()
	_, err := tx.ExecContext(ctx, `INSERT INTO proposals (id,key_id,key_name,payload,justification,lint,status,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		p.ID, p.KeyID, p.KeyName, p.Payload, p.Justification, p.Lint, p.Status, p.CreatedAt)
	return p, wrapErr("création proposition", err)
}

func ProposalByID(ctx context.Context, q Q, id string) (Proposal, error) {
	p, err := scanProposal(q.QueryRowContext(ctx, `SELECT `+propCols+` FROM proposals WHERE id=?`, id))
	return p, wrapErr("proposition", err)
}

func ListProposals(ctx context.Context, q Q, status string) ([]Proposal, error) {
	query, args := `SELECT `+propCols+` FROM proposals`, []any{}
	if status != "" {
		query += ` WHERE status=?`
		args = append(args, status)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, wrapErr("propositions", err)
	}
	return collect(rows, func(r *sql.Rows) (Proposal, error) { return scanProposal(r) })
}

// CountOpenProposals: propositions non traitées (pending ou invalid) d'une clé.
func CountOpenProposals(ctx context.Context, q Q, keyID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM proposals WHERE key_id=? AND status IN ('pending','invalid')`, keyID).Scan(&n)
	return n, err
}

func ReviewProposal(ctx context.Context, q Q, id, status, reviewer, comment, commandID string) error {
	_, err := q.ExecContext(ctx, `UPDATE proposals SET status=?,reviewed_by=?,reviewed_at=?,review_comment=?,command_id=? WHERE id=?`,
		status, nullStr(reviewer), Now(), comment, nullStr(commandID), id)
	return wrapErr("revue proposition", err)
}
