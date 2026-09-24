package service

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func serviceDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func serviceInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	calendar := values.CalendarRef{Ref: "service.test", Version: "1"}
	var (
		interval values.EffectiveInterval
		err      error
	)
	if end == "" {
		interval, err = values.NewOpenLocalDateInterval(serviceDate(t, start), calendar)
	} else {
		interval, err = values.NewLocalDateInterval(serviceDate(t, start), serviceDate(t, end), calendar)
	}
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func serviceKnown(t *testing.T) values.KnownAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	instant := values.NewInstant(at)
	known, err := values.NewKnownAt(instant)
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func serviceRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func employmentCredit(t *testing.T, kind CreditSourceKind, evidence, authority string) CreditSource {
	t.Helper()
	return CreditSource{Kind: kind, EvidenceRef: evidence, AuthorityRef: authority, Revision: serviceRevision(t, "service.credit", 1)}
}

func servicePeriod(t *testing.T, id, start, end string, source CreditSource) ServicePeriod {
	t.Helper()
	return ServicePeriod{
		ID: id, EmploymentID: "employment-1", Interval: serviceInterval(t, start, end),
		Credit: source, Break: BreakNone, Dimensions: []SeniorityDimension{DimensionGeneral},
		KnownAt: serviceKnown(t), Revision: serviceRevision(t, "service.period."+id, 1),
	}
}

func serviceRule(t *testing.T) SeniorityRule {
	t.Helper()
	return SeniorityRule{
		Dimension: DimensionGeneral, Unit: UnitMonths, DaysPerUnit: 30, Rounding: RoundDown,
		Bridge:   BridgeRule{MaxGapDays: 5, BreakTypes: []BreakType{BreakUncredited}, EvidenceRef: "policy/bridge-1"},
		Revision: serviceRevision(t, "service.rule.general", 1), EvidenceRef: "policy/seniority-1",
	}
}

func serviceMeasure(t *testing.T, snapshot SenioritySnapshot, dimension SeniorityDimension) SeniorityMeasure {
	t.Helper()
	for _, measure := range snapshot.Measures {
		if measure.Dimension == dimension {
			return measure
		}
	}
	t.Fatalf("missing measure for %s", dimension)
	return SeniorityMeasure{}
}

func scopedServiceModel(t *testing.T, model ServiceModel, employmentID string) ServiceModel {
	t.Helper()
	periods := clonePeriods(model.Periods)
	for i := range periods {
		periods[i].EmploymentID = ""
		periods[i].EmploymentRef = values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: employmentID}
	}
	scoped, err := NewServiceModel(periods, model.Rules)
	if err != nil {
		t.Fatal(err)
	}
	return scoped
}

func validServiceModel(t *testing.T) ServiceModel {
	t.Helper()
	periods := []ServicePeriod{
		servicePeriod(t, "period-a", "2026-01-01", "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/employment-a", "")),
		servicePeriod(t, "period-b", "2026-03-05", "2026-05-01", employmentCredit(t, CreditEmployment, "evidence/employment-b", "")),
	}
	model, err := NewServiceModel(periods, []SeniorityRule{serviceRule(t)})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func TestServiceModelRejectsOverlappingUnattributedPeriodsAndImplicitSeniority(t *testing.T) {
	first := servicePeriod(t, "period-a", "2026-01-01", "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/a", ""))
	overlap := servicePeriod(t, "period-overlap", "2026-02-01", "2026-04-01", employmentCredit(t, CreditEmployment, "evidence/overlap", ""))
	if _, err := NewServiceModel([]ServicePeriod{first, overlap}, []SeniorityRule{serviceRule(t)}); !errors.Is(err, ErrOverlappingPeriods) {
		t.Fatalf("overlap error = %v, want ErrOverlappingPeriods", err)
	}

	unattributed := employmentCredit(t, CreditAcquiredService, "evidence/acquired", "")
	if _, err := NewServiceModel([]ServicePeriod{servicePeriod(t, "period-acquired", "2026-01-01", "2026-02-01", unattributed)}, []SeniorityRule{serviceRule(t)}); !errors.Is(err, ErrInvalidCreditSource) || !strings.Contains(err.Error(), "authority_ref") {
		t.Fatalf("unattributed credit error = %v, want authority_ref validation", err)
	}

	if _, err := NewServiceModel([]ServicePeriod{first}, nil); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("implicit seniority error = %v, want ErrInvalidService", err)
	}

	model := validServiceModel(t)
	asOf, err := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Measures) != 1 || snapshot.Measures[0].BridgedDays != 4 || snapshot.Measures[0].RoundedUnits != 4 {
		t.Fatalf("seniority measure = %+v, want four bridged days and four rounded months", snapshot.Measures)
	}
	if snapshot.Canonical() == nil || snapshot.CanonicalDigest == "" {
		t.Fatal("valid seniority snapshot did not produce canonical bytes and digest")
	}
	if explanation, err := model.Explain(); err != nil || explanation.CanonicalDigest == "" || explanation.PeriodCount != 2 {
		t.Fatalf("service explanation = %+v, err=%v", explanation, err)
	}
}

func TestTodo_SERVICE_001_Property(t *testing.T) {
	model := validServiceModel(t)
	asOf, err := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if got := serviceMeasure(t, snapshot, DimensionGeneral).TotalCreditedDays; got != 120 {
		t.Fatalf("total credited days = %d, want 120 including the bridged gap", got)
	}
}
func TestTodo_SERVICE_001_Golden(t *testing.T) {
	model := validServiceModel(t)
	if model.CanonicalDigest == "" || canonicalbytes.Digest(model.body()) != model.CanonicalDigest {
		t.Fatalf("service model digest=%q does not match canonical body", model.CanonicalDigest)
	}
}
func TestTodo_SERVICE_001_Race(t *testing.T) {
	model := validServiceModel(t)
	asOf, err := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan SenioritySnapshot, 12)
	errors := make(chan error, 12)
	for i := 0; i < cap(results); i++ {
		go func() {
			got, err := model.Compute(asOf)
			if err != nil {
				errors <- err
				return
			}
			results <- got
		}()
	}
	for i := 0; i < cap(results); i++ {
		select {
		case err := <-errors:
			t.Fatal(err)
		case got := <-results:
			if got.CanonicalDigest != want.CanonicalDigest || len(got.Measures) != 1 || got.Measures[0].ContinuousDays != want.Measures[0].ContinuousDays {
				t.Fatalf("concurrent computation diverged: got=%+v want=%+v", got, want)
			}
		}
	}
}
func TestTodo_SERVICE_001_Fault(t *testing.T) {
	first := servicePeriod(t, "first", "2026-01-01", "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/first", ""))
	second := servicePeriod(t, "second", "2026-02-01", "2026-04-01", employmentCredit(t, CreditEmployment, "evidence/second", ""))
	if _, err := NewServiceModel([]ServicePeriod{first, second}, []SeniorityRule{serviceRule(t)}); !errors.Is(err, ErrOverlappingPeriods) {
		t.Fatalf("overlapping periods error=%v, want ErrOverlappingPeriods", err)
	}
}
func TestTodo_SERVICE_001_Security(t *testing.T) {
	unattributed := employmentCredit(t, CreditAcquiredService, "evidence/acquired", "")
	if _, err := NewServiceModel([]ServicePeriod{servicePeriod(t, "acquired", "2026-01-01", "2026-02-01", unattributed)}, []SeniorityRule{serviceRule(t)}); !errors.Is(err, ErrInvalidCreditSource) || !strings.Contains(err.Error(), "authority_ref") {
		t.Fatalf("unattributed acquired service error=%v, want authority_ref refusal", err)
	}
}
func TestTodo_SERVICE_001_Conformance(t *testing.T) {
	model := validServiceModel(t)
	asOf, err := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Measures) != 1 || snapshot.Measures[0].Dimension != DimensionGeneral || snapshot.CanonicalDigest == "" {
		t.Fatalf("unexpected governed measure: %+v", snapshot.Measures)
	}
}
func TestTodo_SERVICE_001_Mutation(t *testing.T) {
	period := servicePeriod(t, "copy", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/copy", ""))
	period.Dimensions = []SeniorityDimension{DimensionGeneral}
	model, err := NewServiceModel([]ServicePeriod{period}, []SeniorityRule{serviceRule(t)})
	if err != nil {
		t.Fatal(err)
	}
	digest := model.CanonicalDigest
	period.Dimensions[0] = DimensionBenefits
	if model.CanonicalDigest != digest || model.Periods[0].Dimensions[0] != DimensionGeneral {
		t.Fatal("service model retained caller-owned dimensions")
	}
}

func TestServiceCalculationExplainsBreakBridgeCreditAndDimensionSpecificOrder(t *testing.T) {
	first := servicePeriod(t, "first", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/first", ""))
	first.Dimensions = []SeniorityDimension{DimensionGeneral, DimensionBenefits}
	second := servicePeriod(t, "second", "2026-01-20", "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/second", ""))
	third := servicePeriod(t, "third", "2026-03-05", "2026-04-01", employmentCredit(t, CreditEmployment, "evidence/third", ""))
	breakPeriod := servicePeriod(t, "termination", "2026-03-01", "2026-03-05", employmentCredit(t, CreditEmployment, "evidence/break", ""))
	breakPeriod.Break = BreakTermination
	rules := []SeniorityRule{
		{Dimension: DimensionGeneral, Unit: UnitDays, DaysPerUnit: 1, Rounding: RoundExact, AllowOverlap: true, Bridge: BridgeRule{MaxGapDays: 5, BreakTypes: []BreakType{BreakTermination}, EvidenceRef: "policy/termination-bridge"}, Revision: serviceRevision(t, "rule/general", 1), EvidenceRef: "policy/general"},
		{Dimension: DimensionBenefits, Unit: UnitDays, DaysPerUnit: 1, Rounding: RoundExact, Bridge: BridgeRule{}, Revision: serviceRevision(t, "rule/benefits", 1), EvidenceRef: "policy/benefits"},
	}
	model, err := NewServiceModel([]ServicePeriod{first, second, breakPeriod, third}, rules)
	if err != nil {
		t.Fatal(err)
	}
	asOf, err := NewAsOf(serviceDate(t, "2026-04-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Measures) != 2 {
		t.Fatalf("measures=%d, want 2", len(snapshot.Measures))
	}
	general := serviceMeasure(t, snapshot, DimensionGeneral)
	benefits := serviceMeasure(t, snapshot, DimensionBenefits)
	if general.Dimension != DimensionGeneral || general.RawDays != 86 || general.BridgedDays != 4 || general.ContinuousDays != 90 || general.AdjustedDate.String() != "2026-01-01" || general.Status != StatusKnown || len(general.Trace) == 0 {
		t.Fatalf("general=%+v, want union duration, bridged gap, adjusted date and trace", general)
	}
	if benefits.Dimension != DimensionBenefits || benefits.RawDays != 31 || benefits.TotalCreditedDays != 31 || benefits.ContinuousDays != 0 || benefits.BridgedDays != 0 || benefits.AdjustedDate.String() != "2026-04-01" || benefits.Status != StatusPartial {
		t.Fatalf("benefits=%+v, want dimension-specific calculation", benefits)
	}
	if general.InputsDigest != benefits.InputsDigest || snapshot.CanonicalDigest == "" {
		t.Fatal("results are not tied to one pinned input digest")
	}
}

func TestTodo_SERVICE_002_Property(t *testing.T) {
	first := servicePeriod(t, "z", "2024-02-01", "2024-03-01", employmentCredit(t, CreditEmployment, "evidence/z", ""))
	second := servicePeriod(t, "a", "2024-02-15", "2024-03-15", employmentCredit(t, CreditEmployment, "evidence/a", ""))
	rule := serviceRule(t)
	rule.AllowOverlap, rule.Unit, rule.DaysPerUnit, rule.Rounding = true, UnitDays, 1, RoundExact
	asOf, _ := NewAsOf(serviceDate(t, "2024-03-15"), serviceKnown(t))
	a, err := ComputeSeniority([]ServicePeriod{first, second}, asOf, []SeniorityRule{rule})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComputeSeniority([]ServicePeriod{second, first}, asOf, []SeniorityRule{rule})
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalDigest != b.CanonicalDigest || a.Measures[0].RawDays != 43 || a.Measures[0].AdjustedDate.String() != "2024-02-01" {
		t.Fatalf("permuted concurrent credits differ: a=%+v b=%+v", a, b)
	}
}
func TestTodo_SERVICE_002_Golden(t *testing.T) {
	model := validServiceModel(t)
	if Version() != 1 || evaluationSchemaVersion != 2 {
		t.Fatalf("service model version=%d evaluation version=%d", Version(), evaluationSchemaVersion)
	}
	if model.CanonicalDigest != "sha256:738cb9de457e60722f7d95e124ab1546cfa2f502987f734f04c3849797084881" {
		t.Fatalf("legacy model digest changed: %s", model.CanonicalDigest)
	}
	asOf, _ := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalDigest != "sha256:41f2c2a7b7181f505ccda5765f1c40965601ba07fce404b4419f7fcced45bfd5" {
		t.Fatalf("canonical digest changed: %s", snapshot.CanonicalDigest)
	}
}

func TestTodo_SERVICE_002_RankPopulationAndUnknownHistory(t *testing.T) {
	model := validServiceModel(t)
	asOf, _ := NewAsOf(serviceDate(t, "2026-06-15"), serviceKnown(t))
	without, err := model.Compute(asOf)
	if err != nil || without.Measures[0].RankStatus != StatusUnknown || without.Measures[0].SeniorityRank != 0 {
		t.Fatalf("missing peer history fabricated rank: %+v, err=%v", without.Measures, err)
	}
	model = scopedServiceModel(t, model, "00000000-0000-0000-0000-000000000001")
	peerPeriod := servicePeriod(t, "peer", "2025-01-01", "2026-05-01", employmentCredit(t, CreditEmployment, "evidence/peer", ""))
	peerPeriod.EmploymentID = ""
	peerPeriod.EmploymentRef = values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: "00000000-0000-0000-0000-000000000002"}
	rankedModel, err := model.WithPeerPopulation([]SeniorityPeer{{SubjectID: "peer-1", Periods: []ServicePeriod{peerPeriod}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	ranked, err := rankedModel.Compute(asOf)
	if err != nil || ranked.Measures[0].RankStatus != StatusKnown || ranked.Measures[0].SeniorityRank != 2 {
		t.Fatalf("rank=%+v, err=%v; expected peer with longer service to rank first", ranked.Measures, err)
	}
	tiedPeriods := clonePeriods(model.Periods)
	for i := range tiedPeriods {
		tiedPeriods[i].EmploymentRef.Id = "00000000-0000-0000-0000-000000000003"
	}
	tiedModel, err := model.WithPeerPopulation([]SeniorityPeer{
		{SubjectID: "peer-longer", Periods: []ServicePeriod{peerPeriod}},
		{SubjectID: "peer-tied", Periods: tiedPeriods},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	tied, err := tiedModel.Compute(asOf)
	if err != nil || tied.Measures[0].SeniorityRank != 2 {
		t.Fatalf("competition rank must give equal credit equal rank: %+v, err=%v", tied.Measures, err)
	}
	if _, err := model.WithPeerPopulation([]SeniorityPeer{{SubjectID: "peer-1", Periods: []ServicePeriod{peerPeriod}}}, false); err != nil {
		t.Fatal(err)
	}
	emptyComplete, err := model.WithPeerPopulation(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	emptyRank, err := emptyComplete.Compute(asOf)
	if err != nil || emptyRank.Measures[0].RankStatus != StatusKnown || emptyRank.Measures[0].SeniorityRank != 1 {
		t.Fatalf("complete empty cohort must rank the primary exactly once: %+v, err=%v", emptyRank.Measures, err)
	}
	incomplete, err := model.WithPeerPopulation([]SeniorityPeer{{SubjectID: "peer-1", Periods: []ServicePeriod{peerPeriod}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	incompleteResult, err := incomplete.Compute(asOf)
	if err != nil || incompleteResult.Measures[0].Status != StatusPartial || incompleteResult.Measures[0].RankStatus != StatusUnknown {
		t.Fatalf("incomplete cohort must stay partial and unranked: %+v, err=%v", incompleteResult.Measures, err)
	}
	if _, err := model.WithPeerPopulation([]SeniorityPeer{{SubjectID: "00000000-0000-0000-0000-000000000001", Periods: []ServicePeriod{peerPeriod}}}, true); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("primary duplicated as peer: %v", err)
	}
}
func TestTodo_SERVICE_002_Race(t *testing.T) {
	model := validServiceModel(t)
	asOf, err := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	results := make(chan SenioritySnapshot, workers)
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := model.Compute(asOf)
			if err != nil {
				errors <- err
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	count := 0
	for got := range results {
		count++
		if got.CanonicalDigest != want.CanonicalDigest || len(got.Measures) != len(want.Measures) || got.Measures[0].ContinuousDays != want.Measures[0].ContinuousDays {
			t.Fatalf("concurrent calculation diverged: got=%+v want=%+v", got.Measures, want.Measures)
		}
	}
	if count != workers {
		t.Fatalf("successful calculations=%d, want %d", count, workers)
	}
}
func TestTodo_SERVICE_002_Fault(t *testing.T) {
	period := servicePeriod(t, "fraction", "2026-01-01", "2026-01-02", employmentCredit(t, CreditEmployment, "evidence/fraction", ""))
	rule := serviceRule(t)
	rule.DaysPerUnit, rule.Rounding = 30, RoundExact
	asOf, _ := NewAsOf(serviceDate(t, "2026-01-02"), serviceKnown(t))
	if _, err := ComputeSeniority([]ServicePeriod{period}, asOf, []SeniorityRule{rule}); !errors.Is(err, ErrRoundingConflict) {
		t.Fatalf("error=%v, want ErrRoundingConflict", err)
	}
	if _, err := validServiceModel(t).WithPeerPopulation(make([]SeniorityPeer, maxSeniorityPopulation+1), true); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("oversized population error=%v", err)
	}
}
func TestTodo_SERVICE_002_Security(t *testing.T) {
	model := validServiceModel(t)
	asOf, _ := NewAsOf(serviceDate(t, "2026-05-01"), serviceKnown(t))
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Measures[0].InputsDigest = "sha256:forged"
	if !errors.Is(snapshot.Validate(), ErrInvalidService) {
		t.Fatal("forged per-dimension input digest was accepted")
	}
	primary := servicePeriod(t, "primary", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/primary", ""))
	primary.EmploymentID = ""
	primary.EmploymentRef = values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: "00000000-0000-0000-0000-000000000001"}
	scoped, err := NewServiceModel([]ServicePeriod{primary}, []SeniorityRule{serviceRule(t)})
	if err != nil {
		t.Fatal(err)
	}
	peer := servicePeriod(t, "peer", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/peer", ""))
	peer.EmploymentID = ""
	peer.EmploymentRef = values.EntityRef{Tenant: "tenant-b", Kind: "employment", Id: "00000000-0000-0000-0000-000000000002"}
	if _, err := scoped.WithPeerPopulation([]SeniorityPeer{{SubjectID: "peer-2", Periods: []ServicePeriod{peer}}}, true); !errors.Is(err, ErrInvalidService) || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("cross-tenant peer error=%v", err)
	}
	peer.EmploymentRef.Tenant = "tenant-a"
	pinned, err := scoped.WithPeerPopulation([]SeniorityPeer{{SubjectID: "peer-2", Periods: []ServicePeriod{peer}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	pinnedDigest := pinned.CanonicalDigest
	peer.Dimensions[0] = DimensionPay
	if pinned.CanonicalDigest != pinnedDigest || canonicalbytes.Digest(pinned.body()) != pinnedDigest {
		t.Fatal("pinned population aliases caller-owned period input")
	}
	incomplete, err := scoped.WithPeerPopulation(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := scoped.WithPeerPopulation(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	incompleteResult, err := incomplete.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	completeResult, err := complete.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	if incompleteResult.InputsDigest == completeResult.InputsDigest {
		t.Fatal("history completeness is missing from the pinned inputs digest")
	}
	pinned.HistoryComplete = false
	if _, err := pinned.Compute(asOf); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("forged completeness was accepted: %v", err)
	}
}
func TestTodo_SERVICE_002_Conformance(t *testing.T) {
	for _, tc := range []struct {
		name, secondStart string
		bridged           int64
		status            CalculationStatus
	}{{"at threshold", "2026-02-06", 5, StatusKnown}, {"past threshold", "2026-02-07", 0, StatusPartial}} {
		t.Run(tc.name, func(t *testing.T) {
			first := servicePeriod(t, "first", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/first", ""))
			second := servicePeriod(t, "second", tc.secondStart, "2026-03-01", employmentCredit(t, CreditEmployment, "evidence/second", ""))
			rule := serviceRule(t)
			rule.Unit, rule.DaysPerUnit, rule.Rounding = UnitDays, 1, RoundExact
			asOf, _ := NewAsOf(serviceDate(t, "2026-03-01"), serviceKnown(t))
			snapshot, err := ComputeSeniority([]ServicePeriod{first, second}, asOf, []SeniorityRule{rule})
			if err != nil {
				t.Fatal(err)
			}
			got := snapshot.Measures[0]
			if got.BridgedDays != tc.bridged || got.Status != tc.status {
				t.Fatalf("measure=%+v", got)
			}
		})
	}
}

func TestTodo_SERVICE_002_ContinuousRunResetsAtUnbridgedAndTrailingBreaks(t *testing.T) {
	rule := serviceRule(t)
	rule.Unit, rule.DaysPerUnit, rule.Rounding = UnitDays, 1, RoundExact
	asOf, _ := NewAsOf(serviceDate(t, "2026-03-02"), serviceKnown(t))
	allowed := []ServicePeriod{
		servicePeriod(t, "allowed-first", "2026-01-02", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/allowed-first", "")),
		servicePeriod(t, "allowed-second", "2026-02-06", "2026-03-02", employmentCredit(t, CreditEmployment, "evidence/allowed-second", "")),
	}
	refused := []ServicePeriod{
		servicePeriod(t, "refused-first", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/refused-first", "")),
		servicePeriod(t, "refused-second", "2026-02-07", "2026-03-02", employmentCredit(t, CreditEmployment, "evidence/refused-second", "")),
	}
	allowedResult, err := ComputeSeniority(allowed, asOf, []SeniorityRule{rule})
	if err != nil {
		t.Fatal(err)
	}
	refusedResult, err := ComputeSeniority(refused, asOf, []SeniorityRule{rule})
	if err != nil {
		t.Fatal(err)
	}
	a, b := allowedResult.Measures[0], refusedResult.Measures[0]
	if a.RawDays != b.RawDays || a.RawDays != 54 || a.TotalCreditedDays != 59 || a.ContinuousDays != 59 || a.AdjustedDate.String() != "2026-01-02" {
		t.Fatalf("allowed bridge=%+v", a)
	}
	if b.TotalCreditedDays != 54 || b.ContinuousDays != 23 || b.AdjustedDate.String() != "2026-02-07" || b.Status != StatusPartial {
		t.Fatalf("refused bridge did not reset continuous run: %+v", b)
	}

	termination := servicePeriod(t, "termination", "2026-02-01", "2026-02-05", employmentCredit(t, CreditEmployment, "evidence/termination", ""))
	termination.Break = BreakTermination
	trailingRule := rule
	trailingRule.Bridge = BridgeRule{MaxGapDays: 3, BreakTypes: []BreakType{BreakTermination}, EvidenceRef: "policy/trailing"}
	trailingAsOf, _ := NewAsOf(serviceDate(t, "2026-02-05"), serviceKnown(t))
	trailing, err := ComputeSeniority([]ServicePeriod{
		servicePeriod(t, "employment", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/employment", "")), termination,
	}, trailingAsOf, []SeniorityRule{trailingRule})
	if err != nil {
		t.Fatal(err)
	}
	if got := trailing.Measures[0]; got.RawDays != 31 || got.TotalCreditedDays != 31 || got.ContinuousDays != 0 || got.AdjustedDate.String() != "2026-02-05" || got.Status != StatusPartial {
		t.Fatalf("trailing termination did not end continuous service: %+v", got)
	}

	boundaryAsOf, _ := NewAsOf(serviceDate(t, "2026-02-01"), serviceKnown(t))
	boundary, err := ComputeSeniority([]ServicePeriod{servicePeriod(t, "boundary", "2026-01-01", "2026-02-01", employmentCredit(t, CreditEmployment, "evidence/boundary", ""))}, boundaryAsOf, []SeniorityRule{rule})
	if err != nil {
		t.Fatal(err)
	}
	if got := boundary.Measures[0]; got.ContinuousDays != 31 || got.AdjustedDate.String() != "2026-01-01" || len(got.Breaks) != 0 {
		t.Fatalf("period ending exactly at as-of created a trailing break: %+v", got)
	}
}

func TestTodo_SERVICE_002_Mutation(t *testing.T) {
	period := servicePeriod(t, "benefits-only", "2024-02-29", "2024-03-01", employmentCredit(t, CreditEmployment, "evidence/leap", ""))
	period.Dimensions = []SeniorityDimension{DimensionBenefits}
	rules := []SeniorityRule{
		{Dimension: DimensionGeneral, Unit: UnitDays, DaysPerUnit: 1, Rounding: RoundExact, Revision: serviceRevision(t, "rule/general", 1), EvidenceRef: "policy/general"},
		{Dimension: DimensionBenefits, Unit: UnitDays, DaysPerUnit: 1, Rounding: RoundExact, Revision: serviceRevision(t, "rule/benefits", 1), EvidenceRef: "policy/benefits"},
	}
	asOf, _ := NewAsOf(serviceDate(t, "2024-03-01"), serviceKnown(t))
	model, err := NewServiceModel([]ServicePeriod{period}, rules)
	if err != nil {
		t.Fatal(err)
	}
	model = scopedServiceModel(t, model, "00000000-0000-0000-0000-000000000001")
	peer := servicePeriod(t, "peer-general", "2024-01-01", "2024-03-01", employmentCredit(t, CreditEmployment, "evidence/peer", ""))
	peer.EmploymentID = ""
	peer.EmploymentRef = values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: "00000000-0000-0000-0000-000000000002"}
	model, err = model.WithPeerPopulation([]SeniorityPeer{{SubjectID: "peer-general", Periods: []ServicePeriod{peer}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := model.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	general := serviceMeasure(t, snapshot, DimensionGeneral)
	benefits := serviceMeasure(t, snapshot, DimensionBenefits)
	if general.Status != StatusUnknown || general.RankStatus != StatusUnknown || general.SeniorityRank != 0 || benefits.AdjustedDate.String() != "2024-02-29" || benefits.RankStatus != StatusUnknown || benefits.SeniorityRank != 0 || !strings.Contains(strings.Join(benefits.Trace, ","), "rank:unknown-peer-history") {
		t.Fatalf("unknown/leap measures=%+v", snapshot.Measures)
	}
}

func TestServiceCorrectionAppendsHistoryAndReevaluatesOnlyDependentEligibility(t *testing.T) {
	current := validServiceModel(t)
	replacementPeriods := clonePeriods(current.Periods)
	replacementPeriods[1].Interval = serviceInterval(t, "2026-03-05", "2026-06-01")
	replacementPeriods[1].ParentDigest = canonicalbytes.Digest(current.Periods[1].Canonical())
	replacementPeriods[1].Revision = serviceRevision(t, current.Periods[1].Revision.Stream(), 2)
	replacement, err := NewServiceModel(replacementPeriods, current.Rules)
	if err != nil {
		t.Fatal(err)
	}
	asOf, _ := NewAsOf(serviceDate(t, "2026-06-15"), serviceKnown(t))
	previousResult, err := current.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	deps := []ServiceDependency{
		{ID: "benefit-1", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: serviceInterval(t, "2026-01-01", "2026-06-01"), RuleRelease: "benefits/1", OutcomeProtected: true},
		{ID: "unrelated-worker", WorkerRef: "worker-2", Dimension: DimensionGeneral, Interval: serviceInterval(t, "2026-01-01", "2026-06-01"), RuleRelease: "benefits/1"},
		{ID: "unaffected-period", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: serviceInterval(t, "2025-01-01", "2025-02-01"), RuleRelease: "benefits/1"},
	}
	plan, err := CorrectService(ServiceCorrectionRequest{Current: current, Replacement: replacement, AsOf: asOf, WorkerRef: "worker-1", ExpectedCurrentDigest: current.CanonicalDigest, ExpectedPreviousResultDigest: previousResult.CanonicalDigest, Dependencies: deps})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Impacts) != 1 || plan.Impacts[0].Dependency.ID != "benefit-1" || plan.Impacts[0].Kind != IntentGovernedCorrectionRequired || !plan.Draft {
		t.Fatalf("scoped impacts=%+v", plan.Impacts)
	}
	if plan.PreviousExplanation.PeriodCount != len(current.Periods) || plan.PreviousResult.CanonicalDigest == plan.SuccessorResult.CanonicalDigest {
		t.Fatal("correction did not preserve old explanation and produce successor result")
	}
	if plan.Previous.CanonicalDigest == plan.Successor.CanonicalDigest || plan.CanonicalDigest == "" {
		t.Fatal("correction is not append-only")
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
}

func correctionFixture(t *testing.T, dimensions []SeniorityDimension, dependencies []ServiceDependency) ServiceCorrectionRequest {
	t.Helper()
	period := servicePeriod(t, "corrected", "2026-01-01", "2026-06-01", employmentCredit(t, CreditEmployment, "evidence/corrected", ""))
	period.Dimensions = append([]SeniorityDimension(nil), dimensions...)
	rules := make([]SeniorityRule, len(dimensions))
	for i, dimension := range dimensions {
		rules[i] = SeniorityRule{Dimension: dimension, Unit: UnitDays, DaysPerUnit: 1, Rounding: RoundExact, Revision: serviceRevision(t, "correction.rule."+string(dimension), 1), EvidenceRef: "policy/" + string(dimension)}
	}
	current, err := NewServiceModel([]ServicePeriod{period}, rules)
	if err != nil {
		t.Fatal(err)
	}
	next := period
	next.Interval = serviceInterval(t, "2026-01-01", "2026-07-01")
	next.ParentDigest = canonicalbytes.Digest(period.Canonical())
	next.Revision = serviceRevision(t, period.Revision.Stream(), 2)
	replacement, err := NewServiceModel([]ServicePeriod{next}, rules)
	if err != nil {
		t.Fatal(err)
	}
	asOf, _ := NewAsOf(serviceDate(t, "2026-07-01"), serviceKnown(t))
	previous, err := current.Compute(asOf)
	if err != nil {
		t.Fatal(err)
	}
	return ServiceCorrectionRequest{Current: current, Replacement: replacement, AsOf: asOf, WorkerRef: "worker-1", ExpectedCurrentDigest: current.CanonicalDigest, ExpectedPreviousResultDigest: previous.CanonicalDigest, Dependencies: dependencies}
}

func TestTodo_SERVICE_003_Property(t *testing.T) {
	interval := serviceInterval(t, "2026-06-01", "2026-07-01")
	deps := []ServiceDependency{{ID: "z", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: interval, RuleRelease: "general/1"}, {ID: "a", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: interval, RuleRelease: "general/1"}, {ID: "m", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: interval, RuleRelease: "general/1"}}
	first, err := CorrectService(correctionFixture(t, []SeniorityDimension{DimensionGeneral}, deps))
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(deps)
	second, err := CorrectService(correctionFixture(t, []SeniorityDimension{DimensionGeneral}, deps))
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest || len(first.Impacts) != 3 || first.Impacts[0].Dependency.ID != "a" || first.Impacts[2].Dependency.ID != "z" {
		t.Fatalf("dependency permutation changed plan: first=%+v second=%+v", first.Impacts, second.Impacts)
	}
}
func TestTodo_SERVICE_003_Golden(t *testing.T) {
	dep := ServiceDependency{ID: "benefit", WorkerRef: "worker-1", Dimension: DimensionBenefits, Interval: serviceInterval(t, "2026-06-01", "2026-07-01"), RuleRelease: "benefits/1", OutcomeProtected: true}
	plan, err := CorrectService(correctionFixture(t, []SeniorityDimension{DimensionBenefits}, []ServiceDependency{dep}))
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanonicalDigest != "sha256:44d008632bedbef5b5fb0db9f9c3f26f8096f851733f7e5bfebe2a0b7446206e" || canonicalbytes.Digest(plan.canonical()) != plan.CanonicalDigest {
		t.Fatalf("correction golden digest=%s", plan.CanonicalDigest)
	}
}
func TestTodo_SERVICE_003_Race(t *testing.T) {
	dep := ServiceDependency{ID: "leave", WorkerRef: "worker-1", Dimension: DimensionLeave, Interval: serviceInterval(t, "2026-06-01", "2026-07-01"), RuleRelease: "leave/1"}
	req := correctionFixture(t, []SeniorityDimension{DimensionLeave}, []ServiceDependency{dep})
	type result struct {
		digest string
		err    error
	}
	results := make(chan result, 12)
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan, err := CorrectService(req)
			results <- result{digest: plan.CanonicalDigest, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var digest string
	for result := range results {
		if result.err != nil || result.digest == "" {
			t.Fatalf("concurrent correction result=%+v", result)
		}
		if digest != "" && result.digest != digest {
			t.Fatalf("concurrent digests differ: %s != %s", result.digest, digest)
		}
		digest = result.digest
	}
}
func TestTodo_SERVICE_003_Fault(t *testing.T) {
	if _, err := CorrectService(ServiceCorrectionRequest{}); !errors.Is(err, ErrCorrectionNotGoverned) {
		t.Fatalf("ungoverned correction err=%v", err)
	}
	current := validServiceModel(t)
	asOf, _ := NewAsOf(serviceDate(t, "2026-06-15"), serviceKnown(t))
	previous, _ := current.Compute(asOf)
	if _, err := CorrectService(ServiceCorrectionRequest{Current: current, Replacement: current, AsOf: asOf, WorkerRef: "worker-1", ExpectedCurrentDigest: "sha256:stale", ExpectedPreviousResultDigest: previous.CanonicalDigest}); !errors.Is(err, ErrCorrectionStale) {
		t.Fatalf("stale current pin err=%v", err)
	}
}
func TestTodo_SERVICE_003_Security(t *testing.T) {
	current := validServiceModel(t)
	replacementPeriods := clonePeriods(current.Periods)
	replacementPeriods[1].Interval = serviceInterval(t, "2026-03-05", "2026-06-01")
	replacement, err := NewServiceModel(replacementPeriods, current.Rules)
	if err != nil {
		t.Fatal(err)
	}
	asOf, _ := NewAsOf(serviceDate(t, "2026-06-15"), serviceKnown(t))
	previous, _ := current.Compute(asOf)
	_, err = CorrectService(ServiceCorrectionRequest{Current: current, Replacement: replacement, AsOf: asOf, WorkerRef: "worker-1", ExpectedCurrentDigest: current.CanonicalDigest, ExpectedPreviousResultDigest: previous.CanonicalDigest})
	if !errors.Is(err, ErrCorrectionLineage) {
		t.Fatalf("in-place rewrite without successor lineage err=%v", err)
	}
}
func TestTodo_SERVICE_003_Conformance(t *testing.T) {
	interval := serviceInterval(t, "2026-06-01", "2026-07-01")
	deps := []ServiceDependency{
		{ID: "leave", WorkerRef: "worker-1", Dimension: DimensionLeave, Interval: interval, RuleRelease: "leave/1"},
		{ID: "benefit", WorkerRef: "worker-1", Dimension: DimensionBenefits, Interval: interval, RuleRelease: "benefits/1"},
		{ID: "pto", WorkerRef: "worker-1", Dimension: DimensionLeave, Interval: interval, RuleRelease: "pto/1"},
		{ID: "severance", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: interval, RuleRelease: "severance/1", OutcomeProtected: true},
		{ID: "promotion", WorkerRef: "worker-1", Dimension: DimensionPay, Interval: interval, RuleRelease: "promotion/1"},
		{ID: "unrelated-worker", WorkerRef: "worker-2", Dimension: DimensionGeneral, Interval: interval, RuleRelease: "severance/1"},
	}
	plan, err := CorrectService(correctionFixture(t, []SeniorityDimension{DimensionGeneral, DimensionBenefits, DimensionLeave, DimensionPay}, deps))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Impacts) != 5 {
		t.Fatalf("conformance impacts=%+v", plan.Impacts)
	}
	for _, id := range []string{"benefit", "leave", "promotion", "pto", "severance"} {
		if !slices.ContainsFunc(plan.Impacts, func(impact ServiceReevaluationIntent) bool { return impact.Dependency.ID == id }) {
			t.Fatalf("missing %s impact: %+v", id, plan.Impacts)
		}
	}
}
func TestTodo_SERVICE_003_Mutation(t *testing.T) {
	current := validServiceModel(t)
	asOf, _ := NewAsOf(serviceDate(t, "2026-06-15"), serviceKnown(t))
	previous, _ := current.Compute(asOf)
	_, err := CorrectService(ServiceCorrectionRequest{Current: current, Replacement: current, AsOf: asOf, WorkerRef: "worker-1", ExpectedCurrentDigest: current.CanonicalDigest, ExpectedPreviousResultDigest: previous.CanonicalDigest})
	if !errors.Is(err, ErrCorrectionNoChange) {
		t.Fatalf("no-op correction err=%v", err)
	}
}

func TestTodo_SERVICE_003_RemovedScopeAndRuleChangesInvalidateDependencies(t *testing.T) {
	general := serviceRule(t)
	general.Unit, general.DaysPerUnit, general.Rounding = UnitDays, 1, RoundExact
	benefits := general
	benefits.Dimension = DimensionBenefits
	benefits.Revision = serviceRevision(t, "service.rule.bfits", 1)
	period := servicePeriod(t, "scope", "2026-01-01", "2026-06-01", employmentCredit(t, CreditEmployment, "evidence/scope", ""))
	period.Dimensions = []SeniorityDimension{DimensionGeneral, DimensionBenefits}
	current, err := NewServiceModel([]ServicePeriod{period}, []SeniorityRule{general, benefits})
	if err != nil {
		t.Fatal(err)
	}
	generalTail := ServiceDependency{ID: "general-tail", WorkerRef: "worker-1", Dimension: DimensionGeneral, Interval: serviceInterval(t, "2026-05-01", "2026-06-01"), RuleRelease: "general/1"}
	asOf, _ := NewAsOf(serviceDate(t, "2026-06-01"), serviceKnown(t))
	assertImpact := func(t *testing.T, replacement ServiceModel) {
		t.Helper()
		previous, err := current.Compute(asOf)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := CorrectService(ServiceCorrectionRequest{Current: current, Replacement: replacement, AsOf: asOf, WorkerRef: "worker-1", ExpectedCurrentDigest: current.CanonicalDigest, ExpectedPreviousResultDigest: previous.CanonicalDigest, Dependencies: []ServiceDependency{generalTail, {ID: "other-worker", WorkerRef: "worker-2", Dimension: DimensionGeneral, Interval: generalTail.Interval, RuleRelease: "general/1"}, {ID: "other-dimension", WorkerRef: "worker-1", Dimension: DimensionLeave, Interval: generalTail.Interval, RuleRelease: "leave/1"}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Impacts) != 1 || plan.Impacts[0].Dependency.ID != generalTail.ID {
			t.Fatalf("impacts=%+v", plan.Impacts)
		}
	}

	removedDimensionPeriod := period
	removedDimensionPeriod.Dimensions = []SeniorityDimension{DimensionBenefits}
	removedDimensionPeriod.ParentDigest = canonicalbytes.Digest(period.Canonical())
	removedDimensionPeriod.Revision = serviceRevision(t, period.Revision.Stream(), 2)
	removedDimension, err := NewServiceModel([]ServicePeriod{removedDimensionPeriod}, current.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if !dependencyTouchesCorrection(generalTail, current, removedDimension) {
		t.Fatal("dependency in removed dimension was skipped")
	}
	assertImpact(t, removedDimension)

	shortenedPeriod := period
	shortenedPeriod.Interval = serviceInterval(t, "2026-01-01", "2026-05-01")
	shortenedPeriod.ParentDigest = canonicalbytes.Digest(period.Canonical())
	shortenedPeriod.Revision = serviceRevision(t, period.Revision.Stream(), 2)
	shortened, err := NewServiceModel([]ServicePeriod{shortenedPeriod}, current.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if !dependencyTouchesCorrection(generalTail, current, shortened) {
		t.Fatal("dependency solely in removed period tail was skipped")
	}
	assertImpact(t, shortened)

	changedRules := cloneRules(current.Rules)
	changedRules[0].Bridge.MaxGapDays++
	changedRules[0].Revision = serviceRevision(t, changedRules[0].Revision.Stream(), 2)
	ruleOnly, err := NewServiceModel(current.Periods, changedRules)
	if err != nil {
		t.Fatal(err)
	}
	if !dependencyTouchesCorrection(generalTail, current, ruleOnly) {
		t.Fatal("rule-only dimension change was skipped")
	}
	assertImpact(t, ruleOnly)
	unrelated := generalTail
	unrelated.Dimension = DimensionLeave
	if dependencyTouchesCorrection(unrelated, current, ruleOnly) {
		t.Fatal("unrelated dimension was invalidated")
	}
}
