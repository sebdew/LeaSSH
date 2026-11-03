// Package packs: import / export de catalogues de commandes au format YAML (SPEC §12).
// Un pack importé est une entrée non fiable: chaque commande passe par le linter et les avertissements doivent être acceptés.
package packs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/leash-dev/leash/internal/audit"
	"github.com/leash-dev/leash/internal/catalog"
	"github.com/leash-dev/leash/internal/store"
	packsdata "github.com/leash-dev/leash/packs"
	"gopkg.in/yaml.v3"
)

const (
	maxPackBytes    = 900 << 10
	maxPackCommands = 500
)

type Meta struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description,omitempty" json:"description"`
	Category    string `yaml:"category,omitempty" json:"category"` // regroupement dans la liste des packs fournis (Système, Réseau, …)
}

type Cmd struct {
	ID          string                      `yaml:"id" json:"id"`
	Name        string                      `yaml:"name" json:"name"`
	Description string                      `yaml:"description" json:"description"`
	Risk        catalog.Risk                `yaml:"risk" json:"risk"`
	Template    string                      `yaml:"template" json:"template"`
	Params      map[string]catalog.ParamDef `yaml:"params" json:"params"`
	TimeoutS    int                         `yaml:"timeout_s,omitempty" json:"timeout_s"`
	MaxOutputKB int                         `yaml:"max_output_kb,omitempty" json:"max_output_kb"`
}

type File struct {
	Pack     Meta  `yaml:"pack" json:"pack"`
	Commands []Cmd `yaml:"commands" json:"commands"`
}

var (
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)
	verRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,31}$`)
	slug  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

	ErrInvalidPack = errors.New("pack invalide")
	ErrBlocked     = errors.New("import refusé")
)

// Parse lit un pack YAML (champs inconnus refusés, taille bornée).
func Parse(data []byte) (*File, error) {
	if len(data) > maxPackBytes {
		return nil, fmt.Errorf("%w: fichier trop volumineux", ErrInvalidPack)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPack, err)
	}
	if !idRe.MatchString(f.Pack.ID) {
		return nil, fmt.Errorf("%w: pack.id invalide (minuscules, chiffres, - _)", ErrInvalidPack)
	}
	if strings.TrimSpace(f.Pack.Name) == "" || !verRe.MatchString(f.Pack.Version) {
		return nil, fmt.Errorf("%w: pack.name et pack.version sont requis", ErrInvalidPack)
	}
	if len(f.Commands) == 0 || len(f.Commands) > maxPackCommands {
		return nil, fmt.Errorf("%w: 1 à %d commandes attendues", ErrInvalidPack, maxPackCommands)
	}
	seen := map[string]bool{}
	for i := range f.Commands {
		c := &f.Commands[i]
		if !slug.MatchString(c.ID) {
			return nil, fmt.Errorf("%w: id de commande invalide: %q", ErrInvalidPack, c.ID)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("%w: id en double: %s", ErrInvalidPack, c.ID)
		}
		seen[c.ID] = true
		if c.Params == nil {
			c.Params = map[string]catalog.ParamDef{}
		}
		if c.TimeoutS == 0 {
			c.TimeoutS = 30
		}
		if c.MaxOutputKB == 0 {
			c.MaxOutputKB = 64
		}
	}
	return &f, nil
}

// Bundled liste les packs fournis avec le binaire.
func Bundled() (map[string]*File, error) {
	out := map[string]*File{}
	entries, err := fs.ReadDir(packsdata.FS, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		b, err := packsdata.FS.ReadFile(e.Name())
		if err != nil {
			return nil, err
		}
		f, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("pack fourni %s: %w", e.Name(), err)
		}
		out[f.Pack.ID] = f
	}
	return out, nil
}

// ---- aperçu ----

type Change struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type Item struct {
	Slug            string             `json:"slug"`
	Name            string             `json:"name"`
	Risk            catalog.Risk       `json:"risk"`
	Status          string             `json:"status"` // new | modified | unchanged | invalid
	Changes         []Change           `json:"changes,omitempty"`
	Lint            catalog.LintResult `json:"lint"`
	OverridesManual bool               `json:"overrides_manual,omitempty"`
}

type Preview struct {
	Pack             Meta           `json:"pack"`
	InstalledVersion string         `json:"installed_version"`
	Items            []Item         `json:"items"`
	Counts           map[string]int `json:"counts"`
	Warnings         int            `json:"warnings"`
	Errors           int            `json:"errors"`
}

type Service struct{ st *store.Store }

func New(st *store.Store) *Service { return &Service{st: st} }

func pj(v any) string { b, _ := json.Marshal(v); return string(b) }

func diff(old store.Command, c Cmd) []Change {
	var ch []Change
	add := func(f, b, a string) {
		if b != a {
			ch = append(ch, Change{Field: f, Before: b, After: a})
		}
	}
	add("name", old.Name, c.Name)
	add("description", old.Description, c.Description)
	add("risk", string(old.Risk), string(c.Risk))
	add("template", old.Template, c.Template)
	add("params", pj(old.Params), pj(c.Params))
	add("timeout_s", fmt.Sprint(old.TimeoutS), fmt.Sprint(c.TimeoutS))
	add("max_output_kb", fmt.Sprint(old.MaxOutputKB), fmt.Sprint(c.MaxOutputKB))
	return ch
}

func (s *Service) compute(ctx context.Context, q store.Q, f *File) (Preview, error) {
	existing, err := store.ListCommands(ctx, q)
	if err != nil {
		return Preview{}, err
	}
	bySlug := map[string]store.Command{}
	for _, c := range existing {
		bySlug[c.Slug] = c
	}
	p := Preview{Pack: f.Pack, Items: []Item{}, Counts: map[string]int{"new": 0, "modified": 0, "unchanged": 0, "invalid": 0}}
	if packs, err := store.ListPacks(ctx, q); err == nil {
		for _, pk := range packs {
			if pk.ID == f.Pack.ID {
				p.InstalledVersion = pk.Version
			}
		}
	}
	for _, c := range f.Commands {
		it := Item{Slug: c.ID, Name: c.Name, Risk: c.Risk}
		it.Lint = catalog.Lint(c.Template, c.Params, c.Risk, catalog.ModeAdmin)
		if strings.TrimSpace(c.Name) == "" {
			it.Lint.Errors = append(it.Lint.Errors, catalog.Issue{Code: "name_missing", Message: "nom requis"})
		}
		if c.TimeoutS < 1 || c.TimeoutS > 600 || c.MaxOutputKB < 1 || c.MaxOutputKB > 4096 {
			it.Lint.Errors = append(it.Lint.Errors, catalog.Issue{Code: "limits", Message: "timeout_s (1-600) ou max_output_kb (1-4096) hors bornes"})
		}
		old, exists := bySlug[c.ID]
		switch {
		case len(it.Lint.Errors) > 0:
			it.Status = "invalid"
		case !exists:
			it.Status = "new"
		default:
			if it.Changes = diff(old, c); len(it.Changes) == 0 {
				it.Status = "unchanged"
			} else {
				it.Status = "modified"
				it.OverridesManual = old.Source != "pack" || old.PackID != f.Pack.ID
			}
		}
		p.Counts[it.Status]++
		p.Errors += len(it.Lint.Errors)
		p.Warnings += len(it.Lint.Warnings)
		p.Items = append(p.Items, it)
	}
	return p, nil
}

// Preview montre ce que l'import ferait, sans rien écrire.
func (s *Service) Preview(ctx context.Context, f *File) (Preview, error) {
	return s.compute(ctx, s.st.DB, f)
}

// ---- import ----

type Options struct {
	CreateGroup   bool
	AllowWarnings bool
}

type Result struct {
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Unchanged int    `json:"unchanged"`
	GroupID   string `json:"group_id,omitempty"`
}

// Import applique le pack de façon atomique. Refusé s'il contient une commande invalide, ou des avertissements non acceptés.
func (s *Service) Import(ctx context.Context, user store.User, f *File, ip string, opt Options) (Result, error) {
	var res Result
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		prev, err := s.compute(ctx, tx, f)
		if err != nil {
			return err
		}
		if prev.Errors > 0 {
			return fmt.Errorf("%w: %d erreur(s) de validation", ErrBlocked, prev.Errors)
		}
		if prev.Warnings > 0 && !opt.AllowWarnings {
			return fmt.Errorf("%w: %d avertissement(s) à examiner et accepter", ErrBlocked, prev.Warnings)
		}
		existing, err := store.ListCommands(ctx, tx)
		if err != nil {
			return err
		}
		bySlug := map[string]store.Command{}
		for _, c := range existing {
			bySlug[c.Slug] = c
		}
		var memberIDs []string
		var slugs []string
		for i, it := range prev.Items {
			c := f.Commands[i]
			cmd := store.Command{Slug: c.ID, Name: c.Name, Description: c.Description, Risk: c.Risk, Template: c.Template, Params: c.Params,
				TimeoutS: c.TimeoutS, MaxOutputKB: c.MaxOutputKB}
			switch it.Status {
			case "new":
				cmd.Status, cmd.Source, cmd.PackID, cmd.CreatedBy, cmd.ApprovedBy = "enabled", "pack", f.Pack.ID, user.ID, user.ID
				if cmd, err = store.InsertCommand(ctx, tx, cmd); err != nil {
					return err
				}
				res.Created++
				slugs = append(slugs, c.ID)
			case "modified":
				old := bySlug[c.ID]
				cmd.ID, cmd.Status = old.ID, old.Status // un statut désactivé volontairement est conservé
				if cmd, _, err = store.UpdateCommand(ctx, tx, cmd); err != nil {
					return err
				}
				if err = store.MarkCommandFromPack(ctx, tx, cmd.ID, f.Pack.ID); err != nil {
					return err
				}
				res.Updated++
				slugs = append(slugs, c.ID)
			default:
				cmd = bySlug[c.ID]
				res.Unchanged++
			}
			memberIDs = append(memberIDs, cmd.ID)
		}
		if opt.CreateGroup {
			if res.GroupID, err = ensureGroup(ctx, tx, f.Pack, memberIDs); err != nil {
				return err
			}
		}
		if err := store.UpsertPack(ctx, tx, store.Pack{ID: f.Pack.ID, Name: f.Pack.Name, Version: f.Pack.Version, Description: strings.TrimSpace(f.Pack.Description), InstalledBy: user.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: user.ID, ActorName: user.Username, Action: "pack.import", TargetType: "pack", TargetID: f.Pack.ID, TargetName: f.Pack.Name, IP: ip,
			Diff: map[string]any{"version": f.Pack.Version, "previous_version": prev.InstalledVersion, "created": res.Created, "updated": res.Updated, "unchanged": res.Unchanged, "changed_commands": slugs, "group_created": opt.CreateGroup}})
	})
	return res, err
}

func ensureGroup(ctx context.Context, tx store.Q, pk Meta, ids []string) (string, error) {
	groups, err := store.ListGroups(ctx, tx, store.CommandGroups)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Slug == pk.ID {
			return g.ID, store.SetGroupMembers(ctx, tx, store.CommandGroups, g.ID, append(g.MemberIDs, ids...))
		}
	}
	g, err := store.InsertGroup(ctx, tx, store.CommandGroups, store.Group{Slug: pk.ID, Name: pk.Name, Description: strings.TrimSpace(pk.Description), MemberIDs: ids})
	return g.ID, err
}

// ---- export ----

// Export produit un pack YAML à partir de commandes (ids). ids vide = tout le catalogue.
func (s *Service) Export(ctx context.Context, ids []string, meta Meta) ([]byte, error) {
	cmds, err := store.ListCommands(ctx, s.st.DB)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	f := File{Pack: meta}
	for _, c := range cmds {
		if len(want) > 0 && !want[c.ID] {
			continue
		}
		f.Commands = append(f.Commands, Cmd{ID: c.Slug, Name: c.Name, Description: c.Description, Risk: c.Risk, Template: c.Template,
			Params: c.Params, TimeoutS: c.TimeoutS, MaxOutputKB: c.MaxOutputKB})
	}
	if len(f.Commands) == 0 {
		return nil, fmt.Errorf("%w: aucune commande à exporter", store.ErrNotFound)
	}
	sort.Slice(f.Commands, func(i, j int) bool { return f.Commands[i].ID < f.Commands[j].ID })
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("encodage YAML: %w", err)
	}
	return buf.Bytes(), enc.Close()
}
