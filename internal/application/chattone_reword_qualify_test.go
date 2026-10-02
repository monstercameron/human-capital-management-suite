package application

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

func chattoneRewordReplayFixture(t *testing.T) []ChattoneRewordRecorded {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "chattone_reword_replay.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []ChattoneRewordRecorded
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func chattoneRewordService2(t *testing.T, model chatrewrite.CheckedModel) *chatrewrite.Reworder {
	t.Helper()
	reworder, err := NewChattoneReworder(model, chattoneAllowAllPolicy{}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return reworder
}

// TestTodo_CHATTONE_002_Qualification replays a recorded model's answers
// through the real reword service and scores them on the labelled suite: every
// listed fact kept, nothing added, no hostile wording left, natural results.
// It is the free half of the evaluation; the paid half is the live run.
func TestTodo_CHATTONE_002_Qualification(t *testing.T) {
	suite := ChattoneRewordSuite()
	if len(suite) < 20 {
		t.Fatalf("the built-in suite has %d cases", len(suite))
	}
	languages := map[string]int{}
	for _, c := range suite {
		languages[c.Language]++
		if utf8.RuneCountInString(c.Text) < 10 || len(c.Facts) == 0 || len(c.Hostile) == 0 && !strings.Contains(c.Text, "??") {
			t.Fatalf("case %s is not labelled", c.ID)
		}
		if chatrewrite.ScreenHeat(c.Text) != chatrewrite.HeatHeated {
			t.Fatalf("case %s is not read as heated by the word lists", c.ID)
		}
	}
	if len(languages) < 6 {
		t.Fatalf("languages in the suite: %v", languages)
	}
	replay := NewChattoneRewordReplay(chattoneRewordReplayFixture(t))
	record := RunChattoneRewordQualification(context.Background(), chattoneRewordService2(t, replay), suite, agentmodel.ModelIdentity{ProviderID: "openai", ModelID: agentmodel.ChattoneDefaultModel, Version: agentmodel.ChattoneDefaultModel}, time.Now)
	if !record.Passed || record.Reworded != len(suite) || record.Preservation != 1 || record.FactsListed == 0 || record.AddedContent != 0 || record.HostileLeft != 0 || record.Unnatural != 0 || len(record.Failures) != 0 {
		raw, _ := json.MarshalIndent(record, "", " ")
		t.Fatalf("the recorded run did not qualify:\n%s", raw)
	}
	// The record names scores and digests, never message text.
	encoded, _ := json.Marshal(record)
	for _, c := range suite {
		if strings.Contains(string(encoded), c.Text) {
			t.Fatalf("the record carries message text: %s", c.ID)
		}
	}
	if !strings.HasPrefix(record.SuiteDigest, "sha256:") || record.AgentVersion != ChattoneAgentVersionDigest() || record.Model.ModelID != "gpt-6-luna" {
		t.Fatalf("record identity: %+v", record)
	}
}

// TestTodo_CHATTONE_002_Qualification_Negative: a model that does not do what is
// asked does not qualify, for each way of not doing it.
func TestTodo_CHATTONE_002_Qualification_Negative(t *testing.T) {
	suite := ChattoneRewordSuite()
	good := chattoneRewordReplayFixture(t)
	mutate := func(edit func(*ChattoneRewordRecorded)) *ChattoneRewordReplay {
		rows := append([]ChattoneRewordRecorded(nil), good...)
		edit(&rows[0])
		return NewChattoneRewordReplay(rows)
	}
	for name, tc := range map[string]struct {
		replay *ChattoneRewordReplay
		reason string
	}{
		"hostility kept": {mutate(func(r *ChattoneRewordRecorded) {
			r.Text = "You idiot, the deploy broke prod again. {0} must fix {1} files by {2}."
		}), "hostile"},
		"a fact dropped": {mutate(func(r *ChattoneRewordRecorded) {
			r.Text = "The deploy broke prod again. {0} must fix the files by {2}."
		}), "not reworded"},
		"a promise added": {mutate(func(r *ChattoneRewordRecorded) {
			r.Text = "I promise the deploy broke prod again. {0} must fix {1} files by {2}."
		}), "not reworded"},
		"the model's verdict fails": {mutate(func(r *ChattoneRewordRecorded) { r.MeaningPreserved = false }), "not reworded"},
		"no answer":                 {NewChattoneRewordReplay(good[1:]), "not reworded"},
		"hostile word stays": {mutate(func(r *ChattoneRewordRecorded) {
			r.Text = "The idiot deploy broke prod again. {0} must fix {1} files by {2}."
		}), "hostile"},
	} {
		record := RunChattoneRewordQualification(context.Background(), chattoneRewordService2(t, tc.replay), suite, agentmodel.ModelIdentity{}, time.Now)
		if record.Passed || len(record.Failures) == 0 || record.Failures[0].ID != "en-01" || !strings.Contains(record.Failures[0].Reason, tc.reason) {
			t.Errorf("%s: passed=%v failures=%+v", name, record.Passed, record.Failures)
		}
	}
	// A suite with a fact lost across many cases misses the 99 percent floor even
	// when each case alone looks plausible.
	if ChattoneRewordPreservationFloor != 0.99 {
		t.Fatal("the preservation floor changed")
	}
	// A suite file must be labelled.
	for _, bad := range []string{`[]`, `[{"id":"a","text":"x"}]`, `[{"id":"a","text":"x","facts":["1"]},{"id":"a","text":"y","facts":["2"]}]`, `[{"id":"a","text":"x","facts":["1"],"extra":1}]`} {
		if _, err := LoadChattoneRewordSuite([]byte(bad)); err == nil {
			t.Errorf("suite %s accepted", bad)
		}
	}
	if suite, err := LoadChattoneRewordSuite([]byte(`[{"id":"a","language":"en","text":"You idiot, fix 42 files.","facts":["42"],"hostile":["idiot"]}]`)); err != nil || len(suite) != 1 {
		t.Fatalf("a labelled suite: %v", err)
	}
}

// TestTodo_CHATTONE_002_QualifyLive_Recorded runs the paid path against a local
// stand-in for the provider that answers from the recorded fixture: the
// structured operation, the pricing and the recording all work, with no key
// that reaches a real provider.
func TestTodo_CHATTONE_002_QualifyLive_Recorded(t *testing.T) {
	replay := NewChattoneRewordReplay(chattoneRewordReplayFixture(t))
	provider := newChattoneProvider(t)
	provider.rewrite = func(_, user string) string {
		checked, err := replay.RewriteChecked(context.Background(), chatrewrite.Prompt{Data: user})
		if err != nil {
			return "no recorded answer"
		}
		return checked.Text
	}
	base := modelDeploymentFixture(t)
	base.BaseURL = provider.server.URL
	if !strings.HasPrefix(base.BaseURL, "http://127.0.0.1") {
		t.Fatalf("a recorded qualification must never reach a real provider: %q", base.BaseURL)
	}
	recorder := &ChattoneRewordRecorder{}
	record, err := QualifyChattoneReword(context.Background(), ChattoneQualifyConfig{Base: base, Model: "test-model", APIKey: "test-only-provider-key", MaxLatency: 20 * time.Second, MaxCostMicros: 20000, Now: time.Now}, ChattoneRewordSuite(), recorder)
	if err != nil || !record.Passed || record.Cases != 24 || record.CostMicros <= 0 || record.CostMicros%120 != 0 {
		t.Fatalf("%v %+v", err, record)
	}
	if provider.calls() != int(record.CostMicros/120) || provider.calls() != 24 {
		t.Fatalf("%d provider calls for cost %d", provider.calls(), record.CostMicros)
	}
	for _, r := range provider.requests {
		if r.Authorization != "Bearer test-only-provider-key" || r.Store || !strings.Contains(r.Raw, `"json_schema"`) || strings.Contains(r.Raw, "temperature") || strings.Contains(r.Raw, "prompt_cache_key") || !strings.HasSuffix(r.System, chatrewrite.RewordInstruction) {
			t.Fatalf("request %+v", r)
		}
	}
	// What was recorded is what a replay needs: every draft once, with {n}
	// placeholders, the same text as the checked-in fixture.
	rows := recorder.Recorded()
	if len(rows) != 24 {
		t.Fatalf("recorded %d answers", len(rows))
	}
	fixture := NewChattoneRewordReplay(chattoneRewordReplayFixture(t))
	for _, row := range rows {
		if want, ok := fixture.Answers[row.Key]; !ok || want.Text != row.Text || want.MeaningPreserved != row.MeaningPreserved || strings.Contains(row.Text, "⟦") {
			t.Fatalf("recorded row %+v differs from the fixture", row)
		}
	}
	// The default model has no approved price in this deployment: an error that
	// names it, and no call.
	before := provider.calls()
	if _, err := QualifyChattoneReword(context.Background(), ChattoneQualifyConfig{Base: base, APIKey: "test-only-provider-key", MaxLatency: 20 * time.Second, MaxCostMicros: 20000, Now: time.Now}, ChattoneRewordSuite(), nil); err == nil || !strings.Contains(err.Error(), "gpt-6-luna") || provider.calls() != before {
		t.Fatalf("the default model without a price: %v", err)
	}
}

// TestTodo_CHATTONE_002_QualifyBoth: one run scores the same model on the
// writing styles and on rewording, passes only if both do, and the document it
// produces carries the evidence of both.
func TestTodo_CHATTONE_002_QualifyBoth(t *testing.T) {
	replay := NewChattoneRewordReplay(chattoneRewordReplayFixture(t))
	good := func(system, user string) string {
		if strings.HasSuffix(system, chatrewrite.RewordInstruction) {
			checked, err := replay.RewriteChecked(context.Background(), chatrewrite.Prompt{Data: user})
			if err != nil {
				return "no recorded answer"
			}
			return checked.Text
		}
		return chattoneGoodRewrite(system, user)
	}
	run := func(rewrite func(system, user string) string, suite []ChattoneRewordCase) (ChattoneQualification, PersonaModelDeployment, error, *chattoneProvider) {
		provider := newChattoneProvider(t)
		provider.rewrite = rewrite
		base := modelDeploymentFixture(t)
		base.BaseURL = provider.server.URL
		q, dep, err := QualifyChattoneModel(context.Background(), ChattoneQualifyConfig{Base: base, Model: "test-model", APIKey: "test-only-provider-key", MaxLatency: 20 * time.Second, MaxCostMicros: 20000, Now: time.Now, RewordSuite: suite})
		return q, dep, err, provider
	}
	both, dep, err, provider := run(good, ChattoneRewordSuite())
	if err != nil || !both.Passed || both.Reword == nil || !both.Reword.Passed || provider.calls() != 30+24 || both.CostMicros != int64(54*120) {
		t.Fatalf("both suites: %v passed=%v reword=%+v calls=%d", err, both.Passed, both.Reword, provider.calls())
	}
	stylesOnly, _, _, _ := run(good, nil)
	if both.SuiteDigest == stylesOnly.SuiteDigest || both.Evaluation().SuiteDigest != both.SuiteDigest {
		t.Fatal("the evidence does not cover the reword suite")
	}
	raw, _ := json.Marshal(dep)
	loaded, err := ParsePersonaModelDeployment(raw)
	if err != nil {
		t.Fatal(err)
	}
	if qualified, err := chattoneQualify(loaded); err != nil || qualified.profile.Evaluation.SuiteDigest != both.SuiteDigest {
		t.Fatalf("the document does not carry both suites' evidence: %v", err)
	}
	// A model that rewords badly does not qualify, however well it writes styles.
	badReword := func(system, user string) string {
		if strings.HasSuffix(system, chatrewrite.RewordInstruction) {
			return chattoneDraftOf(user)
		}
		return chattoneGoodRewrite(system, user)
	}
	q, dep, err, _ := run(badReword, ChattoneRewordSuite())
	if !errors.Is(err, ErrChattoneNotQualified) || q.Passed || q.Reword == nil || q.Reword.Passed || len(dep.Profiles) != 0 {
		t.Fatalf("a model that rewords badly qualified: %v %v", err, q.Passed)
	}
}
