package leave

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type stubHoursProvider struct {
	hours values.Decimal
	calls int
	from  values.LocalDate
	to    values.LocalDate
}

func (s *stubHoursProvider) WorkedHours(_ context.Context, worker string, from, to values.LocalDate) (values.Decimal, error) {
	s.calls++
	s.from, s.to = from, to
	if worker == "" {
		return values.Decimal{}, errors.New("worker is required")
	}
	return s.hours, nil
}

func mustProtectedDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal %s: %v", text, err)
	}
	return d
}

func mustProtectedDate(t *testing.T, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("date %s: %v", s, err)
	}
	return d
}

func mustProtectedInstant(t *testing.T, s string) values.Instant {
	t.Helper()
	moment, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("instant %s: %v", s, err)
	}
	return values.NewInstant(moment)
}

// TestTodo_WF_CAP_005 proves the PRIMARY matrix of WF-CAP-005: protected
// leave eligibility reads provider-aggregated hours over a twelve-month
// lookback instead of caller-supplied facts, a rolling-backward period kind
// exists, each intermittent episode draws hours, and certification due dates
// plus recertification triggers come from rule packs.
func TestTodo_WF_CAP_005(t *testing.T) {
	pack := FederalCertificationPack()
	if err := pack.Validate(); err != nil {
		t.Fatalf("federal pack: %v", err)
	}
	provider := &stubHoursProvider{hours: mustProtectedDecimal(t, "1320.50")}
	asOf := mustProtectedDate(t, "2026-06-30")
	window, err := LookbackWindow(asOf, 12)
	if err != nil {
		t.Fatalf("LookbackWindow: %v", err)
	}
	if window.From.String() != "2025-06-30" || window.To.String() != "2026-06-30" {
		t.Fatalf("lookback window = %s..%s, want 2025-06-30..2026-06-30", window.From, window.To)
	}
	hours, err := ProviderHours(context.Background(), provider, "worker:w1", window)
	if err != nil {
		t.Fatalf("ProviderHours: %v", err)
	}
	if provider.calls != 1 || provider.from != window.From || provider.to != window.To {
		t.Fatalf("provider was not consulted over the lookback: %+v", provider)
	}
	verdict := EvaluateLookbackEligibility(hours, 14, pack)
	if verdict.Result != ProgramEligible {
		t.Fatalf("eligibility = %s %+v, want ELIGIBLE", verdict.Result, verdict.Blockers)
	}
	short := EvaluateLookbackEligibility(mustProtectedDecimal(t, "900.00"), 14, pack)
	if short.Result != ProgramIneligible || len(short.Blockers) == 0 {
		t.Fatalf("short-hours eligibility = %s %+v, want INELIGIBLE with blockers", short.Result, short.Blockers)
	}
	junior := EvaluateLookbackEligibility(hours, 6, pack)
	if junior.Result != ProgramIneligible {
		t.Fatalf("short-service eligibility = %s, want INELIGIBLE", junior.Result)
	}

	rolling, err := balance.RollingBackwardWindow(mustProtectedInstant(t, "2026-06-30T00:00:00Z"), 12, "fmla-2026", "2026.1")
	if err != nil {
		t.Fatalf("RollingBackwardWindow: %v", err)
	}
	if rolling.Kind != balance.PeriodRollingBackward {
		t.Fatalf("rolling kind = %s", rolling.Kind)
	}
	if err := rolling.Validate(); err != nil {
		t.Fatalf("rolling period: %v", err)
	}
	def := balance.AccumulatorDefinition{ID: "fmla-entitlement", Version: "2026.1", Name: "FMLA entitlement", Unit: "HOUR", Currency: "XXX", Subject: "worker", Period: balance.PeriodRollingBackward,
		Dimensions: []balance.BalanceDimension{{Name: "worker_id", ValueType: "worker_id", Required: true}}, EntryTypes: []string{"GRANT", "DRAW"},
		Authority: balance.Authority{SourceID: "leave", Version: "2026.1"},
		Floor:     balance.PolicyRule{Kind: balance.RuleNone, Version: "2026.1"}, Cap: balance.PolicyRule{Kind: balance.RuleConfigured, Version: "2026.1", Value: "480"},
		Expiry: balance.PolicyRule{Kind: balance.RuleConfigured, Version: "2026.1", Value: "rolling"}, Rollover: balance.PolicyRule{Kind: balance.RuleNone, Version: "2026.1"}, Correction: balance.PolicyRule{Kind: balance.RuleConfigured, Version: "2026.1", Value: "supervisor"}}
	if _, err := balance.Publish(def); err != nil {
		t.Fatalf("publish rolling accumulator: %v", err)
	}

	episodes := []IntermittentEpisode{
		{EpisodeID: "ep-1", WorkerID: "worker:w1", Start: mustProtectedDate(t, "2026-07-06"), End: mustProtectedDate(t, "2026-07-06"), Hours: mustProtectedDecimal(t, "8.00")},
		{EpisodeID: "ep-2", WorkerID: "worker:w1", Start: mustProtectedDate(t, "2026-07-08"), End: mustProtectedDate(t, "2026-07-09"), Hours: mustProtectedDecimal(t, "12.00")},
	}
	draws, err := DrawEpisodes("worker:w1", episodes)
	if err != nil {
		t.Fatalf("DrawEpisodes: %v", err)
	}
	if len(draws) != 2 || draws[0].IdempotencyKey != "leavedraw:worker:w1:ep-1" {
		t.Fatalf("episode draws = %+v", draws)
	}
	if _, err := DrawEpisodes("worker:w1", append(episodes, episodes[0])); !errors.Is(err, ErrDuplicateEpisode) {
		t.Fatalf("duplicate episode = %v, want ErrDuplicateEpisode", err)
	}

	due, err := CertificationDue(mustProtectedDate(t, "2026-01-05"), pack)
	if err != nil {
		t.Fatalf("CertificationDue: %v", err)
	}
	if due.String() != "2026-01-20" {
		t.Fatalf("certification due = %s, want 2026-01-20", due)
	}
	cert := Certification{Issued: mustProtectedDate(t, "2026-01-05"), StatedDurationDays: 60}
	triggers := RecertificationTriggers(cert, mustProtectedDate(t, "2026-02-10"), 3, pack)
	if len(triggers) != 1 || triggers[0] != TriggerIntervalElapsed {
		t.Fatalf("recert triggers = %v, want [INTERVAL_ELAPSED]", triggers)
	}
	late := RecertificationTriggers(cert, mustProtectedDate(t, "2026-03-10"), 0, pack)
	joined := strings.Join(triggerNames(late), ",")
	if !strings.Contains(joined, string(TriggerDurationExceeded)) {
		t.Fatalf("recert triggers = %v, want DURATION_EXCEEDED", late)
	}
}

// TestTodo_WF_CAP_005_Property proves the rolling window, draw keys and
// certification timing behave as total functions over their inputs.
func TestTodo_WF_CAP_005_Property(t *testing.T) {
	pack := FederalCertificationPack()
	asOf := mustProtectedDate(t, "2026-03-31")
	window, err := LookbackWindow(asOf, 12)
	if err != nil {
		t.Fatal(err)
	}
	if window.From.String() != "2025-03-31" {
		t.Fatalf("month-end lookback from = %s", window.From)
	}
	leap, err := LookbackWindow(mustProtectedDate(t, "2024-02-29"), 12)
	if err != nil {
		t.Fatal(err)
	}
	if leap.From.String() != "2023-02-28" || leap.To.String() != "2024-02-29" {
		t.Fatalf("leap lookback = %s..%s", leap.From, leap.To)
	}
	if _, err := LookbackWindow(asOf, 0); !errors.Is(err, ErrInvalidLookback) {
		t.Fatalf("zero-month lookback = %v, want ErrInvalidLookback", err)
	}
	episodes := []IntermittentEpisode{
		{EpisodeID: "ep-a", WorkerID: "worker:w1", Start: asOf, End: asOf, Hours: mustProtectedDecimal(t, "4.00")},
		{EpisodeID: "ep-b", WorkerID: "worker:w1", Start: asOf, End: asOf, Hours: mustProtectedDecimal(t, "4.00")},
	}
	first, err := DrawEpisodes("worker:w1", episodes)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DrawEpisodes("worker:w1", episodes)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].IdempotencyKey == first[1].IdempotencyKey || first[0].IdempotencyKey != second[0].IdempotencyKey {
		t.Fatalf("draw keys are not unique and stable: %+v", first)
	}
	earlier, _ := CertificationDue(mustProtectedDate(t, "2026-01-05"), pack)
	later, _ := CertificationDue(mustProtectedDate(t, "2026-01-06"), pack)
	if earlier.Compare(later) >= 0 {
		t.Fatalf("certification dues are not monotone: %s vs %s", earlier, later)
	}
	cert := Certification{Issued: mustProtectedDate(t, "2026-01-05"), StatedDurationDays: 45}
	feb := RecertificationTriggers(cert, mustProtectedDate(t, "2026-02-01"), 0, pack)
	apr := RecertificationTriggers(cert, mustProtectedDate(t, "2026-04-01"), 0, pack)
	if len(apr) < len(feb) {
		t.Fatalf("triggers shrank over time: %v then %v", feb, apr)
	}
}

// TestTodo_WF_CAP_005_Golden pins the federal rule pack computation.
func TestTodo_WF_CAP_005_Golden(t *testing.T) {
	pack := FederalCertificationPack()
	summary, err := GoldenCertificationSummary(pack, mustProtectedDate(t, "2026-01-05"), Certification{Issued: mustProtectedDate(t, "2026-01-05"), StatedDurationDays: 60}, mustProtectedDate(t, "2026-02-10"))
	if err != nil {
		t.Fatal(err)
	}
	if summary.InitialDue != "2026-01-20" || summary.RecertDue != "2026-02-04" || len(summary.Triggers) != 1 || summary.Triggers[0] != string(TriggerIntervalElapsed) {
		t.Fatalf("golden summary = %+v", summary)
	}
	const want = "sha256:68b6138dd1ea554d68e64d2208b6e98a3cf865fc82edae732373a9dc31762e2f"
	if summary.Digest != want {
		t.Fatalf("golden digest = %q, want %q", summary.Digest, want)
	}
}
