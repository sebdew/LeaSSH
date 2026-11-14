package packs

import (
	"context"
	"errors"
	"testing"

	"github.com/sebdew/LeaSSH/internal/store"
)

func TestOverviewListsBundledWithInstalledVersion(t *testing.T) {
	svc, _, u := setup(t)
	ctx := context.Background()
	installed, bundled, err := svc.Overview(ctx)
	if err != nil || len(installed) != 0 || len(bundled) == 0 {
		t.Fatalf("avant import: %d installés, %d fournis, %v", len(installed), len(bundled), err)
	}
	for i := 1; i < len(bundled); i++ {
		if bundled[i-1].ID >= bundled[i].ID {
			t.Fatal("les packs fournis doivent être triés")
		}
	}
	all, _ := Bundled()
	first := bundled[0]
	if _, err := svc.Import(ctx, u, all[first.ID], "", Options{AllowWarnings: true}); err != nil {
		t.Fatal(err)
	}
	_, bundled, _ = svc.Overview(ctx)
	if bundled[0].InstalledVersion != first.Version || bundled[0].CommandCount == 0 || len(bundled[0].CommandIDs) != bundled[0].CommandCount {
		t.Fatalf("version installée / compteurs: %+v", bundled[0])
	}
}

func TestSourceLoad(t *testing.T) {
	all, _ := Bundled()
	var id string
	for k := range all {
		id = k
		break
	}
	if f, trusted, err := (Source{Bundled: id}).Load(); err != nil || f == nil || !trusted {
		t.Fatalf("pack fourni: %v %v", trusted, err)
	}
	if _, _, err := (Source{Bundled: "absent"}).Load(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pack fourni inconnu: %v", err)
	}
	var ip *InvalidPackError
	if _, _, err := (Source{YAML: "pas: un pack"}).Load(); !errors.As(err, &ip) {
		t.Fatalf("YAML invalide: %v", err)
	}
	if f, trusted, err := (Source{YAML: simple}).Load(); err != nil || trusted || f == nil {
		t.Fatalf("YAML valide: trusted=%v %v", trusted, err)
	}
}

func TestExportFileDefaultsAndGroup(t *testing.T) {
	svc, st, u := setup(t)
	ctx := context.Background()
	all, _ := Bundled()
	for _, f := range all {
		if _, err := svc.Import(ctx, u, f, "", Options{AllowWarnings: true, CreateGroup: true}); err != nil {
			t.Fatal(err)
		}
	}
	data, meta, err := svc.ExportFile(ctx, ExportRequest{})
	if err != nil || len(data) == 0 || meta.ID != "export" || meta.Name != "Export Leassh" || meta.Version != "1.0.0" {
		t.Fatalf("valeurs par défaut: %+v %v", meta, err)
	}
	groups, _ := store.ListGroups(ctx, st.DB, store.CommandGroups)
	if len(groups) == 0 {
		t.Fatal("un groupe devait être créé")
	}
	if _, _, err := svc.ExportFile(ctx, ExportRequest{GroupID: groups[0].ID, Meta: Meta{ID: "g"}}); err != nil {
		t.Fatalf("export d'un groupe: %v", err)
	}
	if _, _, err := svc.ExportFile(ctx, ExportRequest{GroupID: "absent"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("groupe inconnu: %v", err)
	}
}

func TestParseIDs(t *testing.T) {
	if ParseIDs("") != nil || len(ParseIDs("a,b")) != 2 {
		t.Fatal("découpage des identifiants")
	}
}
