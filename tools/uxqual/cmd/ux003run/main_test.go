package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/vpat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func TestExplicitRunAppendsVerifiedRecordsIdempotently(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "ux003-run-journal.jsonl")
	t.Setenv("UX003_RUN_JOURNAL", journalPath)
	governedJournal := filepath.Join("..", "..", "..", "..", "definitions", "ux", "wcag", "ux-003-run-journal.jsonl")
	governedBefore, err := os.ReadFile(governedJournal)
	if err != nil {
		t.Fatalf("read governed journal before command test: %v", err)
	}

	var output bytes.Buffer
	if err := run(nil, &output); err != nil {
		t.Fatalf("first explicit run: %v", err)
	}
	first, err := wcag.LatestRunRecord()
	if err != nil {
		t.Fatalf("load first run: %v", err)
	}
	if !first.GatePassed || first.GateScope != wcag.PrimaryGateScope || !strings.Contains(output.String(), "recorded "+first.ID) {
		t.Fatalf("first run did not record/report a passing SSR primary gate: record=%+v output=%q", first, output.String())
	}
	assertCurrentScores(t, first)
	if !hasGWCAuthorizationFinding(first) {
		t.Fatal("run record omitted the known GWC authorization projection finding")
	}

	firstJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		latest, err := wcag.LatestRunRecord()
		if err != nil {
			t.Fatalf("validate journal run %d: %v", i, err)
		}
		if latest.Digest != first.Digest || latest.Sequence != first.Sequence {
			t.Fatalf("read-only validation changed latest run: %+v", latest)
		}
	}
	firstAfterValidation, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJournal, firstAfterValidation) {
		t.Fatal("repeated journal validation changed the run journal")
	}

	output.Reset()
	if err := run(nil, &output); err != nil {
		t.Fatalf("second explicit run: %v", err)
	}
	second, err := wcag.LatestRunRecord()
	if err != nil {
		t.Fatalf("load second run: %v", err)
	}
	if second.Sequence != first.Sequence+1 || second.PreviousDigest != first.Digest || !strings.Contains(output.String(), "recorded "+second.ID) {
		t.Fatalf("second run did not append/report a chained record: first=%+v second=%+v output=%q", first, second, output.String())
	}
	assertCurrentScores(t, second)
	report, err := vpat.GenerateFromRun(second, nil)
	if err != nil {
		t.Fatalf("generate report from second verified run: %v", err)
	}
	if err := report.ValidateIntegrity(); err != nil {
		t.Fatalf("validate report from second verified run: %v", err)
	}

	beforeBadInputs, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-evidence", filepath.Join(t.TempDir(), "untrusted.yaml")}, &output); err == nil {
		t.Fatal("command accepted caller-supplied evidence path")
	}
	if err := run([]string{"unexpected"}, &output); err == nil {
		t.Fatal("command accepted unexpected positional argument")
	}
	afterBadInputs, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBadInputs, afterBadInputs) {
		t.Fatal("rejected command inputs changed the journal")
	}

	governedAfter, err := os.ReadFile(governedJournal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(governedBefore, governedAfter) {
		t.Fatal("command test mutated the checked-in governed journal")
	}
}

func assertCurrentScores(t *testing.T, record wcag.RunRecord) {
	t.Helper()
	fixture := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		t.Fatal(err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !record.MatchesResults(wcag.Score(ssrDoc), wcag.Score(gwcDoc)) {
		t.Fatal("recorded scorecard does not match freshly rendered UX-003 surfaces")
	}
}

func hasGWCAuthorizationFinding(record wcag.RunRecord) bool {
	for _, result := range record.Results {
		if result.Surface == "GWC" && result.Name == "Accessible authorization projection" && !result.Pass {
			return true
		}
	}
	return false
}
