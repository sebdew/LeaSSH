package packs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sebdew/LeaSSH/internal/store"
)

var ErrEmptyGroup = errors.New("le groupe ne contient aucune commande")

// InvalidPackError: le fichier YAML fourni n'est pas un pack valide.
type InvalidPackError struct{ Msg string }

func (e *InvalidPackError) Error() string { return e.Msg }

// BundledInfo décrit un pack fourni avec le binaire, pour l'écran Packs.
type BundledInfo struct {
	Meta
	CommandCount     int            `json:"command_count"`
	InstalledVersion string         `json:"installed_version"`
	Risks            map[string]int `json:"risks"`       // commandes par niveau de risque
	CommandIDs       []string       `json:"command_ids"` // pour la recherche
}

// Overview renvoie les packs installés et ceux fournis (triés), avec la version installée de chacun.
func (s *Service) Overview(ctx context.Context) ([]store.Pack, []BundledInfo, error) {
	installed, err := store.ListPacks(ctx, s.st.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("packs installés: %w", err)
	}
	bundled, err := Bundled()
	if err != nil {
		return nil, nil, fmt.Errorf("packs fournis: %w", err)
	}
	ver := map[string]string{}
	for _, p := range installed {
		ver[p.ID] = p.Version
	}
	var list []BundledInfo
	for id, f := range bundled {
		bi := BundledInfo{Meta: f.Pack, CommandCount: len(f.Commands), InstalledVersion: ver[id], Risks: map[string]int{"read": 0, "write": 0, "destructive": 0}, CommandIDs: []string{}}
		for _, c := range f.Commands {
			bi.Risks[string(c.Risk)]++
			bi.CommandIDs = append(bi.CommandIDs, c.ID)
		}
		list = append(list, bi)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return installed, list, nil
}

// Source désigne un pack: l'identifiant d'un pack fourni, ou le contenu d'un fichier YAML.
type Source struct {
	Bundled string `json:"bundled"`
	YAML    string `json:"yaml"`
}

// Load résout la source. trusted vaut vrai pour un pack fourni avec le binaire (ses avertissements sont acceptés).
func (in Source) Load() (f *File, trusted bool, err error) {
	if in.Bundled != "" {
		all, err := Bundled()
		if err != nil {
			return nil, false, fmt.Errorf("packs fournis: %w", err)
		}
		f, ok := all[in.Bundled]
		if !ok {
			return nil, false, store.ErrNotFound
		}
		return f, true, nil
	}
	f, err = Parse([]byte(in.YAML))
	if err != nil {
		return nil, false, &InvalidPackError{Msg: err.Error()}
	}
	return f, false, nil
}

// ExportRequest: ce qu'on exporte (un groupe, une liste d'identifiants, ou tout le catalogue) et les métadonnées du pack.
type ExportRequest struct {
	GroupID string
	IDs     []string
	Meta    Meta
}

// ExportFile produit le YAML et renvoie les métadonnées effectives (valeurs par défaut appliquées).
func (s *Service) ExportFile(ctx context.Context, req ExportRequest) ([]byte, Meta, error) {
	ids := req.IDs
	if req.GroupID != "" {
		grp, err := store.GroupByID(ctx, s.st.DB, store.CommandGroups, req.GroupID)
		if err != nil {
			return nil, Meta{}, err
		}
		if ids = grp.MemberIDs; len(ids) == 0 {
			return nil, Meta{}, ErrEmptyGroup
		}
	}
	meta := req.Meta
	if meta.ID == "" {
		meta.ID = "export"
	}
	if meta.Name == "" {
		meta.Name = "Export Leassh"
	}
	if meta.Version == "" {
		meta.Version = "1.0.0"
	}
	data, err := s.Export(ctx, ids, meta)
	return data, meta, err
}

// ParseIDs découpe une liste d'identifiants séparés par des virgules.
func ParseIDs(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}
