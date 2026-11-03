package catalog

import "testing"

func hasCode(is []Issue, code string) bool {
	for _, i := range is {
		if i.Code == code {
			return true
		}
	}
	return false
}

var svc = map[string]ParamDef{"service": {Type: TypeService}}

func TestLintRules(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		defs map[string]ParamDef
		risk Risk
		code string
		// quand le code est attendu comme erreur en mode proposition et avertissement en mode admin
		soft bool
	}{
		{"semicolon", "uptime ; id", nil, RiskRead, "shell_metachar", true},
		{"ampersand", "uptime && id", nil, RiskRead, "shell_metachar", true},
		{"pipe", "ps aux | sort", nil, RiskRead, "pipe", true},
		{"backtick", "echo `id`", nil, RiskRead, "shell_metachar", true},
		{"subshell", "echo $(id)", nil, RiskRead, "shell_metachar", true},
		{"redirect-out", "ls > /tmp/x", nil, RiskRead, "shell_metachar", true},
		{"redirect-in", "cat < /etc/passwd", nil, RiskRead, "shell_metachar", true},
		{"newline", "uptime\nid", nil, RiskRead, "shell_metachar", true},
		{"dollar-brace", "echo ${HOME}", nil, RiskRead, "shell_metachar", true},
		{"quoted-placeholder", `echo "{{service}}"`, svc, RiskRead, "placeholder_quoted", false},
		{"glued-placeholder", `ls /var/{{service}}.log`, svc, RiskRead, "placeholder_not_standalone", false},
		{"undeclared", "ls {{nope}}", nil, RiskRead, "placeholder_undeclared", false},
		{"unused", "uptime", svc, RiskRead, "param_unused", false},
		{"unknown-type", "ls {{x}}", map[string]ParamDef{"x": {Type: "text"}}, RiskRead, "param_definition", false},
		{"empty-enum", "ls {{x}}", map[string]ParamDef{"x": {Type: TypeEnum}}, RiskRead, "param_definition", false},
		{"int-no-bounds", "ls {{x}}", map[string]ParamDef{"x": {Type: TypeInt}}, RiskRead, "param_definition", false},
		{"path-no-prefix", "ls {{x}}", map[string]ParamDef{"x": {Type: TypePath}}, RiskRead, "param_definition", false},
		{"only-placeholder", "{{service}}", svc, RiskRead, "first_word_placeholder", false},
		{"first-word-placeholder", "{{service}} -x", svc, RiskRead, "first_word_placeholder", false},
		{"interp-bash", "bash -c ls", nil, RiskRead, "interpreter", true},
		{"interp-path", "/usr/bin/python3.11 x.py", nil, RiskRead, "interpreter", true},
		{"interp-sudo", "sudo ls", nil, RiskRead, "interpreter", true},
		{"interp-env", "env FOO=1 ls", nil, RiskRead, "interpreter", true},
		{"interp-xargs", "xargs ls", nil, RiskRead, "interpreter", true},
		{"risk-missing", "uptime", nil, "", "risk_missing", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Lint(c.tpl, c.defs, c.risk, ModeProposal)
			if !hasCode(p.Errors, c.code) {
				t.Fatalf("proposition: erreur %s attendue, obtenu %+v", c.code, p)
			}
			a := Lint(c.tpl, c.defs, c.risk, ModeAdmin)
			if c.soft {
				if hasCode(a.Errors, c.code) || !hasCode(a.Warnings, c.code) {
					t.Fatalf("admin: avertissement %s attendu, obtenu %+v", c.code, a)
				}
			} else if !hasCode(a.Errors, c.code) {
				t.Fatalf("admin: erreur %s attendue, obtenu %+v", c.code, a)
			}
		})
	}
}

func TestLintAccepts(t *testing.T) {
	good := []struct {
		tpl  string
		defs map[string]ParamDef
	}{
		{"uptime", nil},
		{"journalctl -u {{service}} -n 50 --no-pager", svc},
		{"systemctl status --unit={{service}}", svc},
		{`mysql --batch -e "SHOW REPLICA STATUS\G"`, nil},
		{"df -h -x tmpfs -x devtmpfs", nil},
	}
	for _, g := range good {
		if r := Lint(g.tpl, g.defs, RiskRead, ModeProposal); !r.OK() || len(r.Warnings) > 0 {
			t.Errorf("%q devrait passer: %+v", g.tpl, r)
		}
	}
}
