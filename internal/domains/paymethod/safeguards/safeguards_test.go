package safeguards

import (
	"strings"
	"sync"
	"testing"
)

// PRIMARY: a covered financial institution resolves a complete written
// information-security program; the consumer count reproducibly drives the
// FTC 500-consumer notice decision.
func TestTodo_SECARCH_017(t *testing.T) {
	rec, err := NewProgram(ProgramArgs{
		Tenant:              "tenant-a",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "assess-2026-01",
		ServiceProviders:    []ProviderDiligence{{Name: "core-processor", ReviewRef: "due-2026-01", Satisfactory: true}},
		AccessControlRef:    "trust015-evidence-001",
		EncryptionRef:       "trust018-evidence-001",
		MonitoringRef:       "trust015-evidence-002",
		IncidentRecords:     []IncidentRecord{{ID: "inc-001", Notified: false}},
		ConsumerCount:       500,
		ConsumerCountMethod: "warehouse-distinct-customers-2026-01",
		ReviewDate:          "2026-01-15",
	})
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	if err := rec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !rec.NoticeRequired() {
		t.Fatal("consumer count 500 must require FTC notice")
	}
	below, err := NewProgram(ProgramArgs{
		Tenant:              "tenant-b",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "assess-2026-01",
		ServiceProviders:    []ProviderDiligence{{Name: "core-processor", ReviewRef: "due-2026-01", Satisfactory: true}},
		AccessControlRef:    "trust015-evidence-001",
		EncryptionRef:       "trust018-evidence-001",
		MonitoringRef:       "trust015-evidence-002",
		ConsumerCount:       499,
		ConsumerCountMethod: "warehouse-distinct-customers-2026-01",
		ReviewDate:          "2026-01-15",
	})
	if err != nil {
		t.Fatalf("NewProgram below threshold: %v", err)
	}
	if below.NoticeRequired() {
		t.Fatal("consumer count 499 must not require FTC notice")
	}
	// RED guard: a covered institution with no written program elements is
	// refused instead of silently accepted.
	if _, err := NewProgram(ProgramArgs{Tenant: "tenant-c", CoveredInstitution: true}); err == nil {
		t.Fatal("empty covered-institution program accepted")
	}
}

func TestTodo_SECARCH_017_Golden(t *testing.T) {
	args := ProgramArgs{
		Tenant:              "tenant-a",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "assess-2026-01",
		ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: true}},
		AccessControlRef:    "a",
		EncryptionRef:       "e",
		MonitoringRef:       "m",
		ConsumerCount:       500,
		ConsumerCountMethod: "method-1",
		ReviewDate:          "2026-01-15",
	}
	a, err := NewProgram(args)
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	b, err := NewProgram(args)
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("digest not reproducible: %q vs %q", a.Digest(), b.Digest())
	}
	if strings.Contains(a.Digest(), "tenant-a") {
		t.Fatalf("digest discloses tenant marker: %q", a.Digest())
	}
}

func TestTodo_SECARCH_017_Security(t *testing.T) {
	// Unsatisfactory provider diligence and missing control evidence are
	// refused; digests carry no raw consumer data or tenant markers.
	if _, err := NewProgram(ProgramArgs{
		Tenant:              "tenant-a",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "assess-1",
		ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: false}},
		AccessControlRef:    "a",
		EncryptionRef:       "e",
		MonitoringRef:       "m",
		ConsumerCount:       10,
		ConsumerCountMethod: "m",
		ReviewDate:          "2026-01-15",
	}); err == nil {
		t.Fatal("unsatisfactory provider diligence accepted")
	}
	rec, err := NewProgram(ProgramArgs{
		Tenant:              "tenant-a",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "assess-1",
		ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: true}},
		AccessControlRef:    "a",
		EncryptionRef:       "e",
		MonitoringRef:       "m",
		ConsumerCount:       10,
		ConsumerCountMethod: "m",
		ReviewDate:          "2026-01-15",
	})
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	if strings.Contains(rec.Digest(), "tenant-a") {
		t.Fatalf("digest discloses tenant: %q", rec.Digest())
	}
}

func TestTodo_SECARCH_017_Integration(t *testing.T) {
	// Non-covered tenants resolve an explicit not-applicable record; covered
	// tenants map every required element including incident history.
	na, err := NewProgram(ProgramArgs{Tenant: "tenant-z", CoveredInstitution: false})
	if err != nil {
		t.Fatalf("not-applicable program refused: %v", err)
	}
	if na.NoticeRequired() {
		t.Fatal("not-applicable program must never require notice")
	}
	rec, err := NewProgram(ProgramArgs{
		Tenant:              "tenant-a",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "assess-1",
		ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: true}},
		AccessControlRef:    "a",
		EncryptionRef:       "e",
		MonitoringRef:       "m",
		IncidentRecords:     []IncidentRecord{{ID: "inc-1", Notified: true}},
		ConsumerCount:       1200,
		ConsumerCountMethod: "m",
		ReviewDate:          "2026-01-15",
	})
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	if !rec.NoticeRequired() || len(rec.IncidentRecords) != 1 {
		t.Fatalf("integration record incomplete: %+v", rec)
	}
}

func TestTodo_SECARCH_017_Conformance(t *testing.T) {
	// The 16 CFR 314 boundary: exactly 500 consumers triggers the notice
	// decision; 499 does not; the decision is a pure function of the
	// reproducible count and method.
	for _, tc := range []struct {
		count int
		want  bool
	}{{499, false}, {500, true}, {5000, true}} {
		rec, err := NewProgram(ProgramArgs{
			Tenant:              "t",
			CoveredInstitution:  true,
			RiskAssessmentRef:   "a",
			ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: true}},
			AccessControlRef:    "a",
			EncryptionRef:       "e",
			MonitoringRef:       "m",
			ConsumerCount:       tc.count,
			ConsumerCountMethod: "m",
			ReviewDate:          "2026-01-15",
		})
		if err != nil {
			t.Fatalf("count %d: %v", tc.count, err)
		}
		if rec.NoticeRequired() != tc.want {
			t.Fatalf("count %d notice = %v want %v", tc.count, rec.NoticeRequired(), tc.want)
		}
	}
}

func TestTodo_SECARCH_017_Mutation(t *testing.T) {
	// Guards that a weakened implementation must fail: dropping the
	// threshold to >500, accepting missing risk assessment, or accepting
	// unsatisfactory provider diligence.
	rec, err := NewProgram(ProgramArgs{
		Tenant:              "t",
		CoveredInstitution:  true,
		RiskAssessmentRef:   "a",
		ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: true}},
		AccessControlRef:    "a",
		EncryptionRef:       "e",
		MonitoringRef:       "m",
		ConsumerCount:       500,
		ConsumerCountMethod: "m",
		ReviewDate:          "2026-01-15",
	})
	if err != nil {
		t.Fatalf("NewProgram: %v", err)
	}
	if !rec.NoticeRequired() {
		t.Fatal("mutation survived: 500-consumer notice dropped")
	}
	if _, err := NewProgram(ProgramArgs{
		Tenant:              "t",
		CoveredInstitution:  true,
		ServiceProviders:    []ProviderDiligence{{Name: "p", ReviewRef: "d", Satisfactory: true}},
		AccessControlRef:    "a",
		EncryptionRef:       "e",
		MonitoringRef:       "m",
		ConsumerCount:       1,
		ConsumerCountMethod: "m",
		ReviewDate:          "2026-01-15",
	}); err == nil {
		t.Fatal("mutation survived: missing risk assessment accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec.Digest() == "" {
				t.Errorf("concurrent digest empty")
			}
		}()
	}
	wg.Wait()
}
