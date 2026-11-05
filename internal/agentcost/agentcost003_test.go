package agentcost

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// The RED state of AGENTCOST-003: what is known about Jev is second-hand and
// no primary source has been read for price, limits, regions, retention,
// training use or availability. These tests pin the research record and the
// measurement harness that runs our own labelled suites against it.

func TestTodo_AGENTCOST_003(t *testing.T) {
	findings := Agentcost003Findings()
	for _, want := range []string{"interface", "calibrated", "price", "data handling", "failure", "self-host", "languages", "images"} {
		found := false
		for _, finding := range findings {
			if strings.Contains(strings.ToLower(finding.Question+" "+finding.Answer), want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("the record does not answer %q", want)
		}
	}
	for _, finding := range findings {
		if len(finding.Answer) < 100 {
			t.Fatalf("finding %q is not answered", finding.Question)
		}
		if len(finding.Sources) == 0 {
			t.Fatalf("finding %q names no source", finding.Question)
		}
	}

	// The comparison runs on our own labelled data: the Task Catcher and
	// Reminder suites plus the persona suites, synthetic members only.
	cases := Agentcost003ScreeningCases()
	if len(cases) < 80 {
		t.Fatalf("screening cases = %d, want the full ambient suites", len(cases))
	}
	var want, dont int
	for _, c := range cases {
		if c.WantTask {
			want++
		} else {
			dont++
		}
	}
	if want == 0 || dont == 0 {
		t.Fatal("the cases are all one label")
	}
	suites := append(append([]agenteval.AmbientCase{}, agenteval.TaskCatcherAmbientSuite()...), agenteval.ReminderAmbientSuite()...)
	persona := agenteval.AmbientPersonaSuite("task-catcher", "task.offer", agenteval.TaskCatcherAmbientSuite())
	if len(persona.Cases) == 0 {
		t.Fatal("the persona suite is empty")
	}
	texts := map[string]bool{}
	for _, label := range suites {
		texts[label.Text] = true
	}
	for _, c := range cases {
		if !texts[c.Text] {
			t.Fatalf("case %q is not from the labelled suites", c.Text)
		}
	}

	// The methodology executes end to end: accuracy, calibration, abstention,
	// latency and cost per thousand decisions, deterministically.
	providers := Agentcost003StubProviders()
	if len(providers) != 3 {
		t.Fatalf("providers = %d, want general model, Jev-hosted and in-deployment stubs", len(providers))
	}
	for _, provider := range providers {
		first := ScoreJevStubProvider(provider, cases, 0.8)
		second := ScoreJevStubProvider(provider, cases, 0.8)
		if first != second {
			t.Fatal("the comparison is not deterministic")
		}
		if first.Accuracy < 0 || first.Accuracy > 1 || first.ECE < 0 || first.ECE > 1 || first.AbstainRate < 0 || first.AbstainRate > 1 {
			t.Fatalf("%s: score out of range: %+v", provider.Name, first)
		}
		var fraction float64
		for _, bin := range first.Curve {
			fraction += bin.Fraction
		}
		if fraction < 0.999 || fraction > 1.001 {
			t.Fatalf("%s: reliability curve fractions sum to %v", provider.Name, fraction)
		}
		if first.CostPerThousandMicros < 0 || first.MeanLatencyMillis < 0 {
			t.Fatalf("%s: negative latency or cost", provider.Name)
		}
		strict := ScoreJevStubProvider(provider, cases, 0.95)
		if strict.AbstainRate < first.AbstainRate {
			t.Fatalf("%s: a stricter threshold abstains less", provider.Name)
		}
	}

	// One recommendation per decision point of AGENTCOST-002, with the
	// break-even volume.
	recommendations := Agentcost003Recommendations()
	if len(recommendations) != len(agentmodel.Agentcost002DecisionPoints()) {
		t.Fatalf("recommendations = %d, want one per decision point", len(recommendations))
	}
	for _, recommendation := range recommendations {
		if recommendation.Verdict != "adopt" && recommendation.Verdict != "adopt behind escalation" && recommendation.Verdict != "do not adopt" {
			t.Fatalf("%s: verdict %q is not one of the three", recommendation.Point, recommendation.Verdict)
		}
		if recommendation.Reason == "" || recommendation.BreakEven == "" {
			t.Fatalf("%s: reason or break-even missing", recommendation.Point)
		}
	}
}

func TestTodo_AGENTCOST_003_Golden(t *testing.T) {
	const golden = "testdata/agentcost003_jev.golden"
	got := RenderAgentcost003Record()
	if os.Getenv("AGENTCOST_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("Jev record changed; review it and regenerate with AGENTCOST_GOLDEN_UPDATE=1")
	}
	for _, text := range []string{"typesafe", "noul", "choice", "score", "calibrated", "$0.042", "retention", "training", "region", "expected calibration error", "abstain", "break-even", "german", "arabic"} {
		if !strings.Contains(strings.ToLower(got), text) {
			t.Fatalf("the record does not cover %q", text)
		}
	}
}

func TestTodo_AGENTCOST_003_Security(t *testing.T) {
	// Any data class the vendor's terms do not cover is answered "none".
	verdicts := Agentcost003DataClassVerdicts()
	for _, class := range []string{"PUBLIC", "INTERNAL", "PII", "COMPENSATION", "BANK", "MEDICAL", "IMMIGRATION", "CASE", "SPECIAL_CATEGORY"} {
		verdict, ok := verdicts[class]
		if !ok || !strings.HasPrefix(strings.ToLower(verdict), "none") {
			t.Fatalf("class %s may be sent to Jev: %q", class, verdict)
		}
	}
	for _, c := range Agentcost003ScreeningCases() {
		lower := strings.ToLower(c.Text)
		for _, marker := range []string{"@priya", "@dana", "salary", "iban", "passport", "diagnosis"} {
			if strings.Contains(lower, marker) {
				t.Fatalf("the comparison content is not synthetic-or-demo: %q", c.Text)
			}
		}
	}
}
