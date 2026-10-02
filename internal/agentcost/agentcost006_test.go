package agentcost

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type fakeOwners struct {
	owners map[string]bool // tenant/agent/actor
	admins map[string]bool // tenant/actor
}

func (f fakeOwners) IsOwner(tenant, agent, actor string) bool {
	return f.owners[tenant+"/"+agent+"/"+actor]
}
func (f fakeOwners) IsAdmin(tenant, actor string) bool { return f.admins[tenant+"/"+actor] }

type fakeNotifier struct{ sent []string }

func (n *fakeNotifier) Notify(tenant, owner, message string) {
	n.sent = append(n.sent, tenant+"|"+owner+"|"+message)
}

type fixture struct {
	gate     *Gate
	notifier *fakeNotifier
	clock    *time.Time
	owners   fakeOwners
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	owners := fakeOwners{
		owners: map[string]bool{"t1/policy/olive": true, "t1/benefits/bob": true, "t2/policy/olive": true},
		admins: map[string]bool{"t1/admin": true},
	}
	notifier := &fakeNotifier{}
	gate, err := NewGate(owners, notifier, time.UTC, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{gate: gate, notifier: notifier, clock: &clock, owners: owners}
}

func policy(conversation string) Subject {
	return Subject{TenantID: "t1", AgentID: "policy", AgentName: "Policy Helper", ConversationID: conversation, ConversationLabel: "#" + conversation, EstimateMicros: 100_000}
}

// Limits in runs and in money, per agent and per conversation, are enforced
// before the call; the sentence says what happened and when it resets; the
// owner hears at 80 percent, once.
func TestTodo_AGENTCOST_006(t *testing.T) {
	f := newFixture(t)
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 5, MaxSpendMicros: 2_000_000}); err != nil {
		t.Fatal(err)
	}
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", ConversationID: "general", MaxRuns: 2}); err != nil {
		t.Fatal(err)
	}
	// Two runs in #general reach its conversation limit; the owner is told at
	// 80 percent of it (the second run is 100 percent, the first 50).
	for i := 0; i < 2; i++ {
		if decision := f.gate.Admit(policy("general")); !decision.Allowed {
			t.Fatalf("run %d refused: %+v", i, decision)
		}
		f.gate.Record(policy("general"), 100_000, "olive")
	}
	decision := f.gate.Admit(policy("general"))
	want := "Policy Helper reached today's limit for #general. It resets at 00:00."
	if decision.Allowed || decision.Reached != want {
		t.Fatalf("decision = %+v, want %q", decision, want)
	}
	// The agent still answers elsewhere: the conversation's limit is its own.
	if decision := f.gate.Admit(policy("random")); !decision.Allowed {
		t.Fatalf("another conversation was stopped: %+v", decision)
	}
	if len(f.notifier.sent) != 1 || !strings.Contains(f.notifier.sent[0], "t1|olive|Policy Helper in #general has used 80 percent of today's limit.") {
		t.Fatalf("notifications = %v", f.notifier.sent)
	}
	// Five runs have now used the agent's day; the sixth would pass it.
	f.gate.Record(policy("random"), 100_000, "olive")
	f.gate.Record(policy("random"), 100_000, "olive")
	f.gate.Record(policy("random"), 100_000, "olive")
	if decision := f.gate.Admit(policy("random")); decision.Allowed || !strings.HasPrefix(decision.Reached, "Policy Helper reached today's limit.") {
		t.Fatalf("the agent's day limit did not stop it: %+v", decision)
	}
	// The next day starts fresh.
	*f.clock = f.clock.Add(24 * time.Hour)
	if decision := f.gate.Admit(policy("general")); !decision.Allowed {
		t.Fatalf("the limit did not reset: %+v", decision)
	}

	// Spend limit: an estimate that would pass it is refused before the call.
	f2 := newFixture(t)
	if err := f2.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxSpendMicros: 250_000}); err != nil {
		t.Fatal(err)
	}
	f2.gate.Record(policy(""), 200_000, "olive")
	if decision := f2.gate.Admit(policy("")); decision.Allowed {
		t.Fatalf("a call that would pass the money limit was allowed: %+v", decision)
	}
	// An agent without a limit is never stopped.
	if decision := f2.gate.Admit(Subject{TenantID: "t1", AgentID: "benefits", EstimateMicros: 9_000_000_000}); !decision.Allowed {
		t.Fatalf("no limit, yet stopped: %+v", decision)
	}
	// The day follows the tenant's zone: 00:00 there, not in UTC.
	zone := time.FixedZone("EDT", -4*3600)
	g3, _ := NewGate(f.owners, nil, zone, func() time.Time { return time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC) })
	_ = g3.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 1})
	g3.Record(policy(""), 1, "olive")
	if decision := g3.Admit(policy("")); !strings.HasSuffix(decision.Reached, "It resets at 00:00.") || decision.ResetsAt.UTC().Hour() != 4 {
		t.Fatalf("reset in the tenant's zone = %+v", decision)
	}
}

// Only an owner can raise or remove a limit; a change is audited; cost never
// shows another owner's or another tenant's spend.
func TestTodo_AGENTCOST_006_Security(t *testing.T) {
	f := newFixture(t)
	base := Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 10, MaxSpendMicros: 5_000_000}
	if err := f.gate.Set("bob", base); err != ErrDenied {
		t.Fatalf("a non-owner set a limit: %v", err)
	}
	if err := f.gate.Set("olive", base); err != nil {
		t.Fatal(err)
	}
	raise := base
	raise.MaxRuns = 50
	for _, actor := range []string{"bob", "admin", "stranger"} {
		if err := f.gate.Set(actor, raise); err != ErrDenied {
			t.Fatalf("%s raised a limit: %v", actor, err)
		}
	}
	if err := f.gate.Set("admin", Limit{TenantID: "t1", AgentID: "policy"}); err != ErrDenied {
		t.Fatalf("an administrator removed a limit: %v", err)
	}
	// An administrator may tighten, never loosen.
	lower := base
	lower.MaxRuns = 4
	if err := f.gate.Set("admin", lower); err != nil {
		t.Fatalf("an administrator could not lower a limit: %v", err)
	}
	loosenOne := lower
	loosenOne.MaxSpendMicros = 9_000_000
	if err := f.gate.Set("admin", loosenOne); err != ErrDenied {
		t.Fatalf("an administrator raised the money limit while lowering runs: %v", err)
	}
	// Another tenant's owner of a same-named agent cannot touch this one.
	other := base
	other.TenantID = "t2"
	if err := f.gate.Set("olive", other); err != nil {
		t.Fatal(err)
	}
	limits, err := f.gate.Limits("t1", "policy", "olive")
	if err != nil || len(limits) != 1 || limits[0].MaxRuns != 4 {
		t.Fatalf("limits = %+v, %v", limits, err)
	}
	if _, err := f.gate.Limits("t1", "policy", "bob"); err != ErrDenied {
		t.Fatalf("a non-owner read limits: %v", err)
	}
	trail, err := f.gate.AuditTrail("t1", "policy", "olive")
	if err != nil || len(trail) != 2 || trail[0].Before != nil || trail[0].After.MaxRuns != 10 || trail[1].Actor != "admin" || trail[1].Before.MaxRuns != 10 || trail[1].After.MaxRuns != 4 {
		t.Fatalf("audit trail = %+v, %v", trail, err)
	}
	if _, err := f.gate.AuditTrail("t1", "policy", "bob"); err != ErrDenied {
		t.Fatalf("a non-owner read the audit trail: %v", err)
	}

	// Cost: each owner sees only what their agents cost, in their tenant.
	var ledger Ledger
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "r1", At: now, Kind: KindAnswer, SpendMicros: 300_000, Answered: true})
	ledger.Add(Run{TenantID: "t1", AgentID: "benefits", RunID: "r2", At: now, Kind: KindAnswer, SpendMicros: 700_000, Answered: true})
	ledger.Add(Run{TenantID: "t2", AgentID: "policy", RunID: "r3", At: now, Kind: KindAnswer, SpendMicros: 9_000_000, Answered: true})
	report := ledger.Report(f.owners, "t1", "olive", now, time.UTC)
	if report.MonthToDateMicros != 300_000 || len(report.RecentRuns) != 1 || report.RecentRuns[0].RunID != "r1" {
		t.Fatalf("olive's report leaks another owner's or tenant's spend: %+v", report)
	}
	if bobs := ledger.Report(f.owners, "t1", "bob", now, time.UTC); bobs.MonthToDateMicros != 700_000 {
		t.Fatalf("bob's report = %+v", bobs)
	}
	if nobody := ledger.Report(f.owners, "t1", "stranger", now, time.UTC); nobody.MonthToDateMicros != 0 || len(nobody.RecentRuns) != 0 {
		t.Fatalf("a stranger sees spend: %+v", nobody)
	}
}

// Agent operations' figures: cost per answered question, the thirty-day
// trend, the screening share, ambient cost per hundred messages and the
// month's forecast, from a ledger of finished runs.
func TestTodo_AGENTCOST_006_Integration(t *testing.T) {
	f := newFixture(t)
	var ledger Ledger
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC) // day 10 of 31
	for i := 0; i < 4; i++ {
		ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "a" + string(rune('0'+i)), At: now.Add(-time.Duration(i) * 24 * time.Hour), Kind: KindAnswer, SpendMicros: 500_000, Answered: true})
	}
	ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "s1", At: now, Kind: KindScreening, SpendMicros: 400_000, MessagesRead: 200})
	ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "d1", At: now, Kind: KindDecision, SpendMicros: 600_000, MessagesRead: 200})
	ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "old", At: now.AddDate(0, 0, -45), Kind: KindAnswer, SpendMicros: 5_000_000, Answered: true})
	report := ledger.Report(f.owners, "t1", "olive", now, time.UTC)

	if len(report.Trend) != 30 || report.Trend[29].Day != "2026-10-10" || report.Trend[0].Day != "2026-09-11" {
		t.Fatalf("trend = %d days from %s to %s", len(report.Trend), report.Trend[0].Day, report.Trend[len(report.Trend)-1].Day)
	}
	if report.Trend[29].SpendMicros != 1_500_000 || report.Trend[29].Runs != 3 {
		t.Fatalf("today = %+v", report.Trend[29])
	}
	// Answers: 4 x 0.50 + the old one 5.00 = 7.00 over 5 answered questions.
	if report.PerAnsweredQuestionMicros != 1_400_000 {
		t.Fatalf("per answered question = %d", report.PerAnsweredQuestionMicros)
	}
	// Screening and decisions: 1.00 of 8.00 total, 12.5 rounded up.
	if report.QuietSharePercent != 13 {
		t.Fatalf("quiet share = %d", report.QuietSharePercent)
	}
	// Ambient: 1.00 spent reading 400 messages = 0.25 per hundred.
	if report.PerHundredMessagesMicros != 250_000 {
		t.Fatalf("per hundred messages = %d", report.PerHundredMessagesMicros)
	}
	// Month so far: three answers on 8, 9, 10 Oct (1.5) is not all of it: runs
	// on 7 Oct count too, so 4 x 0.50 + 1.00 = 3.00; forecast 3.00 x 31 / 10.
	if report.MonthToDateMicros != 3_000_000 || report.ForecastMonthMicros != 9_300_000 {
		t.Fatalf("month to date %d forecast %d", report.MonthToDateMicros, report.ForecastMonthMicros)
	}
	if len(report.PerAgentDay) == 0 || report.PerAgentDay[0].Day != "2026-10-10" || report.PerAgentDay[0].AgentID != "policy" {
		t.Fatalf("per agent day = %+v", report.PerAgentDay)
	}
}

func renderCost(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer("&#39;", "'", "&amp;", "&").Replace(markup)
}

type fakeSave struct {
	agent       string
	runs, micro int64
	err         error
}

func (f *fakeSave) SaveSpendLimit(agentID string, runs, micros int64) error {
	f.agent, f.runs, f.micro = agentID, runs, micros
	return f.err
}

// The card and the panel as the page draws them, in three languages, in every
// state: ready, empty, loading, failed with Try again, limit reached.
func TestTodo_AGENTCOST_006_Browser(t *testing.T) {
	f := newFixture(t)
	var ledger Ledger
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	ledger.Add(Run{TenantID: "t1", AgentID: "policy", RunID: "a1", At: now, Kind: KindAnswer, SpendMicros: 500_000, Answered: true})
	report := ledger.Report(f.owners, "t1", "olive", now, time.UTC)
	view := productui.AgentCostReport{PerAnsweredQuestionMicros: report.PerAnsweredQuestionMicros, MonthToDateMicros: report.MonthToDateMicros, ForecastMonthMicros: report.ForecastMonthMicros, QuietSharePercent: report.QuietSharePercent}
	for _, day := range report.Trend {
		view.Trend = append(view.Trend, productui.AgentCostDay{Day: day.Day, Micros: day.SpendMicros, Runs: day.Runs})
	}
	for _, line := range report.RecentRuns {
		view.Runs = append(view.Runs, productui.AgentCostRun{Agent: line.AgentID, When: line.At.Format("2006-01-02 15:04"), Kind: string(line.Kind), Micros: line.SpendMicros})
	}
	for _, row := range report.PerAgentDay {
		view.PerAgentDay = append(view.PerAgentDay, productui.AgentCostAgentDay{Agent: row.AgentID, Day: row.Day, Micros: row.SpendMicros})
	}

	english := productui.I18nProps{Locale: productui.ResolveProductLocale("en-US")}
	ready := renderCost(t, productui.AgentCostPanel(productui.AgentCostPanelProps{I18nProps: english, State: productui.AgentAccessStateReady, Report: view}))
	for _, want := range []string{`data-agent-cost-state="ready"`, "What your agents cost", "This month so far", "Forecast for the month", "Per answered question", "Spent on screening and decisions", "Last 30 days", "Each agent per day", "Latest runs", "$0.50", "2026-10-10", "<table"} {
		if !strings.Contains(ready, want) {
			t.Errorf("cost panel missing %q:\n%s", want, ready)
		}
	}
	for state, want := range map[productui.AgentAccessLoadState][]string{
		productui.AgentAccessStateLoading:     {"Loading costs"},
		productui.AgentAccessStateUnavailable: {"Costs could not be loaded.", "Try again"},
	} {
		markup := renderCost(t, productui.AgentCostPanel(productui.AgentCostPanelProps{I18nProps: english, State: state}))
		for _, text := range want {
			if !strings.Contains(markup, text) {
				t.Errorf("%s state missing %q:\n%s", state, text, markup)
			}
		}
	}
	if empty := renderCost(t, productui.AgentCostPanel(productui.AgentCostPanelProps{I18nProps: english, State: productui.AgentAccessStateReady})); !strings.Contains(empty, "No agent runs yet.") {
		t.Errorf("empty state missing: %s", empty)
	}
	for locale, want := range map[string][]string{"de-DE": {"Was Ihre Agenten kosten", "Letzte 30 Tage"}, "ar": {"تكلفة وكلائك", "آخر 30 يوماً"}} {
		markup := renderCost(t, productui.AgentCostPanel(productui.AgentCostPanelProps{I18nProps: productui.I18nProps{Locale: productui.ResolveProductLocale(locale)}, State: productui.AgentAccessStateReady, Report: view}))
		for _, text := range want {
			if !strings.Contains(markup, text) {
				t.Errorf("%s panel missing %q", locale, text)
			}
		}
		if strings.Contains(markup, "What your agents cost") {
			t.Errorf("%s panel is in English", locale)
		}
	}

	save := &fakeSave{}
	card := renderCost(t, productui.AgentSpendLimitsCard(productui.AgentSpendLimitsProps{I18nProps: english, AgentID: "policy", AgentName: "Policy Helper", MaxRunsPerDay: 20, MaxSpendMicrosPerDay: 5_000_000, Reached: "Policy Helper reached today's limit for #general. It resets at 00:00.", Client: save}))
	for _, want := range []string{"Spend limits", "Runs per day", "Spend per day (US dollars)", `value="20"`, `value="5.00"`, "Save limits", `data-limit-reached="true"`, "Policy Helper reached today's limit for #general. It resets at 00:00."} {
		if !strings.Contains(card, want) {
			t.Errorf("limits card missing %q:\n%s", want, card)
		}
	}
	if strings.Count(card, `class="button primary"`) != 1 {
		t.Errorf("the card has more than one primary action:\n%s", card)
	}
	if labelAt, inputAt := strings.Index(card, "Runs per day"), strings.Index(card, `id="agent-spend-policy-runs"`); labelAt < 0 || inputAt < labelAt {
		t.Errorf("the label does not come before its field:\n%s", card)
	}
	none := renderCost(t, productui.AgentSpendLimitsCard(productui.AgentSpendLimitsProps{I18nProps: english, AgentID: "policy"}))
	if !strings.Contains(none, "No limits set.") || !strings.Contains(none, "disabled") {
		t.Errorf("a card with no service should say no limits and disable Save:\n%s", none)
	}
}
