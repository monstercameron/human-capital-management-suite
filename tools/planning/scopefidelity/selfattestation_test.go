package scopefidelity

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTodo_REV_001_02 is the REV-001-02 primary: a ticked item whose latest
// evidence text admits a non-green live run must be flagged; a ticked item
// with a green live run and an unticked item with open notes stay clean.
func TestTodo_REV_001_02(t *testing.T) {
	claims := []EvidenceClaim{
		{
			TodoID: "GOV-017",
			Ticked: true,
			Text: "the live tddcontract/plancheck run remains non-green with 1,094 findings; " +
				"cannot close until those red-first evidence and oracle defects are repaired",
		},
		{
			TodoID: "GOV-018",
			Ticked: true,
			Text: "TestTodoTestMatrixApplicability in tools/planning/todogovernance; " +
				"go test -count=1 ./tools/planning/todogovernance/ PASS on windows/arm64 (Go 1.26.3)",
		},
		{
			TodoID: "GOV-030",
			Ticked: false,
			Text:   "still open: live command remains non-green with 6,202 findings",
		},
	}

	byID := map[string][]Finding{}
	for _, f := range FlagSelfAdmittingEvidence(claims) {
		byID[f.TodoID] = append(byID[f.TodoID], f)
	}

	if len(byID["GOV-017"]) == 0 {
		t.Fatal("GOV-017 self-admitting non-green evidence not flagged")
	}
	for _, f := range byID["GOV-017"] {
		if f.Code != CodeSelfAdmittingOpen {
			t.Errorf("GOV-017 code = %q, want %q", f.Code, CodeSelfAdmittingOpen)
		}
	}
	for _, id := range []string{"GOV-018", "GOV-030"} {
		if len(byID[id]) != 0 {
			t.Errorf("%s unexpectedly flagged: %v", id, byID[id])
		}
	}
}

// TestTodo_REV_001_02_Golden pins the exact rendered findings bytes for the
// canonical self-admitting-evidence fixture.
func TestTodo_REV_001_02_Golden(t *testing.T) {
	findings := FlagSelfAdmittingEvidence([]EvidenceClaim{
		{
			TodoID: "GOV-017",
			Ticked: true,
			Text: "the live tddcontract/plancheck run remains non-green with 1,094 findings; " +
				"cannot close until those red-first evidence and oracle defects are repaired",
		},
	})
	want, err := os.ReadFile(filepath.Join("testdata", "rev00102.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderFindings(findings); got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}
