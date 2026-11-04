// Package auditreport produit des rapports d'audit pour un auditeur (ISO 27001, HDS).
//
// Principe: les FAITS (inventaire, activité, intégrité, changements, constats, correspondance aux contrôles) sont calculés de façon
// déterministe à partir de la base; aucune IA n'y intervient. Une synthèse rédigée par une IA est possible, mais facultative,
// stockée à part, hors de l'empreinte, et n'envoie que des agrégats (voir ai.go).
package auditreport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/leassh-dev/leassh/internal/audit"
	"github.com/leassh-dev/leassh/internal/store"
)

const (
	SchemaVersion = 1
	Kind          = "leash-audit-report"
	maxListed     = 1000 // événements d'administration listés dans le rapport (le total est toujours exact)
	unusedDays    = 90
)

// Params: ce que le rapport sait de l'instance qui le génère.
type Params struct {
	Now           time.Time
	From, To      time.Time // jours UTC inclus
	LeasshVersion  string
	RetentionDays int
	ApprovalTTL   time.Duration
	SessionTTL    time.Duration
	BaseURL       string
}

type Period struct {
	From   string `json:"from"` // AAAA-MM-JJ
	To     string `json:"to"`
	FromTS string `json:"from_ts"`
	ToTS   string `json:"to_ts"`
	Days   int    `json:"days"`
}

type ConfigFacts struct {
	RetentionDays   int    `json:"log_retention_days"`
	ApprovalTTLMin  int    `json:"approval_ttl_minutes"`
	SessionTTLHours int    `json:"session_ttl_hours"`
	BaseURL         string `json:"base_url"`
	BaseURLHTTPS    bool   `json:"base_url_https"`
	WebhookEnabled  bool   `json:"webhook_enabled"`
}

type ServerItem struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	HostKey    string `json:"host_key_status"`
	Synced     bool   `json:"synced"`
	LastSSHOK  string `json:"last_ssh_ok_at"`
	Identity   string `json:"identity"`
	AuthMethod string `json:"auth_method"`
}

type ServersFacts struct {
	Total    int          `json:"total"`
	Enabled  int          `json:"enabled"`
	Pinned   int          `json:"pinned"`
	Unknown  int          `json:"unknown"`
	Mismatch int          `json:"mismatch"`
	Synced   int          `json:"synced"`
	Items    []ServerItem `json:"items"`
}

type GroupItem struct {
	Name    string `json:"name"`
	Members int    `json:"members"`
	Synced  bool   `json:"synced"`
}

type GroupsFacts struct {
	ServerGroups  []GroupItem `json:"server_groups"`
	CommandGroups []GroupItem `json:"command_groups"`
}

type CommandItem struct {
	Slug       string `json:"slug"`
	Risk       string `json:"risk"`
	Source     string `json:"source"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
	ApprovedBy string `json:"approved_by"`
	ApprovedAt string `json:"approved_at"`
}

type CommandsFacts struct {
	Total      int            `json:"total"`
	ByRisk     map[string]int `json:"by_risk"`
	BySource   map[string]int `json:"by_source"`
	ByStatus   map[string]int `json:"by_status"`
	Privileged []CommandItem  `json:"privileged"` // commandes d'écriture et destructives
}

type KeyItem struct {
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	State      string   `json:"state"` // active | disabled | expired
	CreatedAt  string   `json:"created_at"`
	ExpiresAt  string   `json:"expires_at"`
	LastUsedAt string   `json:"last_used_at"`
	UseCount   int64    `json:"use_count"`
	CanPropose bool     `json:"can_propose"`
	Rules      []string `json:"rules"`
}

type KeysFacts struct {
	Total      int       `json:"total"`
	Active     int       `json:"active"`
	Disabled   int       `json:"disabled"`
	Expired    int       `json:"expired"`
	NoExpiry   int       `json:"active_without_expiry"`
	Unused     int       `json:"active_unused_over_90d"`
	CanPropose int       `json:"can_propose"`
	Items      []KeyItem `json:"items"`
}

type RuleItem struct {
	Name          string   `json:"name"`
	Profile       string   `json:"profile"`
	Approval      bool     `json:"write_requires_approval"`
	Enabled       bool     `json:"enabled"`
	ServerGroups  []string `json:"server_groups"`
	CommandGroups []string `json:"command_groups"`
	Keys          int      `json:"keys"`
}

type RulesFacts struct {
	Total        int        `json:"total"`
	Enabled      int        `json:"enabled"`
	ReadOnly     int        `json:"read_only"`
	ReadWrite    int        `json:"read_write"`
	RWNoApproval int        `json:"read_write_without_approval"`
	Items        []RuleItem `json:"items"`
}

type UserItem struct {
	Username    string `json:"username"`
	Role        string `json:"role"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"created_at"`
	LastLoginAt string `json:"last_login_at"`
}

type UsersFacts struct {
	Total    int        `json:"total"`
	Admins   int        `json:"admins_enabled"`
	Disabled int        `json:"disabled"`
	Items    []UserItem `json:"items"`
}

type IdentitiesFacts struct {
	Total    int `json:"total"`
	KeyAuth  int `json:"key_auth"`
	Password int `json:"password_auth"`
}

type ProvisionerItem struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Enabled    bool     `json:"enabled"`
	IntervalM  int      `json:"interval_min"`
	LastStatus string   `json:"last_sync_status"`
	LastSyncAt string   `json:"last_sync_at"`
	Servers    int      `json:"servers"`
	Rules      []string `json:"default_rules"`
}

type Scope struct {
	Servers      ServersFacts      `json:"servers"`
	Groups       GroupsFacts       `json:"groups"`
	Commands     CommandsFacts     `json:"commands"`
	Keys         KeysFacts         `json:"keys"`
	Rules        RulesFacts        `json:"rules"`
	Users        UsersFacts        `json:"users"`
	Identities   IdentitiesFacts   `json:"identities"`
	Provisioners []ProvisionerItem `json:"provisioners"`
}

type Activity struct {
	Requests        int                     `json:"requests"`
	Succeeded       int                     `json:"succeeded"`
	Failed          int                     `json:"failed"` // failed + timeout + error
	Denied          int                     `json:"denied"`
	ByStatus        []store.NameCount       `json:"by_status"`
	ByRisk          []store.NameCount       `json:"by_risk"`
	DenialReasons   []store.NameCount       `json:"denial_reasons"`
	ByKey           []store.NameCount       `json:"by_key"`
	FailedByCommand []store.NameCount       `json:"failed_by_command"`
	DistinctKeys    int                     `json:"distinct_keys"`
	DistinctServers int                     `json:"distinct_servers"`
	Approvals       Approvals               `json:"approvals"`
	WriteNoApproval int                     `json:"write_executed_without_approval"`
	DestructiveTot  int                     `json:"destructive_total"`
	Destructive     []store.DestructiveExec `json:"destructive"`
	Security        []store.NameCount       `json:"security_events"` // échecs de connexion, jetons refusés, divergences de clé d'hôte
}

type Approvals struct {
	Approved       int               `json:"approved"`
	Rejected       int               `json:"rejected"`
	Expired        int               `json:"expired"`
	AvgDelaySec    int64             `json:"avg_delay_seconds"`
	MaxDelaySec    int64             `json:"max_delay_seconds"`
	Deciders       []store.NameCount `json:"deciders"`
	ApprovalTTLMin int               `json:"approval_ttl_minutes"`
}

type ChainFacts struct {
	Info     store.ChainInfo   `json:"chain"`
	Verified store.ChainResult `json:"verification"`
}

type Integrity struct {
	Executions     ChainFacts `json:"executions"`
	Admin          ChainFacts `json:"admin_journal"`
	RetentionDays  int        `json:"retention_days"`
	PurgeEvents    int        `json:"purge_events_in_period"`
	VerifiedAt     string     `json:"verified_at"`
	Method         string     `json:"method"`
	KnownLimitNote string     `json:"known_limit"`
}

type Changes struct {
	Total     int                `json:"total"`
	ByAction  []store.NameCount  `json:"by_action"`
	Listed    []store.AdminEvent `json:"listed"`
	Truncated bool               `json:"truncated"`
}

type Snapshot struct {
	Kind          string      `json:"kind"`
	SchemaVersion int         `json:"schema_version"`
	GeneratedAt   string      `json:"generated_at"`
	LeasshVersion  string      `json:"leash_version"`
	Period        Period      `json:"period"`
	Config        ConfigFacts `json:"configuration"`
	Scope         Scope       `json:"scope"`
	Activity      Activity    `json:"activity"`
	Integrity     Integrity   `json:"integrity"`
	Changes       Changes     `json:"changes"`
	Findings      []Finding   `json:"findings"`
	Controls      []Control   `json:"controls"`
	Limitations   []string    `json:"limitations"`
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

// PeriodBounds convertit deux jours UTC inclus en bornes d'horodatage.
func PeriodBounds(from, to time.Time) Period {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 23, 59, 59, 999_000_000, time.UTC)
	return Period{From: day(f), To: day(t), FromTS: store.FormatTime(f), ToTS: store.FormatTime(t), Days: int(t.Sub(f).Hours()/24) + 1}
}

func sortedNames(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func countBy[T any](items []T, key func(T) string) map[string]int {
	m := map[string]int{}
	for _, it := range items {
		m[key(it)]++
	}
	return m
}

// Build calcule l'instantané. q peut être une transaction: toutes les lectures passent par elle.
func Build(ctx context.Context, q store.Q, p Params) (Snapshot, error) {
	now := p.Now.UTC()
	per := PeriodBounds(p.From, p.To)
	snap := Snapshot{Kind: Kind, SchemaVersion: SchemaVersion, GeneratedAt: store.FormatTime(now), LeasshVersion: p.LeasshVersion, Period: per, Limitations: limitations()}

	whEnabled, _ := store.GetSetting(ctx, q, "webhook.enabled")
	snap.Config = ConfigFacts{RetentionDays: p.RetentionDays, ApprovalTTLMin: int(p.ApprovalTTL.Minutes()), SessionTTLHours: int(p.SessionTTL.Hours()), BaseURL: p.BaseURL,
		BaseURLHTTPS: strings.HasPrefix(strings.ToLower(p.BaseURL), "https://"), WebhookEnabled: whEnabled == "1" || whEnabled == "true"}

	if err := buildScope(ctx, q, now, &snap); err != nil {
		return snap, err
	}
	if err := buildActivity(ctx, q, per, p, &snap); err != nil {
		return snap, err
	}
	if err := buildIntegrity(ctx, q, now, p, &snap); err != nil {
		return snap, err
	}
	if err := buildChanges(ctx, q, per, &snap); err != nil {
		return snap, err
	}
	snap.Findings = Evaluate(snap)
	snap.Controls = MapControls(snap)
	return snap, nil
}

func buildScope(ctx context.Context, q store.Q, now time.Time, snap *Snapshot) error {
	servers, err := store.ListServers(ctx, q)
	if err != nil {
		return err
	}
	idents, err := store.ListIdentities(ctx, q)
	if err != nil {
		return err
	}
	identByID := map[string]store.Identity{}
	for _, i := range idents {
		identByID[i.ID] = i
		snap.Scope.Identities.Total++
		if i.AuthType == "password" {
			snap.Scope.Identities.Password++
		} else {
			snap.Scope.Identities.KeyAuth++
		}
	}
	sv := &snap.Scope.Servers
	sv.Items = []ServerItem{}
	for _, s := range servers {
		sv.Total++
		if s.Enabled {
			sv.Enabled++
		}
		switch s.HostKeyStatus {
		case "pinned":
			sv.Pinned++
		case "mismatch":
			sv.Mismatch++
		default:
			sv.Unknown++
		}
		if s.ManagedBy != "" {
			sv.Synced++
		}
		id := identByID[s.IdentityID]
		sv.Items = append(sv.Items, ServerItem{Slug: s.Slug, Name: s.Name, Enabled: s.Enabled, HostKey: s.HostKeyStatus, Synced: s.ManagedBy != "", Identity: id.Name, AuthMethod: id.AuthType})
	}

	sgs, err := store.ListGroups(ctx, q, store.ServerGroups)
	if err != nil {
		return err
	}
	cgs, err := store.ListGroups(ctx, q, store.CommandGroups)
	if err != nil {
		return err
	}
	gname := map[string]string{}
	snap.Scope.Groups.ServerGroups, snap.Scope.Groups.CommandGroups = []GroupItem{}, []GroupItem{}
	for _, g := range sgs {
		gname[g.ID] = g.Name
		snap.Scope.Groups.ServerGroups = append(snap.Scope.Groups.ServerGroups, GroupItem{Name: g.Name, Members: len(g.MemberIDs), Synced: g.ManagedBy != ""})
	}
	for _, g := range cgs {
		gname[g.ID] = g.Name
		snap.Scope.Groups.CommandGroups = append(snap.Scope.Groups.CommandGroups, GroupItem{Name: g.Name, Members: len(g.MemberIDs)})
	}

	users, err := store.ListUsers(ctx, q)
	if err != nil {
		return err
	}
	userName := map[string]string{}
	us := &snap.Scope.Users
	us.Items = []UserItem{}
	for _, u := range users {
		userName[u.ID] = u.Username
		us.Total++
		if u.Disabled {
			us.Disabled++
		} else if u.Role == "admin" {
			us.Admins++
		}
		us.Items = append(us.Items, UserItem{Username: u.Username, Role: u.Role, Disabled: u.Disabled, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt})
	}

	cmds, err := store.ListCommands(ctx, q)
	if err != nil {
		return err
	}
	cf := &snap.Scope.Commands
	cf.Total = len(cmds)
	cf.ByRisk = countBy(cmds, func(c store.Command) string { return string(c.Risk) })
	cf.BySource = countBy(cmds, func(c store.Command) string { return c.Source })
	cf.ByStatus = countBy(cmds, func(c store.Command) string { return c.Status })
	cf.Privileged = []CommandItem{}
	for _, c := range cmds {
		if c.Risk == "write" || c.Risk == "destructive" {
			by := userName[c.ApprovedBy]
			if by == "" {
				by = c.ApprovedBy
			}
			cf.Privileged = append(cf.Privileged, CommandItem{Slug: c.Slug, Risk: string(c.Risk), Source: c.Source, Status: c.Status, Version: c.Version, ApprovedBy: by, ApprovedAt: c.ApprovedAt})
		}
	}
	sort.Slice(cf.Privileged, func(i, j int) bool { return cf.Privileged[i].Slug < cf.Privileged[j].Slug })

	rules, err := store.ListRules(ctx, q)
	if err != nil {
		return err
	}
	ruleName := map[string]string{}
	rf := &snap.Scope.Rules
	rf.Items = []RuleItem{}
	for _, r := range rules {
		ruleName[r.ID] = r.Name
		rf.Total++
		if r.Enabled {
			rf.Enabled++
		}
		if r.Profile == "RW" {
			rf.ReadWrite++
			if !r.WriteRequiresApproval && r.Enabled {
				rf.RWNoApproval++
			}
		} else {
			rf.ReadOnly++
		}
		it := RuleItem{Name: r.Name, Profile: r.Profile, Approval: r.WriteRequiresApproval, Enabled: r.Enabled, Keys: len(r.KeyIDs), ServerGroups: []string{}, CommandGroups: []string{}}
		for _, id := range r.ServerGroupIDs {
			it.ServerGroups = append(it.ServerGroups, gname[id])
		}
		for _, id := range r.CommandGroupIDs {
			it.CommandGroups = append(it.CommandGroups, gname[id])
		}
		it.ServerGroups, it.CommandGroups = sortedNames(it.ServerGroups), sortedNames(it.CommandGroups)
		rf.Items = append(rf.Items, it)
	}

	keys, err := store.ListKeys(ctx, q)
	if err != nil {
		return err
	}
	kf := &snap.Scope.Keys
	kf.Items = []KeyItem{}
	nowS := store.FormatTime(now)
	cutoff := store.FormatTime(now.AddDate(0, 0, -unusedDays))
	for _, k := range keys {
		kf.Total++
		state := "active"
		switch {
		case k.Disabled:
			state = "disabled"
			kf.Disabled++
		case k.ExpiresAt != "" && k.ExpiresAt < nowS:
			state = "expired"
			kf.Expired++
		default:
			kf.Active++
			if k.ExpiresAt == "" {
				kf.NoExpiry++
			}
			if (k.LastUsedAt == "" && k.CreatedAt < cutoff) || (k.LastUsedAt != "" && k.LastUsedAt < cutoff) {
				kf.Unused++
			}
			if k.CanPropose {
				kf.CanPropose++
			}
		}
		rn := []string{}
		for _, id := range k.RuleIDs {
			rn = append(rn, ruleName[id])
		}
		kf.Items = append(kf.Items, KeyItem{Name: k.Name, Prefix: k.Prefix, State: state, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, UseCount: k.UseCount, CanPropose: k.CanPropose, Rules: sortedNames(rn)})
	}

	provs, err := store.ListProvisioners(ctx, q)
	if err != nil {
		return err
	}
	snap.Scope.Provisioners = []ProvisionerItem{}
	for _, pv := range provs {
		it := ProvisionerItem{Name: pv.Name, Type: pv.Type, Enabled: pv.Enabled, IntervalM: pv.IntervalMin, LastStatus: pv.LastSyncStatus, LastSyncAt: pv.LastSyncAt, Rules: []string{}}
		for _, s := range servers {
			if s.ManagedBy == pv.ID {
				it.Servers++
			}
		}
		for _, id := range pv.RuleIDs {
			it.Rules = append(it.Rules, ruleName[id])
		}
		it.Rules = sortedNames(it.Rules)
		snap.Scope.Provisioners = append(snap.Scope.Provisioners, it)
	}
	return nil
}

func buildActivity(ctx context.Context, q store.Q, per Period, p Params, snap *Snapshot) error {
	pa, err := store.GetPeriodActivity(ctx, q, per.FromTS, per.ToTS)
	if err != nil {
		return err
	}
	a := &snap.Activity
	a.Requests, a.ByStatus, a.ByRisk, a.DenialReasons, a.ByKey, a.FailedByCommand = pa.Requests, pa.ByStatus, pa.ByRisk, pa.DenialReasons, pa.ByKey, pa.FailedByCommand
	for _, s := range pa.ByStatus {
		switch s.Name {
		case "succeeded":
			a.Succeeded += s.Count
		case "failed", "timeout", "error":
			a.Failed += s.Count
		case "denied":
			a.Denied += s.Count
		}
	}
	a.DistinctKeys, a.DistinctServers = pa.DistinctKeys, pa.DistinctServers
	a.Approvals = Approvals{Approved: pa.Approved, Rejected: pa.Rejected, Expired: pa.Expired, AvgDelaySec: pa.AvgApprovalSec, MaxDelaySec: pa.MaxApprovalSec, Deciders: pa.Deciders, ApprovalTTLMin: int(p.ApprovalTTL.Minutes())}
	a.WriteNoApproval, a.DestructiveTot, a.Destructive = pa.WriteNoApproval, pa.DestructiveTotal, pa.Destructive

	counts, err := store.AdminActionCounts(ctx, q, per.FromTS, per.ToTS)
	if err != nil {
		return err
	}
	a.Security = []store.NameCount{}
	for _, c := range counts {
		switch c.Name {
		case "user.login_failed", "key.auth_failed", "host_key.mismatch":
			a.Security = append(a.Security, c)
		}
	}
	return nil
}

func buildIntegrity(ctx context.Context, q store.Q, now time.Time, p Params, snap *Snapshot) error {
	in := &snap.Integrity
	var err error
	if in.Executions.Info, err = store.GetChainInfo(ctx, q, "executions"); err != nil {
		return err
	}
	if in.Executions.Verified, err = store.VerifyExecutions(ctx, q); err != nil {
		return err
	}
	if in.Admin.Info, err = store.GetChainInfo(ctx, q, "audit_events"); err != nil {
		return err
	}
	if in.Admin.Verified, err = audit.Verify(ctx, q); err != nil {
		return err
	}
	in.RetentionDays = p.RetentionDays
	in.VerifiedAt = store.FormatTime(now)
	in.Method = "Chaque ligne porte le hash SHA-256 de son contenu et de la ligne précédente; la vérification recalcule toute la chaîne (altération, suppression au milieu, trou de séquence)."
	in.KnownLimitNote = "La première ligne conservée sert d'ancre (la rétention purge les plus anciennes) et la troncature de la fin de chaîne n'est pas détectable sans ancrage externe: conservez la tête de chaîne (head_hash) de chaque rapport."
	counts, err := store.AdminActionCounts(ctx, q, snap.Period.FromTS, snap.Period.ToTS)
	if err != nil {
		return err
	}
	for _, c := range counts {
		if c.Name == "logs.purge" {
			in.PurgeEvents = c.Count
		}
	}
	return nil
}

func buildChanges(ctx context.Context, q store.Q, per Period, snap *Snapshot) error {
	by, err := store.AdminActionCounts(ctx, q, per.FromTS, per.ToTS)
	if err != nil {
		return err
	}
	total := 0
	for _, c := range by {
		total += c.Count
	}
	list, err := store.ListAdminEventsBetween(ctx, q, per.FromTS, per.ToTS, maxListed)
	if err != nil {
		return err
	}
	snap.Changes = Changes{Total: total, ByAction: by, Listed: list, Truncated: total > len(list)}
	return nil
}

func limitations() []string {
	return []string{
		"Ce rapport est une aide à l'audit: il ne constitue ni une certification, ni une attestation de conformité ISO 27001 ou HDS.",
		"Il ne couvre que ce que LeaSSH peut observer: accès SSH médiés par LeaSSH. Les accès directs aux serveurs, le système d'exploitation hôte et l'hébergement de LeaSSH n'en font pas partie.",
		"L'inventaire (périmètre, clés, règles, utilisateurs) est celui du moment de la génération; l'activité et les changements sont ceux de la période choisie.",
		"La troncature de la fin d'un journal n'est pas détectable sans ancrage externe de la tête de chaîne.",
		"Les seuils des constats (90 jours d'inactivité, rétention de 365 jours) sont des repères indicatifs, à adapter à votre politique.",
	}
}

// Marshal produit le JSON compact, déterministe et sans échappement HTML, dont l'empreinte sert de signature.
func Marshal(s Snapshot) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("sérialisation du rapport: %w", err)
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// Digest: SHA-256 hexadécimal du JSON compact.
func Digest(compact []byte) string {
	h := sha256.Sum256(compact)
	return hex.EncodeToString(h[:])
}
