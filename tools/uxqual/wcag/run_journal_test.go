package wcag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

func TestUX003RunJournalBindsActualResultsAndChainsRuns(t *testing.T) {
	t.Setenv(runJournalEnv, filepath.Join(t.TempDir(), "ux003.jsonl"))
	evidence := Evidence{Todo: "UX-003", Standard: "WCAG 2.2 AA", Artifact: "tools/uxqual/wcag"}
	firstResults := []qual.CriterionResult{{Name: "Keyboard-only completion", Pass: true, Detail: "observed"}}
	first, err := RecordUX003Run(firstResults, firstResults, evidence, true)
	if err != nil {
		t.Fatal(err)
	}
	secondResults := []qual.CriterionResult{{Name: "Keyboard-only completion", Pass: false, Detail: "regression observed"}}
	second, err := RecordUX003Run(secondResults, secondResults, evidence, false)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := LoadRunJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != first.ID || entries[1].ID != second.ID || entries[1].Sequence != 2 || entries[1].PreviousDigest != first.Digest {
		t.Fatalf("journal did not chain observed executions: %+v", entries)
	}
	if entries[1].GatePassed || entries[1].Results[0].Pass {
		t.Fatal("failed UX-003 execution was recorded as a pass")
	}

	path := filepath.Join(t.TempDir(), "tampered.jsonl")
	t.Setenv(runJournalEnv, path)
	if _, err := RecordUX003Run(firstResults, firstResults, evidence, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"pass":true`, `"pass":false`, 1)
	if tampered == string(raw) {
		t.Fatal("fixture did not contain a result to mutate")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunJournal(); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("mutated run journal was accepted: %v", err)
	}
}
