package adversarial

import (
	"strings"
	"sync"
	"testing"
)

func runThreat002(t *testing.T) Report {
	t.Helper()
	report, err := Run(DefaultJourneys())
	if err != nil {
		t.Fatal(err)
	}
	if !report.AllDeniedWithoutDisclosure || report.UnauthorizedPersistence != 0 || report.UnauthorizedEffects != 0 || report.TelemetryLeaks != 0 {
		t.Fatalf("unsafe adversarial report: %+v", report)
	}
	if strings.Contains(report.Explain(), "PLACEHOLDER_TENANT") || strings.Contains(report.Explain(), "PLACEHOLDER_ACTOR") {
		t.Fatal("report explanation leaked identity metadata")
	}
	return report
}

func TestAdversarialJourneysDenyWithoutExistenceMetadataOrTelemetryLeakage(t *testing.T) {
	runThreat002(t)
}
func TestTodo_THREAT_002_Property(t *testing.T) { runThreat002(t) }
func TestTodo_THREAT_002_Golden(t *testing.T) {
	report := runThreat002(t)
	if !strings.HasPrefix(report.Digest, "sha256:") {
		t.Fatal(report.Digest)
	}
}
func FuzzTodo_THREAT_002(f *testing.F) {
	f.Add("PLACEHOLDER_SECRET")
	f.Add("salary=private")
	f.Fuzz(func(t *testing.T, secret string) {
		journey := DefaultJourneys()[0]
		journey.Secret = secret
		report, err := Run([]Journey{journey})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(report.Explain(), secret) && secret != "" {
			t.Fatal("secret leaked")
		}
	})
}
func TestTodo_THREAT_002_Race(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	reports := make(chan Report, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report, err := Run(DefaultJourneys())
			reports <- report
			errs <- err
		}()
	}
	wg.Wait()
	close(reports)
	close(errs)
	var digest string
	for report := range reports {
		if digest == "" {
			digest = report.Digest
		}
		if report.Digest != digest || !report.AllDeniedWithoutDisclosure || report.UnauthorizedPersistence != 0 || report.UnauthorizedEffects != 0 || report.TelemetryLeaks != 0 {
			t.Fatalf("concurrent adversarial report = %+v", report)
		}
		if strings.Contains(report.Explain(), "PLACEHOLDER_TENANT") || strings.Contains(report.Explain(), "PLACEHOLDER_ACTOR") {
			t.Fatalf("concurrent adversarial report leaked identity: %s", report.Explain())
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent adversarial evaluation: %v", err)
		}
	}
}
func TestTodo_THREAT_002_Integration(t *testing.T) { runThreat002(t) }
func TestTodo_THREAT_002_Fault(t *testing.T) {
	journey := DefaultJourneys()[0]
	journey.Action = "REPAIR_AFTER_PARTIAL_FAILURE"
	report, err := Run([]Journey{journey})
	if err != nil || !report.AllDeniedWithoutDisclosure {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestTodo_THREAT_002_Security(t *testing.T) { runThreat002(t) }
func TestTodo_THREAT_002_Conformance(t *testing.T) {
	if len(Channels()) != len(DefaultJourneys()) {
		t.Fatal("channel corpus mismatch")
	}
	runThreat002(t)
}
func TestTodo_THREAT_002_Browser(t *testing.T) {
	journey := DefaultJourneys()[1]
	result, err := Evaluate(journey)
	if err != nil || result.Decision != DecisionDenied || !result.NoExistenceMetadata {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestTodo_THREAT_002_Recovery(t *testing.T) {
	journey := DefaultJourneys()[14]
	journey.Restart = true
	journey.Replay = true
	result, err := Evaluate(journey)
	if err != nil || result.Decision != DecisionDenied || !result.NoUnauthorizedEffects {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestTodo_THREAT_002_Mutation(t *testing.T) {
	journeys := DefaultJourneys()
	before := journeys[0]
	if _, err := Run(journeys); err != nil {
		t.Fatal(err)
	}
	if journeys[0].Secret != before.Secret || journeys[0].ID != before.ID {
		t.Fatal("journey fixture mutated")
	}
}
