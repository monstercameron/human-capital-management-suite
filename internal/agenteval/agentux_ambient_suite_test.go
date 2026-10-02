package agenteval

import "testing"

func TestAgentUXAmbient_LabelledSuites_Golden(t *testing.T) {
	for name, cases := range map[string][]AmbientCase{"tasks": TaskCatcherAmbientSuite(), "reminders": ReminderAmbientSuite()} {
		if len(cases) != 40 {
			t.Fatalf("%s has %d cases", name, len(cases))
		}
		seen := map[string]bool{}
		private, public, negative := 0, 0, 0
		for _, c := range cases {
			if c.Text == "" || seen[c.Text] {
				t.Fatalf("%s duplicate or blank case", name)
			}
			seen[c.Text] = true
			if c.Kind == "" {
				negative++
				continue
			}
			if c.Scope == "PRIVATE" {
				private++
				if c.Person == "" {
					t.Fatal("private label missing owner")
				}
			} else if c.Scope == "PUBLIC" {
				public++
				if c.Person != "" {
					t.Fatal("public label has owner")
				}
			} else {
				t.Fatal("invalid scope")
			}
		}
		if private < 5 || public < 5 || negative < 15 {
			t.Fatalf("%s incomplete label distribution", name)
		}
	}
}
