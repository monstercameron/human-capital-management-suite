package reasontext

import "testing"

func TestTokenShaped(t *testing.T) {
	cases := map[string]bool{
		"promotion_into_senior_hrbp": true,
		"scope-growth":               true,
		"  team_lead  ":              true,
		"Reorganisation":             false,
		"":                           false,
		"   ":                        false,
		"Took on the team_lead role": false,
		"Leads the on-call rotation": false,
	}
	for value, want := range cases {
		if got := TokenShaped(value); got != want {
			t.Errorf("TokenShaped(%q) = %v, want %v", value, got, want)
		}
	}
}
