package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// journeyProposalFixture is a complete, valid manager input.
func journeyProposalFixture() workspace.ProposalInput {
	return workspace.ProposalInput{
		WorkerRef:        "omar-reyes",
		TargetJobCode:    "OPS-HRBP3",
		TargetGrade:      "P3",
		TargetPositionID: "POS-HRBP-301",
		ProposedBase:     "98000.00",
		EffectiveDate:    "2026-06-01",
		BusinessReason:   "promotion_into_senior_hrbp",
	}
}

func TestTodo_PROMOUX_015_PositionRevisionHasCanonicalSubject(t *testing.T) {
	const id = "44444444-4444-4444-8444-444444444444"
	entity := values.EntityRef{Tenant: fixtures.Tenant, Kind: position.KindPosition, Id: id}
	revision, err := values.NewOpaqueRevision("aggregate.job_position."+id, []byte("seeded-target-position"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := position.EncodeRevisionRef(entity, revision)
	if err != nil {
		t.Fatal(err)
	}
	subjects := journeySubjects("omar-reyes", selected.String())
	if len(subjects) != 2 || subjects[1].GetSubjectId() != id {
		t.Fatalf("position subject = %+v, want canonical entity id %s", subjects, id)
	}
	if err := (intent.SubjectReference{Kind: subjects[1].GetSubjectKind(), SubjectID: subjects[1].GetSubjectId(), AuthorityDomain: subjects[1].GetAuthorityDomain()}).Validate(); err != nil {
		t.Fatalf("picker-issued position produced an inadmissible intent subject: %v", err)
	}
	if got := journeySubjects("omar-reyes", "POS-HRBP-301")[1].GetSubjectId(); got != "POS-HRBP-301" {
		t.Fatalf("malformed position must remain available for the domain's not-found refusal, got %q", got)
	}
}

func TestValidateProposalInputAcceptsACompleteForm(t *testing.T) {
	if err := validateProposalInput(journeyProposalFixture()); err != nil {
		t.Fatalf("validateProposalInput(complete) = %v, want nil", err)
	}
}

func TestPublishedPromotionPathGuardsRoleAndExactBaseIncrease(t *testing.T) {
	current := journeyCurrent{jobCode: "OPS-HRBP2", grade: "P2"}
	baseline := journeyBaselineFacts{currentBase: "93000.00", currency: "USD"}

	valid := journeyProposalFixture()
	if err := validatePublishedPromotionPath(current, valid, baseline); err != nil {
		t.Fatalf("valid published edge: %v", err)
	}

	unrelated := valid
	unrelated.TargetJobCode, unrelated.TargetGrade = "CLN-NURSE4", "N4"
	if err := validatePublishedPromotionPath(current, unrelated, baseline); !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "published next step") {
		t.Fatalf("unrelated payable role error = %v", err)
	}

	belowMinimum := valid
	belowMinimum.ProposedBase = "94000.00"
	if err := validatePublishedPromotionPath(current, belowMinimum, baseline); !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "0.0500") {
		t.Fatalf("below-minimum base error = %v", err)
	}

	aboveMaximum := valid
	aboveMaximum.ProposedBase = "108000.00"
	if err := validatePublishedPromotionPath(current, aboveMaximum, baseline); !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "0.1500") {
		t.Fatalf("above-maximum base error = %v", err)
	}
}

func TestPublishedDemoPromotionPathsAreAdmittedByTheServerLadder(t *testing.T) {
	options, err := workforceOptions()
	if err != nil {
		t.Fatal(err)
	}
	baseline := journeyBaselineFacts{currentBase: "135000.00", currency: "USD"}
	checked := 0
	for _, edge := range demoworkforce.PromotionPaths() {
		published := false
		for _, option := range options.PromotionPaths {
			if option.SourceJobCode == edge.SourceJobCode && option.SourceGrade == edge.SourceGrade &&
				option.TargetJobCode == edge.TargetJobCode && option.TargetGrade == edge.TargetGrade {
				published = true
				break
			}
		}
		if !published {
			t.Errorf("demo edge %s -> %s is missing from options", edge.SourceJobCode, edge.TargetJobCode)
			continue
		}
		current := journeyCurrent{orgUnit: edge.OrgUnit, jobCode: edge.SourceJobCode, grade: edge.SourceGrade}
		in := workspace.ProposalInput{TargetJobCode: edge.TargetJobCode, TargetGrade: edge.TargetGrade, ProposedBase: "170000.00"}
		if err := validatePublishedPromotionPath(current, in, baseline); err != nil {
			t.Errorf("published demo edge %s -> %s was refused: %v", edge.SourceJobCode, edge.TargetJobCode, err)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no demo ladder edges were checked")
	}
	current := journeyCurrent{orgUnit: "sales", jobCode: "SAL-AE3", grade: "P4"}
	in := workspace.ProposalInput{TargetJobCode: "SAL-DIR", TargetGrade: "M4", ProposedBase: "not-money"}
	err = validatePublishedPromotionPath(current, in, baseline)
	var input *workspace.JourneyInputError
	if !errors.As(err, &input) || input.FieldPath != "proposed_base" || input.ReasonRef != "promotion.base_pay.not_exact" {
		t.Fatalf("invalid demo salary refusal = %v, want typed pay correction", err)
	}
	for _, tc := range []struct {
		pay   string
		valid bool
	}{
		{"141749.99", false}, {"141750.00", true},
		{"202500.00", true}, {"202500.01", false},
	} {
		in.ProposedBase = tc.pay
		err := validatePublishedPromotionPath(current, in, baseline)
		if (err == nil) != tc.valid {
			t.Errorf("demo pay %s admissible=%v, want %v (err=%v)", tc.pay, err == nil, tc.valid, err)
		}
	}
	for _, pay := range []string{"141749.999", "141750.001"} {
		in.ProposedBase = pay
		err := validatePublishedPromotionPath(current, in, baseline)
		var input *workspace.JourneyInputError
		if !errors.As(err, &input) || input.ReasonRef != "promotion.base_pay.not_exact" {
			t.Errorf("sub-cent pay %s was not refused as inexact: %v", pay, err)
		}
	}
	current.orgUnit = "not-sales"
	in.ProposedBase = "170000.00"
	if err := validatePublishedPromotionPath(current, in, baseline); !errors.Is(err, workspace.ErrJourneyInput) {
		t.Fatalf("cross-organization demo edge was admitted: %v", err)
	}
}

func TestTodo_PROMOUX_007_ServerMatchesDisplayedCentBounds(t *testing.T) {
	minimum, err := values.NewPercentage("0.0500", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewPercentage("0.1800", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	baseline := journeyBaselineFacts{currentBase: "100.03", currency: "USD"}
	for _, tc := range []struct {
		pay   string
		valid bool
	}{
		{"105.03", false}, {"105.04", true}, {"118.03", true}, {"118.04", false},
	} {
		err := validatePublishedBaseIncrease(tc.pay, baseline, minimum, maximum)
		if (err == nil) != tc.valid {
			t.Errorf("%s admitted=%v, want %v: %v", tc.pay, err == nil, tc.valid, err)
		}
		if !tc.valid {
			var input *workspace.JourneyInputError
			if !errors.As(err, &input) || input.PayRange == nil ||
				input.PayRange.Minimum.String() != "105.04 USD" || input.PayRange.Maximum.String() != "118.03 USD" {
				t.Fatalf("%s refusal lost exact server bounds: %+v", tc.pay, input)
			}
		}
	}
}

func TestValidateProposalInputNamesTheMissingField(t *testing.T) {
	// target_position_id is deliberately absent from this table: PROMOUX-004
	// made it optional (a non-empty value is now checked against the real
	// Position domain instead of merely required to be present), and
	// TestValidateProposalInputAcceptsAFormWithNoTargetPosition covers that
	// case explicitly.
	cases := map[string]func(*workspace.ProposalInput){
		"worker_ref":      func(in *workspace.ProposalInput) { in.WorkerRef = "  " },
		"target_job_code": func(in *workspace.ProposalInput) { in.TargetJobCode = "" },
		"target_grade":    func(in *workspace.ProposalInput) { in.TargetGrade = "" },
		"proposed_base":   func(in *workspace.ProposalInput) { in.ProposedBase = "" },
		"effective_date":  func(in *workspace.ProposalInput) { in.EffectiveDate = "" },
		"business_reason": func(in *workspace.ProposalInput) { in.BusinessReason = "" },
	}
	for field, mutate := range cases {
		t.Run(field, func(t *testing.T) {
			in := journeyProposalFixture()
			mutate(&in)
			err := validateProposalInput(in)
			if !errors.Is(err, workspace.ErrJourneyInput) {
				t.Fatalf("validateProposalInput = %v, want ErrJourneyInput", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("refusal %q does not name the field %q", err, field)
			}
		})
	}
}

// TestValidateProposalInputAcceptsAFormWithNoTargetPosition proves
// target_position_id is genuinely optional: checkPlacement already accepts
// a job/grade/org-only placement on its own, and PROMOUX-004 does not make
// the field mandatory again -- it makes a non-empty value mean something
// checkable.
func TestValidateProposalInputAcceptsAFormWithNoTargetPosition(t *testing.T) {
	in := journeyProposalFixture()
	in.TargetPositionID = ""
	if err := validateProposalInput(in); err != nil {
		t.Fatalf("validateProposalInput(no target position) = %v, want nil", err)
	}
}

func TestValidateProposalInputRefusesAMalformedEffectiveDate(t *testing.T) {
	in := journeyProposalFixture()
	in.EffectiveDate = "01/06/2026"
	err := validateProposalInput(in)
	if !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "effective_date") {
		t.Fatalf("validateProposalInput(bad date) = %v, want ErrJourneyInput naming effective_date", err)
	}
}

func TestJourneyErrorProjectsOwnedRefusalsOntoThePortSentinels(t *testing.T) {
	cases := []struct {
		name string
		err  *envelope.Error
		want error
	}{
		{"denied", envelope.New(envelope.CodePermissionDenied, reasonExecutionRoleRequired, "no"), workspace.ErrDenied},
		{"unknown", envelope.New(envelope.CodeNotFound, reasonIntentNotFound, "no"), workspace.ErrJourneyUnknown},
		{"stage", envelope.New(envelope.CodeFailedPrecondition, reasonNoExecutablePlan, "no"), workspace.ErrJourneyStage},
		{"unavailable", envelope.New(envelope.CodeFailedPrecondition, reasonExecutionUnavailable, "no"), workspace.ErrJourneyUnavailable},
		{"p1a ceiling", envelope.New(envelope.CodeFailedPrecondition, reasonNoGovernedWrite, "no"), workspace.ErrJourneyUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := journeyError(tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("journeyError(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	if journeyError(nil) != nil {
		t.Fatal("journeyError(nil) must stay nil")
	}
	plain := errors.New("not owned")
	if got := journeyError(plain); !errors.Is(got, plain) {
		t.Fatalf("journeyError(plain) = %v, want the original error", got)
	}
}

func TestJourneyErrorLeavesAnUnclassifiedRefusalOwned(t *testing.T) {
	owned := envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable, "no")
	got := journeyError(owned)
	for _, sentinel := range []error{
		workspace.ErrDenied, workspace.ErrJourneyUnknown,
		workspace.ErrJourneyStage, workspace.ErrJourneyUnavailable,
	} {
		if errors.Is(got, sentinel) {
			t.Fatalf("journeyError(unavailable) = %v, must not claim %v", got, sentinel)
		}
	}
}

func TestJourneySanitizeProducesARevisionStreamSegment(t *testing.T) {
	cases := map[string]string{
		"omar-reyes":  "omar-reyes",
		"Omar Reyes":  "omar-reyes",
		"people ops!": "people-ops-",
		"   ":         "worker",
	}
	for in, want := range cases {
		if got := journeySanitize(in); got != want {
			t.Errorf("journeySanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJourneyInitiatorKindMapsEveryActorKind(t *testing.T) {
	cases := map[trust.SubjectKind]intentsv1.InitiatorKind{
		trust.SubjectKindHuman:       intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN,
		trust.SubjectKindService:     intentsv1.InitiatorKind_INITIATOR_KIND_SERVICE,
		trust.SubjectKindAgent:       intentsv1.InitiatorKind_INITIATOR_KIND_AGENT,
		trust.SubjectKindIntegration: intentsv1.InitiatorKind_INITIATOR_KIND_INTEGRATION,
	}
	for kind, want := range cases {
		if got := journeyInitiatorKind(kind); got != want {
			t.Errorf("journeyInitiatorKind(%v) = %v, want %v", kind, got, want)
		}
	}
	if got := journeyInitiatorKind(trust.SubjectKind(99)); got != intentsv1.InitiatorKind_INITIATOR_KIND_UNSPECIFIED {
		t.Errorf("journeyInitiatorKind(unknown) = %v, want UNSPECIFIED", got)
	}
}

func TestNewJourneyEngineDefaultsTheApproverAndTheClock(t *testing.T) {
	engine := newJourneyEngine(nil, nil, "", nil, nil)
	if engine.approver != DefaultJourneyApprover {
		t.Fatalf("approver = %q, want %q", engine.approver, DefaultJourneyApprover)
	}
	if engine.now == nil {
		t.Fatal("a journey engine composed with no clock must default one")
	}
	named := newJourneyEngine(nil, nil, "principal:someone-else", func() time.Time { return time.Unix(0, 0) }, nil)
	if named.approver != "principal:someone-else" {
		t.Fatalf("approver = %q, want the configured principal", named.approver)
	}
}

// journeyCorpusSubject is the resolved worker a corpus proposal is built for.
// It carries no created row, which is what makes the baseline come from the
// ported legacy corpus rather than from a durable record.
func journeyCorpusSubject(t *testing.T) WorkerLocation {
	t.Helper()
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("fixtures.WorkerRef: %v", err)
	}
	return WorkerLocation{Ref: ref, Key: "omar-reyes"}
}

// journeyBuiltPayload is the request bytes Propose would create for a
// complete form and a disclosed placement.
func journeyBuiltPayload(t *testing.T) []byte {
	t.Helper()
	baseline, err := journeyBaseline(journeyProposalFixture(), journeyCorpusSubject(t))
	if err != nil {
		t.Fatalf("journeyBaseline: %v", err)
	}
	wire, err := journeyRequestPayload(journeyProposalFixture(), "omar-reyes", journeyCurrent{
		name: "Omar", jobCode: "OPS-HRBP2", grade: "P2",
		orgUnit: "people-ops", positionID: "POS-HRBP-204", payZone: "US-EAST",
	}, baseline)
	if err != nil {
		t.Fatalf("journeyRequestPayload: %v", err)
	}
	return wire
}

func TestJourneyRequestPayloadCarriesTheGovernedAndCorpusFacts(t *testing.T) {
	payload, err := decodeStruct(journeyBuiltPayload(t))
	if err != nil {
		t.Fatalf("decodeStruct: %v", err)
	}
	// The manager's own fields.
	if got := optionalStr(payload, "business_reason"); got != "promotion_into_senior_hrbp" {
		t.Errorf("business_reason = %q", got)
	}
	target, err := fieldsOf(payload, "target")
	if err != nil {
		t.Fatalf("fieldsOf(target): %v", err)
	}
	if got := optionalStr(target, "job_code"); got != "OPS-HRBP3" {
		t.Errorf("target.job_code = %q, want the form's target", got)
	}
	// The organizational half comes from the governed read, never the form.
	if got := optionalStr(target, "org_unit"); got != "people-ops" {
		t.Errorf("target.org_unit = %q, want the disclosed placement", got)
	}
	if got := optionalStr(target, "pay_zone"); got != "US-EAST" {
		t.Errorf("target.pay_zone = %q, want the disclosed placement", got)
	}
	// The declared baseline comes from the corpus, never the form.
	current, err := fieldsOf(payload, "current")
	if err != nil {
		t.Fatalf("fieldsOf(current): %v", err)
	}
	if got := optionalStr(current, "base"); got == "" || got == "98000.00" {
		t.Errorf("current.base = %q, want the corpus baseline rather than the proposed amount", got)
	}
	proposed, err := fieldsOf(payload, "proposed")
	if err != nil {
		t.Fatalf("fieldsOf(proposed): %v", err)
	}
	if got := optionalStr(proposed, "base"); got != "98000.00" {
		t.Errorf("proposed.base = %q, want the form's amount", got)
	}
	// The whole payload is decodable by the same resolver path the wire
	// request uses: every field the promotion resolver requires is present.
	if _, err := compensationSnapshot(payload, "current"); err != nil {
		t.Fatalf("the built payload is not a decodable current snapshot: %v", err)
	}
	if _, err := compensationSnapshot(payload, "proposed"); err != nil {
		t.Fatalf("the built payload is not a decodable proposed snapshot: %v", err)
	}
	if _, err := targetPlacement(payload); err != nil {
		t.Fatalf("the built payload is not a decodable target placement: %v", err)
	}
	if _, err := budgetObservation(payload); err != nil {
		t.Fatalf("the built payload is not a decodable budget observation: %v", err)
	}
}

func TestJourneyRequestPayloadIsDeterministic(t *testing.T) {
	first, second := journeyBuiltPayload(t), journeyBuiltPayload(t)
	if string(first) != string(second) {
		t.Fatal("the same proposal must encode to the same request bytes; the canonical digest depends on it")
	}
}

func TestJourneySummaryFromProtoDerivesBothSidesOfTheChange(t *testing.T) {
	msg := &intentsv1.IntentInstance{
		IntentId:      "intent:journey-1",
		TenantId:      string(fixtures.Tenant),
		CorrelationId: "corr:journey-1",
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "EMPLOYMENT", SubjectId: "22222222-2222-4222-8222-222222222222", AuthorityDomain: "PEOPLE"},
		},
		Request: &intentsv1.TypedPayload{ProtobufWireBytes: journeyBuiltPayload(t)},
	}
	summary, err := journeySummaryFromProto(msg)
	if err != nil {
		t.Fatalf("journeySummaryFromProto: %v", err)
	}
	if summary.IntentID != "intent:journey-1" || summary.CorrelationID != "corr:journey-1" {
		t.Fatalf("summary identity = %+v", summary)
	}
	if summary.WorkerName != "Omar" {
		t.Errorf("WorkerName = %q, want the name the governed read disclosed", summary.WorkerName)
	}
	if summary.Worker.Id != "22222222-2222-4222-8222-222222222222" {
		t.Errorf("Worker.Id = %q, want the EMPLOYMENT subject", summary.Worker.Id)
	}
	if summary.Current.JobCode != "OPS-HRBP2" || summary.Current.Grade != "P2" {
		t.Errorf("Current = %+v, want the recorded current placement", summary.Current)
	}
	if summary.Target.JobCode != "OPS-HRBP3" || summary.Target.Grade != "P3" {
		t.Errorf("Target = %+v, want the proposed placement", summary.Target)
	}
	if summary.ProposedBase != "98000.00" || summary.Currency != "USD" {
		t.Errorf("pay = %q %q", summary.ProposedBase, summary.Currency)
	}
	if summary.CurrentBase == "" || summary.CurrentBase == summary.ProposedBase {
		t.Errorf("CurrentBase = %q, want the declared baseline", summary.CurrentBase)
	}
	if summary.EffectiveDate != "2026-06-01" {
		t.Errorf("EffectiveDate = %q", summary.EffectiveDate)
	}
	if summary.BusinessReason != "promotion_into_senior_hrbp" {
		t.Errorf("BusinessReason = %q", summary.BusinessReason)
	}
	// The stage and the execution identifiers are never derived from the
	// stored intent alone.
	if summary.Stage != "" || summary.InstanceID != "" {
		t.Errorf("summary asserted execution state it cannot know: %+v", summary)
	}
}

func TestJourneySummaryFromProtoRetainsStoredTenant(t *testing.T) {
	const sandboxTenant = "sandbox-promotion-regression"
	msg := &intentsv1.IntentInstance{
		IntentId: "intent:sandbox-journey", TenantId: sandboxTenant,
		Subjects: []*intentsv1.SubjectReference{{
			SubjectKind: "EMPLOYMENT", SubjectId: "22222222-2222-4222-8222-222222222222", AuthorityDomain: "PEOPLE",
		}},
		Request: &intentsv1.TypedPayload{ProtobufWireBytes: journeyBuiltPayload(t)},
	}
	summary, err := journeySummaryFromProto(msg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Worker.Tenant != values.TenantId(sandboxTenant) {
		t.Fatalf("worker tenant = %q, want stored intent tenant %q", summary.Worker.Tenant, sandboxTenant)
	}
	msg.TenantId = ""
	if _, err := journeySummaryFromProto(msg); err == nil {
		t.Fatal("stored intent without a tenant was accepted")
	}
}

func TestJourneySummaryFromProtoRefusesAnUnreadablePayload(t *testing.T) {
	if _, err := journeySummaryFromProto(nil); err == nil {
		t.Fatal("journeySummaryFromProto(nil) must refuse")
	}
	msg := &intentsv1.IntentInstance{
		IntentId: "intent:broken",
		TenantId: string(fixtures.Tenant),
		Request:  &intentsv1.TypedPayload{ProtobufWireBytes: []byte{0xff, 0xff, 0xff}},
	}
	if _, err := journeySummaryFromProto(msg); err == nil {
		t.Fatal("a payload that is not a Struct must refuse rather than render blank")
	}
}

func TestJourneyBaselineComesFromTheCertifiedCorpus(t *testing.T) {
	baseline, err := journeyBaseline(journeyProposalFixture(), journeyCorpusSubject(t))
	if err != nil {
		t.Fatalf("journeyBaseline: %v", err)
	}
	if baseline.currentBase == "" || baseline.currency == "" ||
		baseline.bonusTarget == "" || baseline.budgetAvailabe == "" || baseline.evaluationDate == "" {
		t.Fatalf("baseline is incomplete: %+v", baseline)
	}
	if baseline.effectiveText != "2026-06-01" {
		t.Fatalf("effective text = %q, want the form's own date", baseline.effectiveText)
	}
}

func TestJourneyBaselineRefusesAMalformedEffectiveDate(t *testing.T) {
	in := journeyProposalFixture()
	in.EffectiveDate = "not-a-date"
	if _, err := journeyBaseline(in, journeyCorpusSubject(t)); !errors.Is(err, workspace.ErrJourneyInput) {
		t.Fatalf("journeyBaseline(bad date) = %v, want ErrJourneyInput", err)
	}
}

func TestJourneyBaselineIsSpecificToTheResolvedWorker(t *testing.T) {
	in := journeyProposalFixture()
	in.WorkerRef = "omar-reyes" // The resolved subject, not form text, owns pay.
	jane, err := journeyBaseline(in, WorkerLocation{Key: "jane-doe"})
	if err != nil {
		t.Fatal(err)
	}
	if jane.currentBase != "165000.00" || jane.currency != "USD" || jane.bonusTarget != "0.1500" {
		t.Fatalf("Jane inherited another worker's baseline: %+v", jane)
	}
	in.TargetJobCode, in.TargetGrade, in.ProposedBase = "ENG-MGR1", "M1", "180000.00"
	if err := validatePublishedPromotionPath(journeyCurrent{jobCode: "ENG-SWE3", grade: "P3"}, in, jane); err != nil {
		t.Fatalf("Jane's certified proposal rejected: %v", err)
	}
	for _, key := range []string{"", "unknown-worker", "priya-shah"} {
		if _, err := journeyBaseline(in, WorkerLocation{Key: key}); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("worker %q inherited a baseline: %v", key, err)
		}
	}
}
