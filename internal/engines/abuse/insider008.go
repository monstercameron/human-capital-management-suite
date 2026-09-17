// ABUSE-008: run insider-risk abuse simulations.
//
// RunInsiderSimulation replays a signed corpus of insider-risk scenarios
// — compromised admin, authorized mass read, bank/payroll/access
// manipulation, collusion, false positive and telemetry gaps — against
// observed signals. Seeded abuse that is missed, or any automatic
// accusation, fails with ABUSE_008_REJECTED. Verdicts are investigative
// leads for human review, never accusations, and signals never cross
// tenants. The simulation is kernel-pure and persists nothing.
package abuse

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// InsiderVersion is the rejection version for ABUSE-008.
const InsiderVersion = "abuse-insider/v1"

var (
	// ErrInsiderRejected is the ABUSE-008 seeded-defect sentinel. A
	// seeded privileged or sensitive-data abuse that is missed, or an
	// automatic accusation, fails with this error carrying the offending
	// field, state and version.
	ErrInsiderRejected = errors.New("ABUSE_008_REJECTED")
)

// InsiderRejection is the stable ABUSE-008 failure shape.
type InsiderRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *InsiderRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrInsiderRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the ABUSE_008_REJECTED sentinel to errors.Is.
func (r *InsiderRejection) Unwrap() error { return ErrInsiderRejected }

func insiderReject(field, state, reason string) error {
	return &InsiderRejection{Field: field, State: state, Version: InsiderVersion, Reason: reason}
}

// InsiderScenario is the closed ABUSE-008 corpus vocabulary.
type InsiderScenario string

const (
	ScenarioCompromisedAdmin InsiderScenario = "COMPROMISED_ADMIN"
	ScenarioMassRead         InsiderScenario = "AUTHORIZED_MASS_READ"
	ScenarioBankManipulation InsiderScenario = "BANK_MANIPULATION"
	ScenarioPayrollAccess    InsiderScenario = "PAYROLL_ACCESS_MANIPULATION"
	ScenarioCollusion        InsiderScenario = "COLLUSION"
	ScenarioFalsePositive    InsiderScenario = "FALSE_POSITIVE"
	ScenarioTelemetryGap     InsiderScenario = "TELEMETRY_GAP"
)

// InsiderScenarios is the required signed-corpus coverage.
var InsiderScenarios = []InsiderScenario{
	ScenarioCompromisedAdmin, ScenarioMassRead, ScenarioBankManipulation,
	ScenarioPayrollAccess, ScenarioCollusion, ScenarioFalsePositive, ScenarioTelemetryGap,
}

// Valid reports whether the scenario is declared.
func (s InsiderScenario) Valid() bool {
	switch s {
	case ScenarioCompromisedAdmin, ScenarioMassRead, ScenarioBankManipulation,
		ScenarioPayrollAccess, ScenarioCollusion, ScenarioFalsePositive, ScenarioTelemetryGap:
		return true
	default:
		return false
	}
}

// SeededCase is one signed corpus entry: the scenario, its tenant, the
// signal pattern that must trip the simulation, and whether the case is
// abusive (false positives and telemetry gaps are not).
type SeededCase struct {
	Scenario    InsiderScenario
	Tenant      string
	Pattern     string
	Abusive     bool
	SeededAbuse bool
}

// InsiderSignal is one observed signal under simulation.
type InsiderSignal struct {
	Tenant  string
	Pattern string
	At      time.Time
}

// InsiderOutcome is the closed per-scenario simulation vocabulary.
// Leads are investigative, never accusations: there is no GUILTY.
type InsiderOutcome string

const (
	InsiderDetected     InsiderOutcome = "DETECTED"
	InsiderMissed       InsiderOutcome = "MISSED"
	InsiderInconclusive InsiderOutcome = "INCONCLUSIVE"
	InsiderBenign       InsiderOutcome = "BENIGN"
)

// ScenarioResult is one scenario's lead for human review.
type ScenarioResult struct {
	Scenario InsiderScenario
	Outcome  InsiderOutcome
	Lead     string
}

// InsiderReport is the deterministic simulation outcome.
type InsiderReport struct {
	CorpusDigest string
	Results      []ScenarioResult
	Leads        int
	Digest       string
}

func (r InsiderReport) computedDigest(tenant string) string {
	results := make([]string, 0, len(r.Results))
	for _, res := range r.Results {
		results = append(results, strings.Join([]string{string(res.Scenario), string(res.Outcome), res.Lead}, "\x00"))
	}
	sort.Strings(results)
	w := canonicalbytes.New("hcmnext.engines.abuse.InsiderReport", 1).
		String("tenant", tenant).
		String("corpus", r.CorpusDigest).
		SortedStrings("results", results)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// corpusDigest seals the signed corpus.
func corpusDigest(corpus []SeededCase) string {
	entries := make([]string, 0, len(corpus))
	for _, c := range corpus {
		entries = append(entries, strings.Join([]string{string(c.Scenario), c.Tenant, c.Pattern, fmt.Sprintf("%v", c.Abusive)}, "\x00"))
	}
	sort.Strings(entries)
	w := canonicalbytes.New("hcmnext.engines.abuse.InsiderCorpus", 1).
		SortedStrings("cases", entries)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// RunInsiderSimulation replays the signed corpus against observed signals
// for one tenant. signedDigest is the corpus seal the caller pins: a
// corpus that does not reproduce it is refused.
func RunInsiderSimulation(tenant, signedDigest string, corpus []SeededCase, signals []InsiderSignal, now time.Time) (InsiderReport, error) {
	if strings.TrimSpace(tenant) == "" {
		return InsiderReport{}, insiderReject("insider.tenant", "MISSING", "tenant is required")
	}
	if len(corpus) == 0 {
		return InsiderReport{}, insiderReject("insider.corpus", "MISSING", "signed corpus is required")
	}
	if now.IsZero() {
		return InsiderReport{}, insiderReject("insider.now", "MISSING", "simulation instant is required")
	}
	covered := map[InsiderScenario]bool{}
	for i, c := range corpus {
		if !c.Scenario.Valid() {
			return InsiderReport{}, insiderReject(fmt.Sprintf("insider.corpus[%d].scenario", i), "UNDECLARED", fmt.Sprintf("scenario %q is not declared", c.Scenario))
		}
		if c.Tenant != tenant {
			return InsiderReport{}, insiderReject(fmt.Sprintf("insider.corpus[%d].tenant", i), "CROSS_TENANT", "corpus never crosses tenants")
		}
		if strings.TrimSpace(c.Pattern) == "" {
			return InsiderReport{}, insiderReject(fmt.Sprintf("insider.corpus[%d].pattern", i), "MISSING", "every case carries a signal pattern")
		}
		covered[c.Scenario] = true
	}
	for _, s := range InsiderScenarios {
		if !covered[s] {
			return InsiderReport{}, insiderReject("insider.corpus", "INCOMPLETE", fmt.Sprintf("scenario %s has no signed case", s))
		}
	}
	if sealed := corpusDigest(corpus); signedDigest != "" && signedDigest != sealed {
		return InsiderReport{}, insiderReject("insider.corpus_digest", "MISMATCH", "corpus digest does not seal the corpus")
	}
	for i, sig := range signals {
		if sig.Tenant != tenant {
			return InsiderReport{}, insiderReject(fmt.Sprintf("insider.signals[%d].tenant", i), "CROSS_TENANT", "signals never cross tenants")
		}
		if sig.At.IsZero() || sig.At.After(now) {
			return InsiderReport{}, insiderReject(fmt.Sprintf("insider.signals[%d].at", i), "INVALID", "signal instant must precede the simulation")
		}
	}
	matched := map[string]bool{}
	for _, sig := range signals {
		matched[sig.Pattern] = true
	}
	report := InsiderReport{CorpusDigest: corpusDigest(corpus)}
	for _, scenario := range InsiderScenarios {
		var cases []SeededCase
		for _, c := range corpus {
			if c.Scenario == scenario {
				cases = append(cases, c)
			}
		}
		res := ScenarioResult{Scenario: scenario}
		hit := false
		for _, c := range cases {
			if matched[c.Pattern] {
				hit = true
				break
			}
		}
		switch {
		case scenario == ScenarioFalsePositive:
			res.Outcome = InsiderBenign
			res.Lead = "known-benign pattern: no lead opened"
		case scenario == ScenarioTelemetryGap:
			if hit {
				res.Outcome = InsiderInconclusive
				res.Lead = "telemetry gap observed: coverage lead for human review"
			} else {
				res.Outcome = InsiderBenign
				res.Lead = "no gap observed: no lead opened"
			}
		case hit:
			res.Outcome = InsiderDetected
			res.Lead = fmt.Sprintf("pattern observed for %s: investigative lead for human review, not an accusation", scenario)
			report.Leads++
		default:
			abusive := false
			for _, c := range cases {
				abusive = abusive || (c.Abusive && c.SeededAbuse)
			}
			if abusive {
				return InsiderReport{}, insiderReject("insider."+strings.ToLower(string(scenario)), "MISSED_ABUSE", fmt.Sprintf("seeded %s abuse was missed", scenario))
			}
			res.Outcome = InsiderInconclusive
			res.Lead = fmt.Sprintf("no signal for %s: inconclusive, coverage lead for human review", scenario)
		}
		report.Results = append(report.Results, res)
	}
	report.Digest = report.computedDigest(tenant)
	return report, nil
}
