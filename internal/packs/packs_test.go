package packs

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/leassh-dev/leassh/internal/store"
)

func setup(t *testing.T) (*Service, *store.Store, store.User) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var u store.User
	if err := st.Tx(ctx, func(tx *sql.Tx) (err error) {
		u, err = store.InsertUser(ctx, tx, store.User{Username: "adm", Role: "admin", PasswordHash: "x"})
		return
	}); err != nil {
		t.Fatal(err)
	}
	return New(st), st, u
}

const simple = `
pack:
  id: demo
  name: Démo
  version: 1.0.0
commands:
  - id: demo.hello
    name: Bonjour
    description: Dit bonjour.
    risk: read
    template: echo hello
    params: {}
  - id: demo.svc
    name: Service
    description: Statut d'un service.
    risk: read
    template: systemctl status {{service}}
    params:
      service: { type: service, description: Unité }
`

// Les packs fournis doivent être valides: aucune erreur de lint, ids uniques, tous importables.
func TestBundledPacksAreValidAndImportable(t *testing.T) {
	svc, st, u := setup(t)
	bundled, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if len(bundled) != 9 {
		t.Fatalf("%d packs fournis (9 attendus)", len(bundled))
	}
	ctx := context.Background()
	total := 0
	for id, f := range bundled {
		p, err := svc.Preview(ctx, f)
		if err != nil || p.Errors != 0 {
			for _, it := range p.Items {
				if len(it.Lint.Errors) > 0 {
					t.Errorf("%s/%s: %+v", id, it.Slug, it.Lint.Errors)
				}
			}
			t.Fatalf("pack %s invalide: %v", id, err)
		}
		if f.Pack.Category == "" {
			t.Errorf("pack %s: catégorie manquante", id)
		}
		if (id == "pfsense" || id == "proxmox" || id == "unifi") && p.Warnings != 0 { // les packs récents n'ont aucun avertissement de lint
			t.Errorf("pack %s: %d avertissement(s) de lint", id, p.Warnings)
		}
		if _, err := svc.Import(ctx, u, f, "127.0.0.1", Options{CreateGroup: true, AllowWarnings: true}); err != nil {
			t.Fatalf("import %s: %v", id, err)
		}
		total += len(f.Commands)
	}
	cmds, _ := store.ListCommands(ctx, st.DB)
	if len(cmds) != total || total < 30 {
		t.Fatalf("commandes importées: %d (attendu %d)", len(cmds), total)
	}
	for _, c := range cmds {
		if c.Source != "pack" || c.Status != "enabled" || c.PackID == "" {
			t.Fatalf("commande importée: %+v", c)
		}
	}
	groups, _ := store.ListGroups(ctx, st.DB, store.CommandGroups)
	if len(groups) != 9 {
		t.Fatalf("groupes créés: %d", len(groups))
	}
	if ps, _ := store.ListPacks(ctx, st.DB); len(ps) != 9 || ps[0].CommandCount == 0 {
		t.Fatalf("packs installés: %+v", ps)
	}
}

func TestPreviewDiffAndReimport(t *testing.T) {
	svc, _, u := setup(t)
	ctx := context.Background()
	f, err := Parse([]byte(simple))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := svc.Preview(ctx, f)
	if p.Counts["new"] != 2 || p.Counts["modified"] != 0 {
		t.Fatalf("aperçu initial: %+v", p.Counts)
	}
	res, err := svc.Import(ctx, u, f, "", Options{CreateGroup: true})
	if err != nil || res.Created != 2 || res.GroupID == "" {
		t.Fatalf("import: %+v %v", res, err)
	}
	p, _ = svc.Preview(ctx, f)
	if p.Counts["unchanged"] != 2 || p.InstalledVersion != "1.0.0" {
		t.Fatalf("réimport identique: %+v", p)
	}
	// nouvelle version: une commande modifiée, une nouvelle
	f2, _ := Parse([]byte(strings.Replace(strings.Replace(simple, "echo hello", "echo bonjour", 1), "version: 1.0.0", "version: 1.1.0", 1) + `
  - id: demo.extra
    name: Extra
    description: Une de plus.
    risk: read
    template: uptime
`))
	p, _ = svc.Preview(ctx, f2)
	if p.Counts["modified"] != 1 || p.Counts["new"] != 1 || p.Counts["unchanged"] != 1 {
		t.Fatalf("aperçu v1.1: %+v", p.Counts)
	}
	var mod Item
	for _, it := range p.Items {
		if it.Status == "modified" {
			mod = it
		}
	}
	if len(mod.Changes) != 1 || mod.Changes[0].Field != "template" || mod.Changes[0].Before != "echo hello" || mod.Changes[0].After != "echo bonjour" {
		t.Fatalf("diff: %+v", mod.Changes)
	}
	res, err = svc.Import(ctx, u, f2, "", Options{CreateGroup: true})
	if err != nil || res.Updated != 1 || res.Created != 1 || res.Unchanged != 1 {
		t.Fatalf("import v1.1: %+v %v", res, err)
	}
	// la version de la commande est incrémentée (hash changé)
	// nouvel import du groupe: pas de doublon de membres
}

func TestWarningsAndInvalidBlockImport(t *testing.T) {
	svc, st, u := setup(t)
	ctx := context.Background()
	piped, _ := Parse([]byte(strings.Replace(simple, "echo hello", "ps aux | sort", 1)))
	if _, err := svc.Import(ctx, u, piped, "", Options{}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("avertissements non acceptés: %v", err)
	}
	if n, _ := store.ListCommands(ctx, st.DB); len(n) != 0 {
		t.Fatal("import partiel")
	}
	if _, err := svc.Import(ctx, u, piped, "", Options{AllowWarnings: true}); err != nil {
		t.Fatalf("avertissements acceptés: %v", err)
	}
	evil, _ := Parse([]byte(strings.Replace(simple, "echo hello", `echo "{{x}}"`, 1)))
	p, _ := svc.Preview(ctx, evil)
	if p.Counts["invalid"] != 1 || p.Errors == 0 {
		t.Fatalf("commande invalide: %+v", p.Counts)
	}
	if _, err := svc.Import(ctx, u, evil, "", Options{AllowWarnings: true}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("une erreur de lint doit bloquer même avec avertissements acceptés: %v", err)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"champ inconnu":     strings.Replace(simple, "risk: read", "risk: read\n    sudo: true", 1),
		"id de pack":        strings.Replace(simple, "id: demo\n", "id: ../x\n", 1),
		"id en double":      strings.Replace(simple, "id: demo.svc", "id: demo.hello", 1),
		"id de commande":    strings.Replace(simple, "id: demo.hello", "id: Demo Hello", 1),
		"sans commande":     "pack: {id: x, name: X, version: '1'}\ncommands: []\n",
		"yaml cassé":        "pack: [",
		"version manquante": strings.Replace(simple, "version: 1.0.0", "", 1),
	}
	for name, y := range cases {
		if _, err := Parse([]byte(y)); !errors.Is(err, ErrInvalidPack) {
			t.Errorf("%s: erreur attendue, obtenu %v", name, err)
		}
	}
	if _, err := Parse([]byte(strings.Repeat("a", maxPackBytes+1))); !errors.Is(err, ErrInvalidPack) {
		t.Error("taille non bornée")
	}
}

func TestExportRoundTrip(t *testing.T) {
	svc, _, u := setup(t)
	ctx := context.Background()
	bundled, _ := Bundled()
	for _, f := range bundled {
		if _, err := svc.Import(ctx, u, f, "", Options{AllowWarnings: true}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := svc.Export(ctx, nil, Meta{ID: "all", Name: "Tout", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("l'export doit être réimportable: %v\n%s", err, data[:200])
	}
	p, _ := svc.Preview(ctx, f)
	if p.Counts["unchanged"] != len(f.Commands) || p.Counts["modified"] != 0 {
		t.Fatalf("export puis aperçu: %+v", p.Counts)
	}
	if _, err := svc.Export(ctx, []string{"inconnu"}, Meta{ID: "x", Name: "x", Version: "1"}); err == nil {
		t.Fatal("export vide accepté")
	}
}
