package mobility

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mobilityInterval(t *testing.T, start, end int) values.EffectiveInterval {
	t.Helper()
	a, err := values.NewLocalDate(2026, time.January, start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := values.NewLocalDate(2026, time.January, end)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(a, b, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func mobilitySplit(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assignment(t *testing.T, id string, role AssignmentRole, start, end int, split string) AssignmentRevision {
	t.Helper()
	a, err := NewAssignmentRevision(AssignmentRevision{AssignmentID: id, Revision: 1, Role: role, Type: LongTerm, EntityRef: "worker:001", EmploymentRef: "employment:001", LocationRef: "location:" + id, Jurisdiction: "US-CA", PayrollRef: "payroll:" + id, PayrollModel: PayrollSplit, Effective: mobilityInterval(t, start, end), CostAllocationSplit: mobilitySplit(t, split), SourceAuthority: "hr.authority/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func validRelocation(t *testing.T) RelocationPackage {
	t.Helper()
	p, err := NewRelocationPackage(RelocationPackage{PackageID: "relocation:001", MobilityID: "mobility:001", WorkerRef: "worker:001", OwnerRef: "mobility-owner", Milestones: []RelocationMilestone{{MilestoneID: "move", Kind: RelocationTravel, Effective: mobilityInterval(t, 3, 4), OwnerRef: "mobility-owner", EvidenceRef: "evidence:move"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func validImmigration(t *testing.T) []ImmigrationMilestone {
	t.Helper()
	kinds := []ImmigrationMilestoneKind{ImmigrationPetition, ImmigrationApproval, ImmigrationExpiry, ImmigrationReverification}
	result := make([]ImmigrationMilestone, 0, len(kinds))
	for i, kind := range kinds {
		m, err := NewImmigrationMilestone(ImmigrationMilestone{MilestoneID: string(kind), ProcessRef: "process:001", Kind: kind, Jurisdiction: "DE-BE", At: func() values.LocalDate { d, _ := values.NewLocalDate(2026, time.March, i+1); return d }(), SourceRef: "immigration.source/v1", Disclosure: DisclosureScope{Jurisdiction: "DE-BE", Allowed: true}})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, m)
	}
	return result
}

func validPlan(t *testing.T) MobilityPlan {
	t.Helper()
	home := assignment(t, "home:001", AssignmentHome, 1, 20, "0.50")
	host := assignment(t, "host:001", AssignmentHost, 5, 15, "0.50")
	p, err := NewMobilityPlan(MobilityPlan{MobilityID: "mobility:001", Revision: 1, WorkerRef: "worker:001", HomeAssignment: home, HostAssignment: host, Legs: []MobilityLeg{{LegID: "leg:001", OriginJurisdiction: "US-CA", DestinationJurisdiction: "DE-BE", Effective: mobilityInterval(t, 5, 15), Purpose: "assignment", WorkPresence: true, SourceRef: "travel:001"}}, Relocation: validRelocation(t), Immigration: validImmigration(t), Obligations: []MobilityObligation{{Kind: ObligationPayroll, Ref: "payroll-review", Status: ObligationReady, OwnerRef: "payroll", EvidenceRef: "evidence:payroll"}, {Kind: ObligationTax, Ref: "tax-review", Status: ObligationReady, OwnerRef: "tax", EvidenceRef: "evidence:tax"}, {Kind: ObligationPE, Ref: "pe-review", Status: ObligationReady, OwnerRef: "legal", EvidenceRef: "evidence:pe"}, {Kind: ObligationPrivacy, Ref: "privacy-review", Status: ObligationReady, OwnerRef: "privacy", EvidenceRef: "evidence:privacy"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMobilityAssignmentRequiresHomeHostAuthorityDatesAndAuthorizationMilestones(t *testing.T) {
	p := validPlan(t)
	if p.Status != StatusReady || p.CanonicalDigest == "" {
		t.Fatalf("plan = %+v", p)
	}
	if p.HomeAssignment.Role != AssignmentHome || p.HostAssignment.Role != AssignmentHost {
		t.Fatal("home/host roles were not preserved")
	}
	if len(p.Immigration) != 4 {
		t.Fatalf("immigration milestones = %d", len(p.Immigration))
	}
	if _, err := p.Assess(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_MOBILITY_001_Property(t *testing.T) {
	property := func(startRaw, spanRaw uint8) bool {
		start := 1 + int(startRaw)%20
		end := start + 1 + int(spanRaw)%(28-start)
		p := validPlan(t)
		p.Legs[0].Effective = mobilityInterval(t, start, end)
		p, err := NewMobilityPlan(p)
		if err != nil || p.Revision != 1 || p.CanonicalDigest == "" || len(p.Canonical()) == 0 {
			return false
		}
		got, err := p.Digest()
		return err == nil && got == p.CanonicalDigest
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 64}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_MOBILITY_001_Golden(t *testing.T) {
	p := validPlan(t)
	canonical := p.Canonical()
	const wantDigest = "sha256:070816042b337e9935fa98e75f90e4cb4a67edc61572948f1db1d45b1f8cecf1"
	if p.CanonicalDigest != wantDigest {
		t.Fatalf("digest=%q want=%q", p.CanonicalDigest, wantDigest)
	}
	if len(canonical) != 3712 {
		t.Fatalf("canonical length=%d want=3712", len(canonical))
	}
	if string(canonical[:24]) != "\x07$schema%hcmnext.domains" {
		t.Fatalf("canonical prefix=%q", canonical[:24])
	}
	if string(canonical[len(canonical)-13:]) != "\x06status\x05READY" {
		t.Fatalf("canonical suffix=%q", canonical[len(canonical)-13:])
	}
	if got, err := p.Digest(); err != nil || got != wantDigest {
		t.Fatalf("digest = %q, %v", got, err)
	}
	x, err := p.Explain()
	if err != nil || x.Status != StatusReady || x.Digest != p.CanonicalDigest {
		t.Fatalf("explanation = %+v, %v", x, err)
	}
}

func TestTodo_MOBILITY_001_Race(t *testing.T) {
	p := validPlan(t)
	var wg sync.WaitGroup
	results := make(chan struct {
		digest string
		status MobilityStatus
		err    error
	}, 24)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, err := p.Explain()
			results <- struct {
				digest string
				status MobilityStatus
				err    error
			}{x.Digest, x.Status, err}
			err = p.Validate()
			results <- struct {
				digest string
				status MobilityStatus
				err    error
			}{p.CanonicalDigest, p.Status, err}
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got.err != nil || got.digest != p.CanonicalDigest || got.status != StatusReady {
			t.Fatalf("shared race result=%+v", got)
		}
	}
}

func TestTodo_MOBILITY_001_Fault(t *testing.T) {
	p := validPlan(t)
	p.HomeAssignment.SourceAuthority = ""
	if err := p.Validate(); err == nil {
		t.Fatal("missing source authority accepted")
	}
	bad := validPlan(t)
	bad.Immigration = bad.Immigration[:3]
	if err := bad.Validate(); err == nil {
		t.Fatal("missing authorization milestone accepted")
	}
}

func TestTodo_MOBILITY_001_Security(t *testing.T) {
	p := validPlan(t)
	text := p.Explain
	x, err := text()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"worker:001", "immigration.source/v1", "evidence:privacy"} {
		if strings.Contains(fmtPlanExplanation(x), secret) {
			t.Fatalf("explanation leaked %q", secret)
		}
	}
}

func fmtPlanExplanation(x PlanExplanation) string { return fmt.Sprintf("%+v", x) }

func TestTodo_MOBILITY_001_Conformance(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("serving contract: %v", err)
	}
	for _, kind := range []ImmigrationMilestoneKind{ImmigrationPetition, ImmigrationApproval, ImmigrationExpiry, ImmigrationReverification} {
		if !kind.Valid() {
			t.Fatal(kind)
		}
	}
	if ImmigrationMilestoneKind("VENDOR_ACCEPTED").Valid() {
		t.Fatal("provider state became an immigration milestone")
	}
	nonExact, err := values.NewDecimal("0.33", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAssignmentRevision(AssignmentRevision{AssignmentID: "bad", Revision: 1, Role: AssignmentHost, EntityRef: "worker", EmploymentRef: "employment", LocationRef: "location", Jurisdiction: "US", PayrollRef: "payroll", CostAllocationSplit: nonExact, SourceAuthority: "source"}); err == nil {
		t.Fatal("non-exact scale accepted")
	}
}

func TestTodo_MOBILITY_001_Mutation(t *testing.T) {
	p := validPlan(t)
	overlap := assignment(t, "host:002", AssignmentHost, 10, 18, "0.50")
	p.HostAssignments = []AssignmentRevision{p.HostAssignment, overlap}
	if !errors.Is(p.Validate(), ErrOverlappingHostAssignment) {
		t.Fatalf("overlap error = %v", p.Validate())
	}
	p = validPlan(t)
	p.CanonicalDigest = "sha256:forged"
	if err := p.Validate(); err == nil {
		t.Fatal("forged digest accepted")
	}
}
