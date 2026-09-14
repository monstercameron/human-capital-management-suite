package roleaccess

import "testing"

func TestVisibilityEvaluatorPreservesAdditiveDirectoryPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policies []VisibilityPolicy
		own      string
		target   string
		self     bool
		want     bool
	}{
		{name: "self without roles", target: "other", self: true, want: true},
		{name: "no roles", own: "Engineering", target: "Engineering"},
		{name: "all", policies: []VisibilityPolicy{{Mode: VisibilityAll}}, target: "Finance", want: true},
		{name: "own unit", policies: []VisibilityPolicy{{Mode: VisibilityOwnUnit}}, own: " Engineering ", target: "engineering", want: true},
		{name: "own unit unknown", policies: []VisibilityPolicy{{Mode: VisibilityOwnUnit}}, target: "Engineering"},
		{name: "allowlist", policies: []VisibilityPolicy{{Mode: VisibilityAllowlist, OrganizationUnits: []string{" Finance "}}}, target: "finance", want: true},
		{name: "allowlist miss", policies: []VisibilityPolicy{{Mode: VisibilityAllowlist, OrganizationUnits: []string{"Finance"}}}, target: "People"},
		{name: "denylist admits outside", policies: []VisibilityPolicy{{Mode: VisibilityDenylist, OrganizationUnits: []string{"Finance"}}}, target: "People", want: true},
		{name: "denylist withholds inside", policies: []VisibilityPolicy{{Mode: VisibilityDenylist, OrganizationUnits: []string{"Finance"}}}, target: "Finance"},
		{name: "additive allow overrides deny", policies: []VisibilityPolicy{{Mode: VisibilityDenylist, OrganizationUnits: []string{"Finance"}}, {Mode: VisibilityAllowlist, OrganizationUnits: []string{"Finance"}}}, target: "Finance", want: true},
		{name: "two denylists union", policies: []VisibilityPolicy{{Mode: VisibilityDenylist, OrganizationUnits: []string{"Finance"}}, {Mode: VisibilityDenylist, OrganizationUnits: []string{"People"}}}, target: "Finance", want: true},
		{name: "unknown mode fails closed", policies: []VisibilityPolicy{{Mode: "ANYTHING"}}, target: "Finance"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NewVisibilityEvaluator(test.policies, test.own).Allows(test.target, test.self)
			if got != test.want {
				t.Fatalf("Allows(%q, self=%t) = %t, want %t", test.target, test.self, got, test.want)
			}
		})
	}
}
