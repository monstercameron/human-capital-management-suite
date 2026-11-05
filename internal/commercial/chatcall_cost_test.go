package commercial

import (
	"strings"
	"testing"
)

// TestTodo_CHATCALL_006 pins the build-against-buy record: a cost model with
// stated call minutes for 50, 500 and 5,000 seats, the operating work, data
// residency and sub-processor consequences, private-network needs, lock-in
// and exit, the constraints on the CHATCALL-005 answers, a recommendation
// with its flip volume, and the quality measures with their operator
// surface.
func TestTodo_CHATCALL_006(t *testing.T) {
	record := CallCostRecord()
	if record.Assumptions.MinutesPerPersonPerMonth <= 0 {
		t.Fatal("the record states no call minutes per person")
	}
	if record.Assumptions.VideoSharePct < 0 || record.Assumptions.VideoSharePct > 100 {
		t.Fatalf("video share = %d%%", record.Assumptions.VideoSharePct)
	}
	if len(record.Scenarios) != 3 {
		t.Fatalf("scenarios = %d, want 50, 500 and 5,000 seats", len(record.Scenarios))
	}
	wantSeats := []int{50, 500, 5000}
	for i, want := range wantSeats {
		if record.Scenarios[i].Seats != want {
			t.Fatalf("scenario %d seats = %d, want %d", i, record.Scenarios[i].Seats, want)
		}
		if record.Scenarios[i].SelfHostCentsPerMonth <= 0 || record.Scenarios[i].ManagedCentsPerMonth <= 0 {
			t.Fatalf("scenario %d seats leaves a monthly cost open: %+v", want, record.Scenarios[i])
		}
	}
	// The model must agree with the recommendation: managed wins small,
	// self-host wins at scale, and the flip sits between.
	if ManagedMonthlyCents(50) >= SelfHostMonthlyCents(50) {
		t.Fatal("at 50 seats the model does not favour the managed service")
	}
	if SelfHostMonthlyCents(5000) >= ManagedMonthlyCents(5000) {
		t.Fatal("at 5,000 seats the model does not favour self-hosting")
	}
	flip := FlipSeats()
	if flip <= 500 || flip >= 5000 {
		t.Fatalf("flip volume = %d seats, want between 500 and 5,000", flip)
	}
	if record.FlipSeats != flip {
		t.Fatalf("recorded flip = %d seats, model flip = %d", record.FlipSeats, flip)
	}
	if ManagedMonthlyCents(500) != ManagedMonthlyCents(500) || SelfHostMonthlyCents(500) != SelfHostMonthlyCents(500) {
		t.Fatal("the model is not deterministic")
	}
	for _, section := range []struct {
		name, value string
	}{
		{"operating work", record.OpsWork},
		{"residency and sub-processors", record.Residency},
		{"private-network needs", record.PrivateNetwork},
		{"lock-in and exit", record.LockIn},
		{"encryption and recording constraints", record.EncryptionConstraints},
		{"recommendation", record.Recommendation},
		{"operator surface", record.OperatorSurface},
	} {
		if strings.TrimSpace(section.value) == "" {
			t.Fatalf("the record leaves %s unanswered", section.name)
		}
	}
	wantMeasures := []string{"setup success rate", "time to first audio", "drop rate", "audio quality score"}
	if len(record.QualityMeasures) != len(wantMeasures) {
		t.Fatalf("quality measures = %v", record.QualityMeasures)
	}
	for i, want := range wantMeasures {
		if record.QualityMeasures[i] != want {
			t.Fatalf("quality measure %d = %q, want %q", i, record.QualityMeasures[i], want)
		}
	}
	rendered := RenderCallCost()
	for _, phrase := range []string{
		"50", "500", "5,000",
		"sub-processor", "private", "exit",
		"CHATCALL-005",
		"setup success rate", "time to first audio", "drop rate", "audio quality score",
		"operations explorer",
	} {
		if !strings.Contains(rendered, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}
}

func TestTodo_CHATCALL_006_Golden(t *testing.T) {
	const digest = "28dbb96addee5a75aafa53410efa27adeb76ba51c443a8b914949d5b614788e2"
	if got := strings.Count(RenderCallCost(), "\n"); got < 15 {
		t.Fatalf("record has %d lines", got)
	}
	if CallCostDigest() != digest {
		t.Fatalf("the comparison changed; digest is now %s", CallCostDigest())
	}
}
