package chatlang

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATLANG_001: the record states one default engine and model, three
// families each with where its text goes and what is still open, a fallback that
// ends at the message as written, and says which research was not measured.
func TestTodo_CHATLANG_001(t *testing.T) {
	d := ResearchDecisions()
	families := map[EngineFamily]int{}
	defaults := 0
	for _, c := range d.Candidates {
		families[c.Family]++
		if c.Name == "" || c.Leaves == "" || c.Open == "" {
			t.Errorf("candidate %+v is not described", c)
		}
		if c.Status == StatusDefault {
			defaults++
			if c.Family != FamilyLanguageModel {
				t.Errorf("the default is %s", c.Family)
			}
		}
	}
	if len(families) != 3 || families[FamilyLanguageModel] != 1 || families[FamilyService] != 1 || families[FamilyLocalModel] != 1 || defaults != 1 {
		t.Fatalf("families %v with %d defaults", families, defaults)
	}
	if d.DefaultEngine != "openai" || d.DefaultModel != "gpt-6-luna" {
		t.Fatalf("default engine %q model %q", d.DefaultEngine, d.DefaultModel)
	}
	if len(d.Fallback) < 2 || d.Fallback[0] != d.DefaultEngine || d.Fallback[len(d.Fallback)-1] != "the message as written" {
		t.Fatalf("fallback %v", d.Fallback)
	}
	for name, list := range map[string][]string{"detection": d.Detection, "quality": d.Quality, "cost": d.Cost, "conditions": d.Conditions, "not measured": d.NotMeasured} {
		if len(list) < 3 {
			t.Errorf("%s has %d entries", name, len(list))
		}
		for _, line := range list {
			if strings.TrimSpace(line) == "" {
				t.Errorf("%s has an empty entry", name)
			}
		}
	}
	// The record must not claim measurements: everything it lists as not
	// measured is the research of CHATLANG-001's GREEN that needs a live model.
	if !slices.ContainsFunc(d.NotMeasured, func(s string) bool { return strings.Contains(s, "300 labelled messages") }) {
		t.Fatal("the record does not say the labelled set was not run")
	}
}

// TestTodo_CHATLANG_001_Golden pins the record, so changing a decision is a
// visible change to this test.
func TestTodo_CHATLANG_001_Golden(t *testing.T) {
	raw, err := json.Marshal(ResearchDecisions())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	const golden = "20c7686524a3842667d769d76ec8b4aec399eef677f820b176e7a0f5ce61c50d"
	if got := hex.EncodeToString(sum[:]); got != golden {
		t.Fatalf("the decision record changed: sha256 %s (%d bytes)\n%s", got, len(raw), raw)
	}
}

// TestTodo_CHATLANG_001_Security: text leaves the deployment only in the data
// classes the translation task is qualified for, only under approved terms, and a
// channel that bars outside services is never sent to one.
func TestTodo_CHATLANG_001_Security(t *testing.T) {
	d := ResearchDecisions()
	classes := append([]string(nil), d.Classes...)
	sort.Strings(classes)
	if strings.Join(classes, ",") != "internal,public" {
		t.Fatalf("classes %v", classes)
	}
	for _, c := range d.Candidates {
		if c.Family == FamilyLocalModel && !strings.Contains(c.Leaves, "stays in the deployment") {
			t.Errorf("a local model's text leaves: %s", c.Leaves)
		}
		if c.Family != FamilyLocalModel && !strings.Contains(c.Leaves, "terms") {
			t.Errorf("an outside candidate does not say whose terms apply: %s", c.Leaves)
		}
	}
	for _, need := range []string{"allows an outside service", "barred outside services", "approved for the translation purpose", "filters would mask"} {
		if !slices.ContainsFunc(d.Conditions, func(s string) bool { return strings.Contains(s, need) }) {
			t.Errorf("the conditions lack %q", need)
		}
	}
	// The conditions are the ones Decide enforces.
	on := Workspace{Enabled: true, ExternalAllowed: true}
	if Decide(on, Channel{External: ExternalBarred}, "de", true, 0) != ReasonExternalBarred {
		t.Fatal("a barred channel may use an outside engine")
	}
	if Decide(Workspace{Enabled: true}, Channel{}, "de", true, 0) != ReasonExternalBarred {
		t.Fatal("a workspace that does not allow outside services is sent to one")
	}
	if Decide(on, Channel{}, "de", true, 0) != Allowed || Decide(Workspace{}, Channel{}, "de", false, 0) != ReasonWorkspaceOff {
		t.Fatal("the conditions do not decide as the record says")
	}
}

// TestTodo_CHATLANG_001_Performance: the budgets of CHATLANG-003 are stated, and
// everything HCM does around an engine call (protecting, building the prompt,
// verifying and restoring) takes a small fraction of the live budget, so the
// budget is the engine's to spend. The engine's own latency is not measured here
// and needs a live run.
func TestTodo_CHATLANG_001_Performance(t *testing.T) {
	d := ResearchDecisions()
	if d.LivePercentile95Millis != 700 || d.PagePercentile95Millis != 2000 {
		t.Fatalf("budgets %d / %d", d.LivePercentile95Millis, d.PagePercentile95Millis)
	}
	text := "Please send the signed form to https://example.com/forms/42 and ping @carla before 2026-10-01 at 14:30; the file is Q3_report.xlsx and the code is `go test ./...`. Thanks!"
	request := Request{Tenant: "t", Source: "en", Target: "de", Context: []string{"Earlier message one", "Earlier message two"}}
	const rounds = 400
	samples := make([]time.Duration, 0, rounds)
	for i := 0; i < rounds; i++ {
		start := time.Now()
		protected := Protect(text, Glossary{}, "de")
		request.Text = protected.Text()
		_, _ = StructuredPrompt(request)
		answer := "[de] " + protected.Text()
		if failures := protected.Verify(answer); len(failures) != 0 {
			t.Fatalf("verify: %v", failures)
		}
		if _, failures, err := protected.Restore(answer); err != nil || len(failures) != 0 {
			t.Fatalf("restore: %v %v", failures, err)
		}
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[rounds*95/100]
	if budget := time.Duration(d.LivePercentile95Millis) * time.Millisecond / 20; p95 > budget {
		t.Fatalf("the work around the engine takes %v at the 95th percentile; it may take 5%% of the live budget, %v", p95, budget)
	}
	t.Logf("protect, prompt, verify and restore: p95 %v over %d messages", p95, rounds)
}
