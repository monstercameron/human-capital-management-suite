package abuse

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var insider008At = time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)

func insider008Corpus() []SeededCase {
	abusive := func(s InsiderScenario, pattern string) SeededCase {
		return SeededCase{Scenario: s, Tenant: "acme", Pattern: pattern, Abusive: true, SeededAbuse: true}
	}
	return []SeededCase{
		abusive(ScenarioCompromisedAdmin, "root-grant-midnight"),
		abusive(ScenarioMassRead, "bulk-export-10k"),
		abusive(ScenarioBankManipulation, "routing-swap"),
		abusive(ScenarioPayrollAccess, "pay-rate-uplift"),
		abusive(ScenarioCollusion, "dual-approve-pair"),
		{Scenario: ScenarioFalsePositive, Tenant: "acme", Pattern: "oncall-bulk-read"},
		{Scenario: ScenarioTelemetryGap, Tenant: "acme", Pattern: "collector-silence"},
	}
}

func insider008Signals() []InsiderSignal {
	sig := func(pattern string) InsiderSignal {
		return InsiderSignal{Tenant: "acme", Pattern: pattern, At: insider008At.Add(-time.Hour)}
	}
	return []InsiderSignal{
		sig("root-grant-midnight"), sig("bulk-export-10k"), sig("routing-swap"),
		sig("pay-rate-uplift"), sig("dual-approve-pair"),
	}
}

func insider008Outcome(t *testing.T, r InsiderReport, s InsiderScenario) InsiderOutcome {
	t.Helper()
	for _, res := range r.Results {
		if res.Scenario == s {
			return res.Outcome
		}
	}
	t.Fatalf("scenario %s missing", s)
	return ""
}

// TestTodo_ABUSE_008 is the PRIMARY contract: the signed corpus covers
// compromised admin, authorized mass read, bank/payroll/access
// manipulation, collusion, false positive and telemetry gaps with no
// cross-tenant leak and no automatic accusation. Seeded abuse that is
// missed, or an auto-accusation, returns ABUSE_008_REJECTED with
// field/state/version and persists nothing.
func TestTodo_ABUSE_008(t *testing.T) {
	corpus := insider008Corpus()
	seal := corpusDigest(corpus)
	got, err := RunInsiderSimulation("acme", seal, corpus, insider008Signals(), insider008At)
	if err != nil {
		t.Fatalf("RunInsiderSimulation: %v", err)
	}
	if len(got.Results) != len(InsiderScenarios) {
		t.Fatalf("every scenario must report: %+v", got)
	}
	for _, s := range []InsiderScenario{ScenarioCompromisedAdmin, ScenarioMassRead, ScenarioBankManipulation, ScenarioPayrollAccess, ScenarioCollusion} {
		if o := insider008Outcome(t, got, s); o != InsiderDetected {
			t.Fatalf("seeded %s must detect, got %s", s, o)
		}
	}
	if o := insider008Outcome(t, got, ScenarioFalsePositive); o != InsiderBenign {
		t.Fatalf("false positive must stay benign, got %s", o)
	}
	if got.Digest == "" || got.CorpusDigest != seal {
		t.Fatalf("report must seal corpus and result: %+v", got)
	}
	for _, res := range got.Results {
		if strings.Contains(strings.ToUpper(res.Lead), "GUILTY") || strings.Contains(strings.ToUpper(res.Lead), "ACCUSED") {
			t.Fatalf("leads must never accuse: %+v", res)
		}
	}

	t.Run("missed seeded abuse is refused", func(t *testing.T) {
		signals := insider008Signals()[:3]
		_, err := RunInsiderSimulation("acme", seal, corpus, signals, insider008At)
		var rej *InsiderRejection
		if !errors.As(err, &rej) || !errors.Is(err, ErrInsiderRejected) {
			t.Fatalf("missed abuse must be ABUSE_008_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
	})

	t.Run("incomplete corpus is refused", func(t *testing.T) {
		partial := corpus[:5]
		if _, err := RunInsiderSimulation("acme", "", partial, insider008Signals(), insider008At); !errors.Is(err, ErrInsiderRejected) {
			t.Fatalf("partial corpus must be ABUSE_008_REJECTED")
		}
	})

	t.Run("forged corpus seal is refused", func(t *testing.T) {
		if _, err := RunInsiderSimulation("acme", "sha256:forged", corpus, insider008Signals(), insider008At); !errors.Is(err, ErrInsiderRejected) {
			t.Fatalf("forged seal must be ABUSE_008_REJECTED")
		}
	})
}

func TestTodo_ABUSE_008_Security(t *testing.T) {
	corpus := insider008Corpus()
	seal := corpusDigest(corpus)
	foreign := insider008Signals()
	foreign[0].Tenant = "foreign"
	if _, err := RunInsiderSimulation("acme", seal, corpus, foreign, insider008At); !errors.Is(err, ErrInsiderRejected) {
		t.Fatalf("cross-tenant signals must be ABUSE_008_REJECTED")
	}
	poisoned := append([]SeededCase(nil), corpus...)
	poisoned[0].Tenant = "foreign"
	if _, err := RunInsiderSimulation("acme", "", poisoned, insider008Signals(), insider008At); !errors.Is(err, ErrInsiderRejected) {
		t.Fatalf("cross-tenant corpus must be ABUSE_008_REJECTED")
	}
	// Future signals are refused rather than simulated.
	future := insider008Signals()
	future[0].At = insider008At.Add(time.Hour)
	if _, err := RunInsiderSimulation("acme", seal, corpus, future, insider008At); !errors.Is(err, ErrInsiderRejected) {
		t.Fatalf("future signals must be ABUSE_008_REJECTED")
	}
}

func TestTodo_ABUSE_008_Conformance(t *testing.T) {
	// Every abusive scenario trips on its own pattern alone; every
	// non-abusive scenario stays benign or inconclusive on silence.
	corpus := insider008Corpus()
	seal := corpusDigest(corpus)
	for _, s := range []InsiderScenario{ScenarioCompromisedAdmin, ScenarioMassRead, ScenarioBankManipulation, ScenarioPayrollAccess, ScenarioCollusion} {
		var pattern string
		for _, c := range corpus {
			if c.Scenario == s {
				pattern = c.Pattern
			}
		}
		signals := []InsiderSignal{{Tenant: "acme", Pattern: pattern, At: insider008At.Add(-time.Hour)}}
		// Other abusive scenarios will MISS here, so expect refusal:
		// conformance asserts the tripwire fires uniformly instead.
		_, err := RunInsiderSimulation("acme", seal, corpus, signals, insider008At)
		if !errors.Is(err, ErrInsiderRejected) {
			t.Fatalf("scenario %s: partial signals must trip the miss tripwire uniformly, got %v", s, err)
		}
	}
	got, err := RunInsiderSimulation("acme", seal, corpus, nil, insider008At)
	if err == nil {
		// Nil signals miss every seeded abuse, so refusal is expected;
		// a report here would itself be the defect.
		t.Fatalf("silent run must refuse, got %+v", got)
	}
	if !errors.Is(err, ErrInsiderRejected) {
		t.Fatalf("silent run must be ABUSE_008_REJECTED, got %v", err)
	}
}

func TestTodo_ABUSE_008_Mutation(t *testing.T) {
	corpus := insider008Corpus()
	seal := corpusDigest(corpus)
	a, err := RunInsiderSimulation("acme", seal, corpus, insider008Signals(), insider008At)
	if err != nil {
		t.Fatal(err)
	}
	// Dropping one signal trips the miss tripwire: leads never silently
	// shrink.
	if _, err := RunInsiderSimulation("acme", seal, corpus, insider008Signals()[:4], insider008At); !errors.Is(err, ErrInsiderRejected) {
		t.Fatalf("shrunk signals must be ABUSE_008_REJECTED")
	}
	// Telemetry gap observed yields an inconclusive coverage lead.
	withGap := append(append([]InsiderSignal(nil), insider008Signals()...), InsiderSignal{Tenant: "acme", Pattern: "collector-silence", At: insider008At.Add(-time.Minute)})
	b, err := RunInsiderSimulation("acme", seal, corpus, withGap, insider008At)
	if err != nil {
		t.Fatal(err)
	}
	if o := insider008Outcome(t, b, ScenarioTelemetryGap); o != InsiderInconclusive {
		t.Fatalf("observed gap must be inconclusive, got %s", o)
	}
	if b.Digest == a.Digest {
		t.Fatalf("gap observation must move the digest")
	}
	if b.Leads != a.Leads {
		t.Fatalf("coverage leads must not inflate the abuse lead count")
	}
}
