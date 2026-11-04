package oidc

// RoleMap: valeurs de revendication associées à chaque rôle Leassh.
type RoleMap struct {
	Admin    []string `json:"admin"`
	Approver []string `json:"approver"`
	Viewer   []string `json:"viewer"`
}

// MapRole détermine le rôle à partir de la revendication configurée. Le rôle le plus élevé gagne.
// Sans correspondance: defaultRole (« viewer », « approver » ou « admin »), ou refus (ok=false) s'il est vide.
// claim vide: tout le monde reçoit le rôle par défaut.
func MapRole(cl Claims, claim string, m RoleMap, defaultRole string) (role string, ok bool) {
	if claim != "" {
		have := map[string]bool{}
		for _, v := range cl.Strings(claim) {
			have[v] = true
		}
		for _, c := range []struct {
			role string
			vals []string
		}{{"admin", m.Admin}, {"approver", m.Approver}, {"viewer", m.Viewer}} {
			for _, v := range c.vals {
				if v != "" && have[v] {
					return c.role, true
				}
			}
		}
	}
	if defaultRole == "" {
		return "", false
	}
	return defaultRole, true
}
