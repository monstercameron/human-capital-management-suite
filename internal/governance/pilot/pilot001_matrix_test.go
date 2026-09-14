package pilot

import (
	"testing"
)

// TestTodo_PILOT_001_Property: the decision table resolves exactly.
func TestTodo_PILOT_001_Property(t *testing.T) {
	// All pass clears GO.
	verdict, err := Review(pilotInput(pilotAllPass()))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionGo {
		t.Fatalf("all-pass decided %v", verdict.Decision)
	}
	// Critical failure fails outright: no averaging.
	for _, criterion := range []string{CriterionSafety, CriterionCorrectness, CriterionSLO} {
		evidence := pilotAllPass()
		for i := range evidence {
			if evidence[i].Criterion == criterion {
				evidence[i].Status = EvidenceFail
			}
		}
		verdict, err := Review(pilotInput(evidence))
		if err != nil {
			t.Fatal(err)
		}
		if verdict.Decision != DecisionNoGo {
			t.Fatalf("%s failure decided %v", criterion, verdict.Decision)
		}
	}
	// Non-critical failure downgrades with a named blocker.
	evidence := pilotAllPass()
	evidence[5].Status = EvidenceFail
	verdict, err = Review(pilotInput(evidence))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionConditionalGo || len(verdict.Blockers) != 1 {
		t.Fatalf("cost failure = %v %+v", verdict.Decision, verdict.Blockers)
	}
	// An absent criterion reselects.
	verdict, err = Review(pilotInput(pilotAllPass()[:6]))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionReselect {
		t.Fatalf("absent criterion decided %v", verdict.Decision)
	}
	// A live non-critical waiver names but does not block.
	evidence = pilotAllPass()
	evidence[3].Status = EvidenceWaived
	evidence[3].Waiver = &Waiver{By: "sponsor", Reason: "adoption lag accepted", ExpiresUnixMilli: 1893456000000}
	verdict, err = Review(pilotInput(evidence))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionGo || len(verdict.Waivers) != 1 {
		t.Fatalf("live waiver = %v %+v", verdict.Decision, verdict.Waivers)
	}
	// Expansion grants only on GO with an approved manifest and
	// passing value; every other combination denies.
	grant := pilotInput(pilotAllPass())
	grant.AllowExpansion = true
	grant.ExpansionManifest = "manifest:expansion-2"
	grant.ManifestApproved = true
	verdict, err = Review(grant)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Expansion.Permitted || verdict.Expansion.ManifestRef != "manifest:expansion-2" {
		t.Fatalf("expansion = %+v", verdict.Expansion)
	}
	grant.ManifestApproved = false
	verdict, err = Review(grant)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Expansion.Permitted {
		t.Fatal("unapproved manifest authorized expansion")
	}
	conditional := pilotAllPass()
	conditional[5].Status = EvidenceFail
	conditionalInput := pilotInput(conditional)
	conditionalInput.AllowExpansion = true
	conditionalInput.ExpansionManifest = "manifest:expansion-2"
	conditionalInput.ManifestApproved = true
	verdict, err = Review(conditionalInput)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Expansion.Permitted {
		t.Fatal("conditional verdict authorized expansion")
	}
}

// TestTodo_PILOT_001_Golden: the canonical all-pass review pins its
// exact digest.
func TestTodo_PILOT_001_Golden(t *testing.T) {
	verdict, err := Review(pilotInput(pilotAllPass()))
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:f36a12d10c6e290957c9249ef7a99d78b42a672e54d617e7da4995255cd0247a"
	if verdict.Digest != want {
		t.Fatalf("digest = %q, want %q", verdict.Digest, want)
	}
	if verdict.Decision != DecisionGo || len(verdict.Metrics) != 7 {
		t.Fatalf("verdict=%+v", verdict)
	}
	again, err := Review(pilotInput(pilotAllPass()))
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != want {
		t.Fatal("identical reviews replayed to different digests")
	}
}

// TestTodo_PILOT_001_Security: forged excuses and unapproved
// authority never move the verdict.
func TestTodo_PILOT_001_Security(t *testing.T) {
	// A waiver on a failed safety measurement is void.
	evidence := pilotAllPass()
	evidence[0].Status = EvidenceWaived
	evidence[0].Numerator = 10
	evidence[0].Waiver = &Waiver{By: "sponsor", Reason: "forged excuse", ExpiresUnixMilli: 1893456000000}
	verdict, err := Review(pilotInput(evidence))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionNoGo {
		t.Fatalf("void waiver decided %v", verdict.Decision)
	}
	// Expansion without approval never grants, even on GO.
	grant := pilotInput(pilotAllPass())
	grant.AllowExpansion = true
	grant.ExpansionManifest = "manifest:expansion-2"
	verdict, err = Review(grant)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Expansion.Permitted {
		t.Fatal("unapproved expansion granted")
	}
	// Duplicate evidence cannot double-count a metric.
	dup := append(pilotAllPass(), pilotAllPass()[0])
	if _, err := Review(pilotInput(dup)); err == nil {
		t.Fatal("duplicate evidence reviewed")
	}
}

// TestTodo_PILOT_001_Conformance: the package maps every required
// criterion with no silent omission and a closed decision set.
func TestTodo_PILOT_001_Conformance(t *testing.T) {
	verdict, err := Review(pilotInput(pilotAllPass()))
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, metric := range verdict.Metrics {
		seen[metric.Criterion] = true
		if metric.Denominator <= 0 || metric.Digest == "" || metric.Outcome == "" {
			t.Fatalf("metric without denominator/digest/outcome: %+v", metric)
		}
	}
	for _, criterion := range requiredCriteria {
		if !seen[criterion] {
			t.Fatalf("criterion %s omitted", criterion)
		}
	}
	switch verdict.Decision {
	case DecisionGo, DecisionConditionalGo, DecisionNoGo, DecisionReselect:
	default:
		t.Fatalf("decision %q outside closed set", verdict.Decision)
	}
}

// BenchmarkTodo_PILOT_001 measures review evaluation throughput.
func BenchmarkTodo_PILOT_001(b *testing.B) {
	input := pilotInput(pilotAllPass())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		verdict, err := Review(input)
		if err != nil {
			b.Fatal(err)
		}
		if verdict.Decision != DecisionGo {
			b.Fatal("clean review decided no-go in benchmark")
		}
	}
	b.ReportMetric(float64(len(pilotAllPass())), "evidence-per-review")
}

// TestTodo_PILOT_001_Mutation: malformed reviews refuse on the
// documented side.
func TestTodo_PILOT_001_Mutation(t *testing.T) {
	cases := map[string]func(*ReviewInput){
		"no evidence":       func(r *ReviewInput) { r.Evidence = nil },
		"unknown criterion": func(r *ReviewInput) { r.Evidence[0].Criterion = "vibes" },
		"unknown status":    func(r *ReviewInput) { r.Evidence[0].Status = "MAYBE" },
		"no outcome":        func(r *ReviewInput) { r.Evidence[0].Outcome = "" },
		"no denominator":    func(r *ReviewInput) { r.Evidence[0].Denominator = 0 },
		"over denominator":  func(r *ReviewInput) { r.Evidence[0].Numerator = 101 },
		"waiver no fields":  func(r *ReviewInput) { r.Evidence[3].Status = EvidenceWaived },
		"bad risk":          func(r *ReviewInput) { r.RiskTier = "YOLO" },
		"expansion no ref":  func(r *ReviewInput) { r.AllowExpansion = true },
	}
	for name, mutate := range cases {
		input := pilotInput(pilotAllPass())
		mutate(&input)
		if _, err := Review(input); err == nil {
			t.Fatalf("%s reviewed", name)
		}
	}
	// Explicitly expired evidence blocks with a named blocker.
	evidence := pilotAllPass()
	evidence[2].ExpiresUnixMilli = 1000
	verdict, err := Review(pilotInput(evidence))
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Decision != DecisionNoGo || len(verdict.Blockers) != 1 {
		t.Fatalf("expired evidence = %v %+v", verdict.Decision, verdict.Blockers)
	}
}
