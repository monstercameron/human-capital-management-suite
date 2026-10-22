package agenteval

import (
	"strings"
	"testing"
)

func TestAgentUXAmbient_PersonaSuite(t *testing.T) {
	target := PersonaEvaluationTarget{TenantID: "tenant", SyntheticTenantID: "synthetic", PersonaID: "persona", PersonaVersion: 1, InvokerID: "admin", ProfileDigest: "sha256:" + strings.Repeat("a", 64), ModelDigest: "sha256:" + strings.Repeat("b", 64)}
	for name, labels := range map[string][]AmbientCase{"task": TaskCatcherAmbientSuite(), "reminder": ReminderAmbientSuite()} {
		suite := AmbientPersonaSuite("suite."+name, "skill."+name, labels)
		if err := validatePersonaSuite(target, suite); err != nil {
			t.Fatalf("%s suite is not a valid persona suite: %v", name, err)
		}
		if len(suite.Cases) != len(labels)+4 {
			t.Fatalf("%s suite has %d cases for %d labels", name, len(suite.Cases), len(labels))
		}
		denied, offers := 0, 0
		for i, testCase := range suite.Cases[:len(labels)] {
			switch {
			case labels[i].Kind != "" && len(testCase.ExpectedSkills) != 1:
				t.Fatalf("%s label %q expects an offer but the case expects no skill", name, labels[i].Text)
			case labels[i].Kind == "" && len(testCase.ExpectedSkills) != 0:
				t.Fatalf("%s label %q expects nothing but the case expects a skill", name, labels[i].Text)
			}
			if testCase.Kind == PersonaDenied {
				denied++
			}
			if labels[i].Kind != "" {
				offers++
			}
		}
		if denied == 0 || offers == 0 {
			t.Fatalf("%s suite lost its denied (%d) or offer (%d) cases", name, denied, offers)
		}
	}
}
