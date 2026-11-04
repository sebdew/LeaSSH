package provisioning

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

// ErrBlocked: la synchro retirerait trop de machines d'un coup; elle demande une confirmation explicite.
var ErrBlocked = errors.New("synchronisation bloquée")

var errPreview = errors.New("aperçu")

// Change est une modification appliquée (ou à appliquer) par la synchro.
type Change struct {
	Kind   string `json:"kind"`   // server | group | membership
	Action string `json:"action"` // create | update | disable | enable | delete | release | add | remove
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

type Report struct {
	Changes  []Change `json:"changes"`
	Machines int      `json:"machines"`          // machines SSH découvertes
	Blocked  string   `json:"blocked,omitempty"` // raison du blocage, le cas échéant
	managed  int      // serveurs déjà tenus par cette synchro, avant application (garde-fou)
}

// Actor identifie l'auteur d'une synchro dans le journal d'administration.
type Actor struct{ Type, ID, Name, IP string }

type Service struct {
	st         *store.Store
	box        *crypto.Box
	log        *slog.Logger
	mu         sync.Mutex // une synchro à la fois
	Invalidate func(serverID string)
}

func New(st *store.Store, box *crypto.Box, log *slog.Logger) *Service {
	return &Service{st: st, box: box, log: log}
}

// Connector construit le connecteur d'un provisioner (secret déchiffré en mémoire seulement).
func (s *Service) Connector(ctx context.Context, p store.Provisioner) (Connector, error) {
	t, err := Lookup(p.Type)
	if err != nil {
		return nil, err
	}
	var secret string
	if len(p.SecretEnc) > 0 {
		b, err := s.box.Open(p.SecretEnc)
		if err != nil {
			return nil, fmt.Errorf("déchiffrement du jeton: %w", err)
		}
		secret = string(b)
	}
	ident, err := store.IdentityByID(ctx, s.st.DB, p.IdentityID)
	if err != nil {
		return nil, fmt.Errorf("identité SSH introuvable: %w", err)
	}
	user := strings.TrimSpace(p.DefaultUser)
	if user == "" {
		user = ident.Username
	}
	return t.New(p.Config, secret, Env{User: user})
}

// Preview calcule les changements sans rien écrire.
func (s *Service) Preview(ctx context.Context, id string) (Report, error) {
	return s.run(ctx, id, false, true, Actor{})
}

// Sync applique la synchro. Sans force, un retrait massif de machines est refusé (ErrBlocked, rien n'est écrit).
func (s *Service) Sync(ctx context.Context, id string, force bool, by Actor) (Report, error) {
	return s.run(ctx, id, force, false, by)
}

func (s *Service) run(ctx context.Context, id string, force, preview bool, by Actor) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := store.ProvisionerByID(ctx, s.st.DB, id)
	if err != nil {
		return Report{}, err
	}
	rep, err := s.discoverAndApply(ctx, p, force, preview, by)
	if !preview {
		status, msg := "ok", fmt.Sprintf("%d machine(s), %d changement(s)", rep.Machines, len(rep.Changes))
		if err != nil {
			status, msg = "error", err.Error()
		}
		_ = store.SetProvisionerSync(ctx, s.st.DB, p.ID, status, msg)
	}
	return rep, err
}

func (s *Service) discoverAndApply(ctx context.Context, p store.Provisioner, force, preview bool, by Actor) (Report, error) {
	conn, err := s.Connector(ctx, p)
	if err != nil {
		return Report{}, err
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	machines, err := conn.Discover(dctx)
	if err != nil {
		return Report{}, err
	}
	var rep Report
	var invalidate []string
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if rep, invalidate, err = apply(ctx, tx, p, machines); err != nil {
			return err
		}
		if blocked := blockReason(rep, force); blocked != "" {
			rep.Blocked = blocked
			return fmt.Errorf("%w: %s", ErrBlocked, blocked)
		}
		if preview {
			return errPreview
		}
		if len(rep.Changes) == 0 {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: by.Type, ActorID: by.ID, ActorName: by.Name, IP: by.IP, Action: "provisioner.sync",
			TargetType: "provisioner", TargetID: p.ID, TargetName: p.Name, Diff: map[string]any{"changes": rep.Changes, "forced": force}})
	})
	if errors.Is(err, errPreview) {
		err = nil
	}
	if err == nil && !preview && s.Invalidate != nil {
		for _, id := range invalidate {
			s.Invalidate(id)
		}
	}
	return rep, err
}

// blockReason: garde-fou contre une source qui renverrait une liste vide ou amputée par erreur.
func blockReason(rep Report, force bool) string {
	if force {
		return ""
	}
	disabled := 0
	for _, c := range rep.Changes {
		if c.Kind == "server" && c.Action == "disable" {
			disabled++
		}
	}
	if (disabled >= 3 && disabled*100 > rep.managed*30) || (rep.Machines == 0 && disabled > 0) {
		return fmt.Sprintf("%d machine(s) sur %d seraient désactivées; vérifiez la source, puis confirmez la synchronisation", disabled, rep.managed)
	}
	return ""
}

var (
	nonSlug  = regexp.MustCompile(`[^a-z0-9]+`)
	accents  = strings.NewReplacer("é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ä", "a", "î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c")
	slugTrim = "-"
)

func slugify(name string) string {
	s := nonSlug.ReplaceAllString(accents.Replace(strings.ToLower(name)), "-")
	s = strings.Trim(s, slugTrim)
	if len(s) > 56 {
		s = strings.Trim(s[:56], slugTrim)
	}
	if s == "" {
		s = "item"
	}
	return s
}

func uniqueSlug(base string, taken map[string]bool) string {
	s := base
	for i := 2; taken[s]; i++ {
		s = fmt.Sprintf("%s-%d", base, i)
	}
	taken[s] = true
	return s
}

// apply écrit dans tx l'état voulu; l'appelant décide de valider ou d'annuler (aperçu).
func apply(ctx context.Context, tx *sql.Tx, p store.Provisioner, machines []Machine) (Report, []string, error) {
	rep := Report{Machines: len(machines), Changes: []Change{}}
	var touched []string
	add := func(kind, action, name, detail string) {
		rep.Changes = append(rep.Changes, Change{kind, action, name, detail})
	}

	servers, err := store.ListServers(ctx, tx)
	if err != nil {
		return rep, nil, err
	}
	groups, err := store.ListGroups(ctx, tx, store.ServerGroups)
	if err != nil {
		return rep, nil, err
	}
	serverSlugs, groupSlugs := map[string]bool{}, map[string]bool{}
	mine := map[string]store.Server{}
	for _, sv := range servers {
		serverSlugs[sv.Slug] = true
		if sv.ManagedBy == p.ID {
			mine[sv.ExternalID] = sv
		}
	}
	rep.managed = len(mine)
	myGroups := map[string]store.Group{}
	for _, g := range groups {
		groupSlugs[g.Slug] = true
		if g.ManagedBy == p.ID {
			myGroups[g.ExternalID] = g
		}
	}

	// 1. Groupes voulus (seulement ceux que la source déclare).
	wantGroups := map[string]string{}
	for _, m := range machines {
		if m.GroupID != "" {
			wantGroups[m.GroupID] = m.GroupName
		}
	}
	groupID := map[string]string{} // id source -> id Leassh
	for ext, name := range wantGroups {
		if g, ok := myGroups[ext]; ok {
			groupID[ext] = g.ID
			if g.Name != name {
				if _, err := tx.ExecContext(ctx, `UPDATE server_groups SET name=? WHERE id=?`, name, g.ID); err != nil {
					return rep, nil, err
				}
				add("group", "update", name, "renommé depuis « "+g.Name+" »")
			}
			continue
		}
		g := store.Group{Slug: uniqueSlug(slugify(name), groupSlugs), Name: name, Description: "Groupe synchronisé depuis " + p.Name}
		g, err := store.InsertGroup(ctx, tx, store.ServerGroups, g)
		if err != nil {
			return rep, nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE server_groups SET managed_by=?,external_id=? WHERE id=?`, p.ID, ext, g.ID); err != nil {
			return rep, nil, err
		}
		for _, r := range p.RuleIDs { // règles par défaut: posées une fois, à la création du groupe
			if err := store.AddRuleServerGroup(ctx, tx, r, g.ID); err != nil {
				return rep, nil, err
			}
		}
		groupID[ext] = g.ID
		detail := ""
		if len(p.RuleIDs) > 0 {
			detail = fmt.Sprintf("ajouté à %d règle(s) par défaut", len(p.RuleIDs))
		}
		add("group", "create", name, detail)
	}

	// 2. Serveurs.
	seen := map[string]bool{}
	for _, m := range machines {
		seen[m.ExternalID] = true
		sv, exists := mine[m.ExternalID]
		if !exists {
			ns := store.Server{
				Slug: uniqueSlug(slugify(m.Name), serverSlugs), Name: m.Name, Host: m.Host, Port: m.Port, IdentityID: p.IdentityID,
				Description: m.Description, Tags: []string{}, Enabled: true, SSHUser: m.SSHUser, ManagedBy: p.ID, ExternalID: m.ExternalID,
			}
			if m.HostKeyFingerp != "" {
				ns.HostKeyFingerprint, ns.HostKeyStatus = m.HostKeyFingerp, "pinned"
			}
			if sv, err = store.InsertServer(ctx, tx, ns); err != nil {
				return rep, nil, err
			}
			add("server", "create", m.Name, "")
		} else {
			var diffs []string
			if sv.Name != m.Name {
				diffs = append(diffs, "nom")
			}
			if sv.Description != m.Description {
				diffs = append(diffs, "description")
			}
			if sv.Host != m.Host || sv.Port != m.Port {
				diffs = append(diffs, "hôte")
			}
			if sv.SSHUser != m.SSHUser && !sv.SSHUserCustom {
				diffs = append(diffs, "utilisateur SSH")
			}
			fpChanged := m.HostKeyFingerp != "" && sv.HostKeyFingerprint != m.HostKeyFingerp
			if fpChanged {
				diffs = append(diffs, "clé d'hôte")
			}
			if len(diffs) > 0 {
				hostChanged := sv.Host != m.Host || sv.Port != m.Port
				sv.Name, sv.Description, sv.Host, sv.Port = m.Name, m.Description, m.Host, m.Port
				if !sv.SSHUserCustom {
					sv.SSHUser = m.SSHUser
				}
				if sv, _, err = store.UpdateServer(ctx, tx, sv); err != nil {
					return rep, nil, err
				}
				if m.HostKeyFingerp != "" && (fpChanged || hostChanged) { // un divergence détectée (mismatch) n'est levée que par un nouvel épinglage de l'admin
					if err := store.SetHostKey(ctx, tx, sv.ID, m.HostKeyFingerp, "pinned"); err != nil {
						return rep, nil, err
					}
				}
				touched = append(touched, sv.ID)
				add("server", "update", m.Name, strings.Join(diffs, ", "))
			}
			if sv.SyncRemoved {
				if _, err := tx.ExecContext(ctx, `UPDATE servers SET enabled=1,sync_removed=0 WHERE id=?`, sv.ID); err != nil {
					return rep, nil, err
				}
				add("server", "enable", m.Name, "de nouveau présent dans la source")
			}
		}
		// Appartenance au groupe de la source: seule celle posée par la synchro est gérée.
		want := ""
		if m.GroupID != "" {
			want = groupID[m.GroupID]
		}
		if err := syncMembership(ctx, tx, sv.ID, want, m.Name, &rep); err != nil {
			return rep, nil, err
		}
	}

	// 3. Machines disparues de la source: désactivées, jamais supprimées (le journal y fait référence).
	for ext, sv := range mine {
		if seen[ext] {
			continue
		}
		if sv.Enabled {
			if _, err := tx.ExecContext(ctx, `UPDATE servers SET enabled=0,sync_removed=1 WHERE id=?`, sv.ID); err != nil {
				return rep, nil, err
			}
			touched = append(touched, sv.ID)
			add("server", "disable", sv.Name, "absent de la source")
		}
		if err := syncMembership(ctx, tx, sv.ID, "", sv.Name, &rep); err != nil {
			return rep, nil, err
		}
	}

	// 4. Groupes disparus: supprimés s'ils sont vides, sinon rendus à la gestion manuelle.
	for ext, g := range myGroups {
		if _, ok := wantGroups[ext]; ok {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM server_group_members WHERE group_id=?`, g.ID).Scan(&n); err != nil {
			return rep, nil, err
		}
		if n == 0 {
			if err := store.DeleteGroup(ctx, tx, store.ServerGroups, g.ID); err != nil {
				return rep, nil, err
			}
			add("group", "delete", g.Name, "plus aucun serveur")
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE server_groups SET managed_by='',external_id='' WHERE id=?`, g.ID); err != nil {
				return rep, nil, err
			}
			add("group", "release", g.Name, "garde des membres ajoutés à la main; il redevient modifiable")
		}
	}

	return rep, touched, nil
}

// syncMembership aligne l'appartenance « synchronisée » d'un serveur sur le groupe voulu (vide = aucune).
func syncMembership(ctx context.Context, tx *sql.Tx, serverID, wantGroup, name string, rep *Report) error {
	rows, err := tx.QueryContext(ctx, `SELECT group_id FROM server_group_members WHERE server_id=? AND managed=1`, serverID)
	if err != nil {
		return err
	}
	have, err := collectStrings(rows)
	if err != nil {
		return err
	}
	has := false
	for _, g := range have {
		if g == wantGroup {
			has = true
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM server_group_members WHERE group_id=? AND server_id=?`, g, serverID); err != nil {
			return err
		}
		rep.Changes = append(rep.Changes, Change{"membership", "remove", name, "retiré d'un groupe synchronisé"})
	}
	if wantGroup != "" && !has {
		if _, err := tx.ExecContext(ctx, `INSERT INTO server_group_members (group_id,server_id,managed) VALUES (?,?,1)
			ON CONFLICT(group_id,server_id) DO UPDATE SET managed=1`, wantGroup, serverID); err != nil {
			return err
		}
		rep.Changes = append(rep.Changes, Change{"membership", "add", name, "ajouté à son groupe"})
	}
	return nil
}

func collectStrings(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Run lance la synchro périodique des provisioners actifs dont l'intervalle est échu.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ps, err := store.ListProvisioners(ctx, s.st.DB)
			if err != nil {
				s.log.Error("provisioning: liste", "err", err)
				continue
			}
			for _, p := range ps {
				if !p.Enabled || p.IntervalMin <= 0 || !due(p) {
					continue
				}
				if _, err := s.Sync(ctx, p.ID, false, Actor{Type: "system", Name: "provisioner:" + p.Name}); err != nil {
					s.log.Warn("provisioning: synchro échouée", "provisioner", p.Name, "err", err)
				}
			}
		}
	}
}

func due(p store.Provisioner) bool {
	if p.LastSyncAt == "" {
		return true
	}
	last, err := store.ParseTime(p.LastSyncAt)
	return err != nil || time.Since(last) >= time.Duration(p.IntervalMin)*time.Minute
}
