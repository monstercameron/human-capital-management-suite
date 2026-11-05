package agentcost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// The RED state of AGENTCOST-001: a run records one cost figure and a stage
// timing line, and nothing reports tokens and cost per model call, per stage,
// per agent and per trigger. These tests pin the per-call ledger line that
// replaces that, the seeded replay it is measured from, and the checked-in
// report under definitions/planning.

func TestTodo_AGENTCOST_001(t *testing.T) {
	line := agentmodel.CallLine{
		TenantID: "t1", AgentID: "policy", TriggerKind: "mention", RunID: "r1", QuestionID: "q1",
		CallSite: "persona_run_executor:answer", Stage: agentmodel.CallStageAnswer, Kind: agentmodel.CallKindGeneration,
		ProviderID: "schemaflux", ModelID: "gpt-6-luna", ModelVersion: "2026-09-01",
		InputTokens:       1200,
		OutputTokens:      300,
		CachedInputTokens: 400,
		CostMicros:        255,
		Basis:             agentmodel.CostMeasured,
		LatencyMillis:     812,
		StartedAt:         time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		PromptDigest:      "sha256:aaa",
		ScheduleVersion:   "rates-2026-10-01",
	}
	if err := line.Validate(); err != nil {
		t.Fatalf("a complete ledger line is refused: %v", err)
	}
	for name, mutate := range map[string]func(*agentmodel.CallLine){
		"no tenant":     func(l *agentmodel.CallLine) { l.TenantID = "" },
		"no agent":      func(l *agentmodel.CallLine) { l.AgentID = "" },
		"no call site":  func(l *agentmodel.CallLine) { l.CallSite = "" },
		"bad stage":     func(l *agentmodel.CallLine) { l.Stage = "guessing" },
		"bad kind":      func(l *agentmodel.CallLine) { l.Kind = "thinking" },
		"bad basis":     func(l *agentmodel.CallLine) { l.Basis = "rumoured" },
		"cached excess": func(l *agentmodel.CallLine) { l.CachedInputTokens = 1201 },
		"no start":      func(l *agentmodel.CallLine) { l.StartedAt = time.Time{} },
	} {
		broken := line
		mutate(&broken)
		if err := broken.Validate(); err == nil {
			t.Fatalf("%s: an incomplete line validates", name)
		}
	}

	// The three kinds separate text a person reads from structured output from
	// "choose one of a few options".
	for profile, want := range map[string]agentmodel.CallKind{
		"chat.answer":            agentmodel.CallKindGeneration,
		"chat.reword":            agentmodel.CallKindGeneration,
		"todo.extract":           agentmodel.CallKindExtraction,
		"reminder.extract_slots": agentmodel.CallKindExtraction,
		"screen.message":         agentmodel.CallKindDecision,
		"audience.decide":        agentmodel.CallKindDecision,
		"search.decide_route":    agentmodel.CallKindDecision,
		"reaction.choose":        agentmodel.CallKindDecision,
		"support.triage":         agentmodel.CallKindDecision,
		"something.new":          agentmodel.CallKindGeneration,
	} {
		if got := agentmodel.ClassifyTaskProfile(profile); got != want {
			t.Fatalf("ClassifyTaskProfile(%q) = %q, want %q", profile, got, want)
		}
	}

	var ledger agentmodel.CallLedger
	lines := Agentcost001SeededLines()
	if len(lines) == 0 {
		t.Fatal("the seeded replay is empty")
	}
	for _, seeded := range lines {
		if err := ledger.Append(seeded); err != nil {
			t.Fatalf("seeded line refused: %v", err)
		}
	}
	byAgent := ledger.TotalsByAgent()
	if len(byAgent) == 0 {
		t.Fatal("no per-agent totals")
	}
	var agents, triggers, questions int64
	for _, totals := range byAgent {
		agents += totals.CostMicros
		if totals.Calls == 0 || totals.TokensIn == 0 {
			t.Fatal("an agent total has no calls or tokens")
		}
	}
	for _, totals := range ledger.TotalsByTrigger() {
		triggers += totals.CostMicros
	}
	for _, totals := range ledger.TotalsByQuestion() {
		questions += totals.CostMicros
	}
	if agents == 0 || agents != triggers || agents != questions {
		t.Fatalf("totals disagree: agents %d triggers %d questions %d", agents, triggers, questions)
	}
	top := ledger.TopCallSites(10)
	if len(top) == 0 || len(top) > 10 {
		t.Fatalf("top call sites = %d", len(top))
	}
	for i := 1; i < len(top); i++ {
		if top[i-1].Totals.CostMicros < top[i].Totals.CostMicros {
			t.Fatal("the ten most expensive call sites are not ordered")
		}
	}
	share := ledger.DecisionSharePercent()
	if share <= 0 || share >= 100 {
		t.Fatalf("decision share = %d percent", share)
	}

	// The recording adapter writes one line per call without changing the call.
	fake := &agentcost001FakeAdapter{usage: agentmodel.ModelUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CostMicros: 60}}
	var recorded agentmodel.CallLedger
	recorder := agentmodel.NewRecordingAdapter(fake, &recorded, nil)
	ctx := agentmodel.WithCallContext(t.Context(), agentmodel.CallContext{
		TenantID: "t1", AgentID: "policy", TriggerKind: "mention", RunID: "r9",
		CallSite: "persona_run_executor:answer", Stage: agentmodel.CallStageAnswer, Kind: agentmodel.CallKindGeneration,
	})
	result, err := recorder.Invoke(ctx, agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion})
	if err != nil || result.Text != "ok" {
		t.Fatalf("the recording adapter changed the call: %v %+v", err, result)
	}
	got := recorded.Lines()
	if len(got) != 1 || got[0].RunID != "r9" || got[0].InputTokens != 100 || got[0].CostMicros != 60 || got[0].Basis != agentmodel.CostMeasured {
		t.Fatalf("recorded lines = %+v", got)
	}
	// Without a provider figure the line is estimated, never silently zero.
	fake.usage.CostMicros = 0
	estimated := agentmodel.NewRecordingAdapter(fake, &recorded, func(agentmodel.ModelIdentity, agentmodel.ModelUsage) (int64, bool) { return 42, false })
	if _, err := estimated.Invoke(ctx, agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion}); err != nil {
		t.Fatal(err)
	}
	got = recorded.Lines()
	if len(got) != 2 || got[1].CostMicros != 42 || got[1].Basis != agentmodel.CostEstimated {
		t.Fatalf("estimated line = %+v", got)
	}
	// A call without a call context is still recorded, under "unknown", so
	// the totals stay complete and the gap stays visible.
	fake.usage.CostMicros = 60
	if _, err := recorder.Invoke(t.Context(), agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion}); err != nil {
		t.Fatal(err)
	}
	got = recorded.Lines()
	if len(got) != 3 || got[2].CallSite != "unknown" || got[2].TenantID != "unknown" || got[2].AgentID != "unknown" || got[2].RunID != "unknown" {
		t.Fatalf("unknown-context line = %+v", got)
	}
	// A refused line never lands: the ledger holds only validated lines.
	if err := ledger.Append(agentmodel.CallLine{}); err == nil {
		t.Fatal("an empty line was appended")
	}
}

type agentcost001FakeAdapter struct {
	usage agentmodel.ModelUsage
	calls int
}

func (f *agentcost001FakeAdapter) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}}
}

func (f *agentcost001FakeAdapter) Invoke(context.Context, agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	f.calls++
	return agentmodel.ModelResult{Text: "ok", Usage: f.usage, Provider: agentmodel.ModelIdentity{ProviderID: "schemaflux", ModelID: "gpt-6-luna", Version: "2026-09-01"}}, nil
}

// The GOLDEN names the ledger line's fields and the checked-in report.
func TestTodo_AGENTCOST_001_Golden(t *testing.T) {
	raw, err := json.Marshal(agentmodel.CallLine{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tenant_id", "agent_id", "trigger_kind", "run_id", "question_id", "call_site", "stage", "kind", "provider_id", "model_id", "model_version", "input_tokens", "output_tokens", "cached_input_tokens", "cost_micros", "cost_basis", "latency_ms", "started_at", "prompt_digest", "schedule_version"} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("ledger line field %q is missing: %s", want, raw)
		}
	}

	report := RenderAgentcost001Report(Agentcost001SeededLines())
	path := filepath.Join("..", "..", "definitions", "planning", "agentcost001-spend-report.md")
	if os.Getenv("AGENTCOST_GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checked, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(checked), "\r\n", "\n") != report {
		t.Fatal("the checked-in spend report differs from the seeded replay; review it and regenerate with AGENTCOST_GOLDEN_UPDATE=1")
	}
	for _, want := range []string{"per agent", "per trigger", "per answered question", "most expensive call sites", "50", "500", "5,000", "screening", "to-do", "reminder", "triage", "announcement"} {
		if !strings.Contains(strings.ToLower(report), strings.ToLower(want)) {
			t.Fatalf("the report does not cover %q", want)
		}
	}
}

// The INTEGRATION replays the seed twice and requires identical numbers, then
// checks the report's figures against a recomputation from the same lines.
func TestTodo_AGENTCOST_001_Integration(t *testing.T) {
	first, second := Agentcost001SeededLines(), Agentcost001SeededLines()
	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("seeded replay lengths %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("seeded replay line %d differs between runs", i)
		}
	}
	var ledger agentmodel.CallLedger
	for _, line := range first {
		if err := ledger.Append(line); err != nil {
			t.Fatal(err)
		}
	}
	report := RenderAgentcost001Report(first)
	byAgent := ledger.TotalsByAgent()
	for agent, totals := range byAgent {
		if !strings.Contains(report, agent) {
			t.Fatalf("agent %q is missing from the report", agent)
		}
		if totals.CostMicros <= 0 {
			t.Fatalf("agent %q has no spend", agent)
		}
	}
	if got := ledger.DecisionSharePercent(); !strings.Contains(report, decisionShareSentence(got)) {
		t.Fatalf("decision share %d is missing from the report", got)
	}
}

// The REVIEW-CELL leg of AGENTCOST-001: the checked-in report regenerates
// from the review cell's run records, not only from the seeded replay. The
// fixture is the week's runs as sealed run records carrying their per-call
// ledger lines beside their stage checkpoints; the loader rebuilds the exact
// seeded lines from them, so the numbers regenerate from the records.
func TestTodo_AGENTCOST_001_RunRecords(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "agentcost001_review_cell_runs.json"))
	if err != nil {
		t.Fatalf("the review-cell run records fixture is missing: %v", err)
	}
	var runs []runstate.Run
	if err := json.Unmarshal(raw, &runs); err != nil {
		t.Fatalf("the run records do not parse as sealed runs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("the run records fixture holds no runs")
	}
	for _, run := range runs {
		if len(run.Calls) == 0 || len(run.Checkpoints) == 0 {
			t.Fatalf("run %q seals no calls or no checkpoints: %+v", run.ID, run)
		}
	}
	lines, err := Agentcost001LinesFromRunRecords(runs)
	if err != nil {
		t.Fatalf("run records do not rebuild ledger lines: %v", err)
	}
	seed := Agentcost001SeededLines()
	if len(lines) != len(seed) {
		t.Fatalf("run records rebuild %d lines, the seeded replay has %d", len(lines), len(seed))
	}
	for i := range seed {
		if lines[i] != seed[i] {
			t.Fatalf("rebuilt line %d = %+v, seeded %+v", i, lines[i], seed[i])
		}
	}
	if RenderAgentcost001Report(lines) != RenderAgentcost001Report(seed) {
		t.Fatal("the report built from run records differs from the seeded report")
	}

	// Edge cases: no runs rebuild no lines; a run without calls is skipped;
	// a sealed line that no longer validates, or one sealed under the wrong
	// run, fails closed instead of entering the report.
	if rebuilt, err := Agentcost001LinesFromRunRecords(nil); err != nil || len(rebuilt) != 0 {
		t.Fatalf("no runs rebuilt %+v, %v", rebuilt, err)
	}
	empty := runstate.Run{ID: "r-empty", TenantID: "review-cell", AgentID: "policy-helper"}
	if rebuilt, err := Agentcost001LinesFromRunRecords([]runstate.Run{empty}); err != nil || len(rebuilt) != 0 {
		t.Fatalf("a run without calls rebuilt %+v, %v", rebuilt, err)
	}
	broken := seed[0]
	broken.CostMicros = -1
	if _, err := Agentcost001LinesFromRunRecords([]runstate.Run{{ID: broken.RunID, TenantID: broken.TenantID, AgentID: broken.AgentID, Calls: []agentmodel.CallLine{broken}}}); err == nil {
		t.Fatal("an invalid sealed line entered the report")
	}
	foreign := seed[0]
	foreign.RunID = "another-run"
	if _, err := Agentcost001LinesFromRunRecords([]runstate.Run{{ID: seed[0].RunID, TenantID: foreign.TenantID, AgentID: foreign.AgentID, Calls: []agentmodel.CallLine{foreign}}}); err == nil {
		t.Fatal("a line sealed under the wrong run entered the report")
	}
}
