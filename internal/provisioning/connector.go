// Package provisioning crée et tient à jour serveurs et groupes à partir d'une source externe (Warpgate, …).
//
// Les connecteurs sont compilés dans le binaire et inscrits dans un registre: pas de chargement dynamique
// ni de sous-processus (invariant 10). Un connecteur ne fait que lire la source; c'est le moteur qui écrit.
package provisioning

import (
	"context"
	"fmt"
	"sort"
)

// Machine est une machine découverte dans la source.
type Machine struct {
	ExternalID  string // identifiant stable côté source
	Name        string
	Description string
	GroupID     string // groupe côté source (vide = aucun)
	GroupName   string

	// Accès SSH (le connecteur les connaît: avec un bastion, toutes les machines partagent hôte et clé).
	Host           string
	Port           int
	SSHUser        string // vide = utilisateur de l'identité
	HostKeyFingerp string // empreinte validée par un admin (vide = à valider)
}

// Env transmet au connecteur ce que le moteur sait de la configuration commune.
type Env struct {
	User string // utilisateur par défaut (celui de l'identité, ou celui choisi dans la synchro)
}

// Connector lit les machines d'une source.
type Connector interface {
	Discover(ctx context.Context) ([]Machine, error)
}

// Bastion est implémenté par les connecteurs qui passent par un point d'entrée SSH unique:
// l'admin épingle sa clé une fois, toutes les machines en héritent.
type Bastion interface {
	Endpoint() (host string, port int)
}

// FieldKind décrit le widget du formulaire de configuration.
type FieldKind string

const (
	FieldText     FieldKind = "text"
	FieldNumber   FieldKind = "number"
	FieldBool     FieldKind = "bool"
	FieldPassword FieldKind = "password" // le secret du connecteur (jamais renvoyé)
)

type Field struct {
	Key      string    `json:"key"`
	Kind     FieldKind `json:"kind"`
	Label    string    `json:"label"`
	Help     string    `json:"help,omitempty"`
	Required bool      `json:"required"`
	Default  any       `json:"default,omitempty"`
}

// Type est un connecteur enregistré.
type Type struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
	// New construit le connecteur; secret est la valeur déchiffrée du champ password.
	New func(cfg map[string]any, secret string, env Env) (Connector, error) `json:"-"`
}

var registry = map[string]Type{}

// Register inscrit un type de connecteur (appelé au démarrage par cmd/leash).
func Register(t Type) { registry[t.ID] = t }

func Lookup(id string) (Type, error) {
	t, ok := registry[id]
	if !ok {
		return Type{}, fmt.Errorf("type de connecteur inconnu: %q", id)
	}
	return t, nil
}

func Types() []Type {
	out := make([]Type, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Helpers de lecture de configuration.
func Str(cfg map[string]any, k string) string { s, _ := cfg[k].(string); return s }
func Bool(cfg map[string]any, k string) bool  { b, _ := cfg[k].(bool); return b }
func Int(cfg map[string]any, k string, def int) int {
	switch v := cfg[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}
