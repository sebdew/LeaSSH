package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/leassh-dev/leassh/internal/catalog"
)

type Command struct {
	ID          string                      `json:"id"`
	Slug        string                      `json:"slug"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Risk        catalog.Risk                `json:"risk"`
	Template    string                      `json:"template"`
	Params      map[string]catalog.ParamDef `json:"params"`
	TimeoutS    int                         `json:"timeout_s"`
	MaxOutputKB int                         `json:"max_output_kb"`
	Status      string                      `json:"status"`
	Hash        string                      `json:"hash"`
	Version     int                         `json:"version"`
	Source      string                      `json:"source"`
	PackID      string                      `json:"pack_id"`
	CreatedBy   string                      `json:"created_by"`
	ApprovedBy  string                      `json:"approved_by"`
	ApprovedAt  string                      `json:"approved_at"`
	CreatedAt   string                      `json:"created_at"`
	UpdatedAt   string                      `json:"updated_at"`
}

const cmdCols = `id,slug,name,description,risk,template,params,timeout_s,max_output_kb,status,hash,version,source,pack_id,
	coalesce(created_by,''),coalesce(approved_by,''),coalesce(approved_at,''),created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scanCommand(r scanner) (Command, error) {
	var c Command
	var params string
	err := r.Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.Risk, &c.Template, &params, &c.TimeoutS, &c.MaxOutputKB,
		&c.Status, &c.Hash, &c.Version, &c.Source, &c.PackID, &c.CreatedBy, &c.ApprovedBy, &c.ApprovedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(params), &c.Params); err != nil {
		return c, fmt.Errorf("params de %s: %w", c.Slug, err)
	}
	if c.Params == nil {
		c.Params = map[string]catalog.ParamDef{}
	}
	return c, nil
}

func ListCommands(ctx context.Context, q Q) ([]Command, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+cmdCols+` FROM commands ORDER BY slug`)
	if err != nil {
		return nil, wrapErr("liste commandes", err)
	}
	return collect(rows, func(r *sql.Rows) (Command, error) { return scanCommand(r) })
}

func CommandByID(ctx context.Context, q Q, id string) (Command, error) {
	c, err := scanCommand(q.QueryRowContext(ctx, `SELECT `+cmdCols+` FROM commands WHERE id=?`, id))
	return c, wrapErr("commande", err)
}

func CommandBySlug(ctx context.Context, q Q, slug string) (Command, error) {
	c, err := scanCommand(q.QueryRowContext(ctx, `SELECT `+cmdCols+` FROM commands WHERE slug=?`, slug))
	return c, wrapErr("commande", err)
}

func normalizeCommand(c *Command) {
	if c.Params == nil {
		c.Params = map[string]catalog.ParamDef{}
	}
	if c.TimeoutS == 0 {
		c.TimeoutS = 30
	}
	if c.MaxOutputKB == 0 {
		c.MaxOutputKB = 64
	}
	c.Hash = catalog.Hash(c.Template, c.Params, c.Risk)
}

// InsertCommand calcule le hash et fixe version=1.
func InsertCommand(ctx context.Context, tx Q, c Command) (Command, error) {
	normalizeCommand(&c)
	c.ID, c.Version = NewID(), 1
	c.CreatedAt = Now()
	c.UpdatedAt = c.CreatedAt
	if c.Status == "enabled" && c.ApprovedAt == "" {
		c.ApprovedAt, c.ApprovedBy = c.CreatedAt, c.CreatedBy
	}
	params, _ := json.Marshal(c.Params)
	_, err := tx.ExecContext(ctx, `INSERT INTO commands (id,slug,name,description,risk,template,params,timeout_s,max_output_kb,status,hash,version,source,pack_id,created_by,approved_by,approved_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Slug, c.Name, c.Description, c.Risk, c.Template, string(params), c.TimeoutS, c.MaxOutputKB, c.Status, c.Hash, c.Version,
		c.Source, c.PackID, nullStr(c.CreatedBy), nullStr(c.ApprovedBy), nullStr(c.ApprovedAt), c.CreatedAt, c.UpdatedAt)
	return c, wrapErr("création commande", err)
}

// UpdateCommand: toute modification de template/params/risk recalcule le hash et incrémente la version.
func UpdateCommand(ctx context.Context, tx Q, c Command) (Command, Command, error) {
	old, err := CommandByID(ctx, tx, c.ID)
	if err != nil {
		return Command{}, Command{}, err
	}
	normalizeCommand(&c)
	c.Version = old.Version
	if c.Hash != old.Hash {
		c.Version++
	}
	c.Source, c.PackID, c.CreatedAt, c.CreatedBy = old.Source, old.PackID, old.CreatedAt, old.CreatedBy
	c.ApprovedBy, c.ApprovedAt = old.ApprovedBy, old.ApprovedAt
	c.UpdatedAt = Now()
	params, _ := json.Marshal(c.Params)
	_, err = tx.ExecContext(ctx, `UPDATE commands SET slug=?,name=?,description=?,risk=?,template=?,params=?,timeout_s=?,max_output_kb=?,status=?,hash=?,version=?,approved_by=?,approved_at=?,updated_at=? WHERE id=?`,
		c.Slug, c.Name, c.Description, c.Risk, c.Template, string(params), c.TimeoutS, c.MaxOutputKB, c.Status, c.Hash, c.Version,
		nullStr(c.ApprovedBy), nullStr(c.ApprovedAt), c.UpdatedAt, c.ID)
	return c, old, wrapErr("mise à jour commande", err)
}

func DeleteCommand(ctx context.Context, tx Q, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM commands WHERE id=?`, id)
	return wrapErr("suppression commande", err)
}

// ---- Groupes (serveurs / commandes) ----

type GroupKind string

const (
	ServerGroups  GroupKind = "server"
	CommandGroups GroupKind = "command"
)

func (k GroupKind) tables() (group, members, col string) {
	if k == ServerGroups {
		return "server_groups", "server_group_members", "server_id"
	}
	return "command_groups", "command_group_members", "command_id"
}

type Group struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MemberIDs   []string `json:"member_ids"`
	// Groupes de serveurs tenus par une synchro: ManagedBy = id du provisioner, ManagedMemberIDs = membres posés par elle.
	ManagedBy        string   `json:"managed_by"`
	ExternalID       string   `json:"external_id"`
	ManagedMemberIDs []string `json:"managed_member_ids"`
}

func ListGroups(ctx context.Context, q Q, k GroupKind) ([]Group, error) {
	g, m, col := k.tables()
	managed, managedMember := `'',''`, `0`
	if k == ServerGroups {
		managed, managedMember = `managed_by,external_id`, `managed`
	}
	rows, err := q.QueryContext(ctx, `SELECT id,slug,name,description,`+managed+` FROM `+g+` ORDER BY slug`)
	if err != nil {
		return nil, wrapErr("liste groupes", err)
	}
	groups, err := collect(rows, func(r *sql.Rows) (Group, error) {
		var x Group
		return x, r.Scan(&x.ID, &x.Slug, &x.Name, &x.Description, &x.ManagedBy, &x.ExternalID)
	})
	if err != nil {
		return nil, wrapErr("liste groupes", err)
	}
	mrows, err := q.QueryContext(ctx, `SELECT group_id,`+col+`,`+managedMember+` FROM `+m+` ORDER BY `+col)
	if err != nil {
		return nil, wrapErr("membres", err)
	}
	type member struct {
		group, id string
		managed   bool
	}
	pairs, err := collect(mrows, func(r *sql.Rows) (member, error) {
		var p member
		return p, r.Scan(&p.group, &p.id, &p.managed)
	})
	if err != nil {
		return nil, wrapErr("membres", err)
	}
	idx := map[string]*Group{}
	for i := range groups {
		groups[i].MemberIDs, groups[i].ManagedMemberIDs = []string{}, []string{}
		idx[groups[i].ID] = &groups[i]
	}
	for _, p := range pairs {
		if g := idx[p.group]; g != nil {
			g.MemberIDs = append(g.MemberIDs, p.id)
			if p.managed {
				g.ManagedMemberIDs = append(g.ManagedMemberIDs, p.id)
			}
		}
	}
	return groups, nil
}

func GroupByID(ctx context.Context, q Q, k GroupKind, id string) (Group, error) {
	all, err := ListGroups(ctx, q, k)
	if err != nil {
		return Group{}, err
	}
	for _, g := range all {
		if g.ID == id {
			return g, nil
		}
	}
	return Group{}, ErrNotFound
}

func InsertGroup(ctx context.Context, tx Q, k GroupKind, g Group) (Group, error) {
	t, _, _ := k.tables()
	g.ID = NewID()
	_, err := tx.ExecContext(ctx, `INSERT INTO `+t+` (id,slug,name,description) VALUES (?,?,?,?)`, g.ID, g.Slug, g.Name, g.Description)
	if err != nil {
		return g, wrapErr("création groupe", err)
	}
	return g, SetGroupMembers(ctx, tx, k, g.ID, g.MemberIDs)
}

func UpdateGroup(ctx context.Context, tx Q, k GroupKind, g Group) error {
	t, _, _ := k.tables()
	res, err := tx.ExecContext(ctx, `UPDATE `+t+` SET slug=?,name=?,description=? WHERE id=?`, g.Slug, g.Name, g.Description, g.ID)
	if err != nil {
		return wrapErr("mise à jour groupe", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if g.MemberIDs != nil {
		return SetGroupMembers(ctx, tx, k, g.ID, g.MemberIDs)
	}
	return nil
}

func DeleteGroup(ctx context.Context, tx Q, k GroupKind, id string) error {
	t, _, _ := k.tables()
	_, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE id=?`, id)
	return wrapErr("suppression groupe", err)
}

// SetGroupMembers remplace la liste des membres. Les appartenances posées par une synchro (groupes de serveurs)
// ne peuvent pas être retirées: elles sont conservées quelle que soit la liste reçue.
func SetGroupMembers(ctx context.Context, tx Q, k GroupKind, groupID string, ids []string) error {
	_, m, col := k.tables()
	var keep []string
	if k == ServerGroups {
		rows, err := tx.QueryContext(ctx, `SELECT server_id FROM server_group_members WHERE group_id=? AND managed=1`, groupID)
		if err != nil {
			return wrapErr("membres synchronisés", err)
		}
		if keep, err = collect(rows, func(r *sql.Rows) (string, error) { var v string; return v, r.Scan(&v) }); err != nil {
			return wrapErr("membres synchronisés", err)
		}
		ids = append(append([]string{}, ids...), keep...)
	}
	del := `DELETE FROM ` + m + ` WHERE group_id=?`
	if k == ServerGroups {
		del += ` AND managed=0`
	}
	if _, err := tx.ExecContext(ctx, del, groupID); err != nil {
		return wrapErr("membres", err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO `+m+` (group_id,`+col+`) VALUES (?,?)`, groupID, id); err != nil {
			return wrapErr("membre "+id, err)
		}
	}
	return nil
}
