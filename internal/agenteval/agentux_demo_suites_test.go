package agenteval

import "testing"

func TestAgentUXDemo_Suites(t *testing.T) {
	birthday, support := BirthdayBuddySuite("reply"), SupportDeskSuite("ticket", "alert")
	if birthday.ID != "AGENTUX-053.birthday-buddy" || len(birthday.Cases) != 8 || support.ID != "AGENTUX-054.support-desk" || len(support.Cases) != 10 {
		t.Fatalf("contracts: %+v %+v", birthday, support)
	}
	for _, suite := range []PersonaSuite{birthday, support} {
		seen := map[string]bool{}
		for _, c := range suite.Cases {
			if seen[c.ID] || c.Prompt == "" {
				t.Fatalf("invalid case: %+v", c)
			}
			seen[c.ID] = true
		}
		if PersonaSuiteDigest(suite) == "" {
			t.Fatal("missing suite digest")
		}
	}
	for _, i := range []int{2, 3, 4, 7} {
		if len(support.Cases[i].ExpectedSkills) != 2 {
			t.Fatal("injection or escalation must retain only the two support effects")
		}
	}
	if support.Cases[6].Kind != PersonaDenied || birthday.Cases[6].Kind != PersonaOutOfScope || birthday.Cases[7].Kind != PersonaDenied {
		t.Fatal("refusal cases missing")
	}
	fresh := SupportDeskSuite("ticket", "alert")
	fresh.Cases[0].ExpectedSkills[0] = "tampered"
	if support.Cases[0].ExpectedSkills[0] != "ticket" {
		t.Fatal("suite calls alias skills")
	}
}
