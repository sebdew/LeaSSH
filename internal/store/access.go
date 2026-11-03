package store

import (
	"context"
	"database/sql"
)

type MCPKey struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Prefix      string   `json:"prefix"`
	Hash        string   `json:"-"`
	ExpiresAt   string   `json:"expires_at"`
	LastUsedAt  string   `json:"last_used_at"`
	LastUsedIP  string   `json:"last_used_ip"`
	UseCount    int64    `json:"use_count"`
	CanPropose  bool     `json:"can_propose"`
	Disabled    bool     `json:"disabled"`
	CreatedBy   string   `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
	RuleIDs     []string `json:"rule_ids"`
}

const keyCols = `id,name,description,prefix,hash,coalesce(expires_at,''),coalesce(last_used_at,''),last_used_ip,use_count,can_propose,disabled,coalesce(created_by,''),created_at`

func scanKey(r scanner) (MCPKey, error) {
	var k MCPKey
	err := r.Scan(&k.ID, &k.Name, &k.Description, &k.Prefix, &k.Hash, &k.ExpiresAt, &k.LastUsedAt, &k.LastUsedIP, &k.UseCount, &k.CanPropose, &k.Disabled, &k.CreatedBy, &k.CreatedAt)
	k.RuleIDs = []string{}
	return k, err
}

func attachRules(ctx context.Context, q Q, keys []MCPKey) error {
	rows, err := q.QueryContext(ctx, `SELECT key_id,rule_id FROM access_rule_keys ORDER BY rule_id`)
	if err != nil {
		return wrapErr("règles de clés", err)
	}
	pairs, err := collect(rows, func(r *sql.Rows) ([2]string, error) {
		var p [2]string
		return p, r.Scan(&p[0], &p[1])
	})
	if err != nil {
		return wrapErr("règles de clés", err)
	}
	idx := map[string]*MCPKey{}
	for i := range keys {
		idx[keys[i].ID] = &keys[i]
	}
	for _, p := range pairs {
		if k := idx[p[0]]; k != nil {
			k.RuleIDs = append(k.RuleIDs, p[1])
		}
	}
	return nil
}

func ListKeys(ctx context.Context, q Q) ([]MCPKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+keyCols+` FROM mcp_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, wrapErr("clés", err)
	}
	keys, err := collect(rows, func(r *sql.Rows) (MCPKey, error) { return scanKey(r) })
	if err != nil {
		return nil, wrapErr("clés", err)
	}
	return keys, attachRules(ctx, q, keys)
}

func KeyByID(ctx context.Context, q Q, id string) (MCPKey, error) {
	k, err := scanKey(q.QueryRowContext(ctx, `SELECT `+keyCols+` FROM mcp_keys WHERE id=?`, id))
	if err != nil {
		return k, wrapErr("clé", err)
	}
	ks := []MCPKey{k}
	err = attachRules(ctx, q, ks)
	return ks[0], err
}

// KeyByHash retrouve une clé à partir du hash SHA-256 du jeton.
func KeyByHash(ctx context.Context, q Q, hash string) (MCPKey, error) {
	k, err := scanKey(q.QueryRowContext(ctx, `SELECT `+keyCols+` FROM mcp_keys WHERE hash=?`, hash))
	return k, wrapErr("clé", err)
}

func InsertKey(ctx context.Context, tx Q, k MCPKey) (MCPKey, error) {
	k.ID, k.CreatedAt = NewID(), Now()
	_, err := tx.ExecContext(ctx, `INSERT INTO mcp_keys (id,name,description,prefix,hash,expires_at,can_propose,disabled,created_by,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		k.ID, k.Name, k.Description, k.Prefix, k.Hash, nullStr(k.ExpiresAt), k.CanPropose, k.Disabled, nullStr(k.CreatedBy), k.CreatedAt)
	if err != nil {
		return k, wrapErr("création clé", err)
	}
	return k, SetKeyRules(ctx, tx, k.ID, k.RuleIDs)
}

func UpdateKey(ctx context.Context, tx Q, k MCPKey) error {
	res, err := tx.ExecContext(ctx, `UPDATE mcp_keys SET name=?,description=?,expires_at=?,can_propose=?,disabled=? WHERE id=?`,
		k.Name, k.Description, nullStr(k.ExpiresAt), k.CanPropose, k.Disabled, k.ID)
	if err != nil {
		return wrapErr("mise à jour clé", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if k.RuleIDs != nil {
		return SetKeyRules(ctx, tx, k.ID, k.RuleIDs)
	}
	return nil
}

func SetKeyRules(ctx context.Context, tx Q, keyID string, ruleIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_keys WHERE key_id=?`, keyID); err != nil {
		return wrapErr("règles de clé", err)
	}
	for _, r := range ruleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO access_rule_keys (rule_id,key_id) VALUES (?,?)`, r, keyID); err != nil {
			return wrapErr("règle "+r, err)
		}
	}
	return nil
}

func DeleteKey(ctx context.Context, tx Q, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM mcp_keys WHERE id=?`, id)
	return wrapErr("suppression clé", err)
}

// TouchKey met à jour last_used_at / IP et le compteur (appel list_*, get_execution, run_command…).
func TouchKey(ctx context.Context, q Q, id, ip string) error {
	_, err := q.ExecContext(ctx, `UPDATE mcp_keys SET last_used_at=?,last_used_ip=?,use_count=use_count+1 WHERE id=?`, Now(), ip, id)
	return err
}

type AccessRule struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	ServerGroupIDs        []string `json:"server_group_ids"`
	CommandGroupIDs       []string `json:"command_group_ids"`
	Profile               string   `json:"profile"`
	WriteRequiresApproval bool     `json:"write_requires_approval"`
	Enabled               bool     `json:"enabled"`
	KeyIDs                []string `json:"key_ids"`
}

// pairsOf lit une table de liaison (rule_id, x) et regroupe par règle.
func pairsOf(ctx context.Context, q Q, query string) (map[string][]string, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapErr("liaisons de règles", err)
	}
	pairs, err := collect(rows, func(r *sql.Rows) ([2]string, error) {
		var p [2]string
		return p, r.Scan(&p[0], &p[1])
	})
	if err != nil {
		return nil, wrapErr("liaisons de règles", err)
	}
	m := map[string][]string{}
	for _, p := range pairs {
		m[p[0]] = append(m[p[0]], p[1])
	}
	return m, nil
}

func ListRules(ctx context.Context, q Q) ([]AccessRule, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,name,profile,write_requires_approval,enabled FROM access_rules ORDER BY name,id`)
	if err != nil {
		return nil, wrapErr("règles", err)
	}
	rules, err := collect(rows, func(r *sql.Rows) (AccessRule, error) {
		var x AccessRule
		return x, r.Scan(&x.ID, &x.Name, &x.Profile, &x.WriteRequiresApproval, &x.Enabled)
	})
	if err != nil {
		return nil, wrapErr("règles", err)
	}
	keys, err := pairsOf(ctx, q, `SELECT rule_id,key_id FROM access_rule_keys ORDER BY key_id`)
	if err != nil {
		return nil, err
	}
	sgs, err := pairsOf(ctx, q, `SELECT rule_id,group_id FROM access_rule_server_groups ORDER BY group_id`)
	if err != nil {
		return nil, err
	}
	cgs, err := pairsOf(ctx, q, `SELECT rule_id,group_id FROM access_rule_command_groups ORDER BY group_id`)
	if err != nil {
		return nil, err
	}
	orEmpty := func(v []string) []string {
		if v == nil {
			return []string{}
		}
		return v
	}
	for i := range rules {
		id := rules[i].ID
		rules[i].KeyIDs, rules[i].ServerGroupIDs, rules[i].CommandGroupIDs = orEmpty(keys[id]), orEmpty(sgs[id]), orEmpty(cgs[id])
	}
	return rules, nil
}

func RuleByID(ctx context.Context, q Q, id string) (AccessRule, error) {
	all, err := ListRules(ctx, q)
	if err != nil {
		return AccessRule{}, err
	}
	for _, r := range all {
		if r.ID == id {
			return r, nil
		}
	}
	return AccessRule{}, ErrNotFound
}

func InsertRule(ctx context.Context, tx Q, r AccessRule) (AccessRule, error) {
	r.ID = NewID()
	_, err := tx.ExecContext(ctx, `INSERT INTO access_rules (id,name,profile,write_requires_approval,enabled) VALUES (?,?,?,?,?)`,
		r.ID, r.Name, r.Profile, r.WriteRequiresApproval, r.Enabled)
	if err != nil {
		return r, wrapErr("création règle", err)
	}
	if err := SetRuleGroups(ctx, tx, r.ID, r.ServerGroupIDs, r.CommandGroupIDs); err != nil {
		return r, err
	}
	return r, SetRuleKeys(ctx, tx, r.ID, r.KeyIDs)
}

func UpdateRule(ctx context.Context, tx Q, r AccessRule) error {
	res, err := tx.ExecContext(ctx, `UPDATE access_rules SET name=?,profile=?,write_requires_approval=?,enabled=? WHERE id=?`,
		r.Name, r.Profile, r.WriteRequiresApproval, r.Enabled, r.ID)
	if err != nil {
		return wrapErr("mise à jour règle", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := SetRuleGroups(ctx, tx, r.ID, r.ServerGroupIDs, r.CommandGroupIDs); err != nil {
		return err
	}
	return SetRuleKeys(ctx, tx, r.ID, r.KeyIDs)
}

// SetRuleGroups remplace les groupes de serveurs et de commandes visés par la règle.
func SetRuleGroups(ctx context.Context, tx Q, ruleID string, serverGroups, commandGroups []string) error {
	for _, t := range []struct {
		table string
		ids   []string
	}{{"access_rule_server_groups", serverGroups}, {"access_rule_command_groups", commandGroups}} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t.table+` WHERE rule_id=?`, ruleID); err != nil {
			return wrapErr("groupes de règle", err)
		}
		for _, g := range t.ids {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO `+t.table+` (rule_id,group_id) VALUES (?,?)`, ruleID, g); err != nil {
				return wrapErr("groupe "+g, err)
			}
		}
	}
	return nil
}

// AddRuleServerGroup ajoute un groupe de serveurs à une règle sans toucher au reste.
func AddRuleServerGroup(ctx context.Context, tx Q, ruleID, groupID string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO access_rule_server_groups (rule_id,group_id) VALUES (?,?)`, ruleID, groupID)
	return wrapErr("groupe de règle", err)
}

func SetRuleKeys(ctx context.Context, tx Q, ruleID string, keyIDs []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_rule_keys WHERE rule_id=?`, ruleID); err != nil {
		return wrapErr("clés de règle", err)
	}
	for _, k := range keyIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO access_rule_keys (rule_id,key_id) VALUES (?,?)`, ruleID, k); err != nil {
			return wrapErr("clé "+k, err)
		}
	}
	return nil
}

func DeleteRule(ctx context.Context, tx Q, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM access_rules WHERE id=?`, id)
	return wrapErr("suppression règle", err)
}
