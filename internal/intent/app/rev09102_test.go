package app

// REV-091-02: Inspect computes the reviewer's reporting-line impact and the
// compensation guardrail from the journey's pinned request and the cell's
// own ports, under the viewer's authorization.
//
// TestTodo_REV_091_02 is the engine half of the PRIMARY: an authorized
// compensation administrator gets the exact guardrail the domain computes for
// the pinned request, and the target manager the org reader certifies, named.
// TestTodo_REV_091_02_Security proves a viewer the compensation policy does
// not admit receives the typed NOT_AUTHORIZED guardrail with every amount
// zero, and a viewer who may not see the reporting line gets no manager.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var rev09102At = time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

const (
	rev09102Subject = "22222222-2222-4222-8222-222222222222"
	rev09102Manager = "33333333-3333-4333-8333-333333333333"
	rev09102Report  = "44444444-4444-4444-8444-444444444444"
	// The worker's current position and the vacancy the request targets.
	rev09102CurrentPosition = "55555555-5555-4555-8555-555555555555"
	rev09102TargetPosition  = "66666666-6666-4666-8666-666666666666"
)

func rev09102Principal(t *testing.T, subject string, role authz.RoleID) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: demoworkforce.CompanyKey, Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "harborcare", Roles: []string{string(role)},
		Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-" + subject,
		IssuedAt: rev09102At.Add(-time.Hour), ExpiresAt: rev09102At.Add(time.Hour), CredentialDigest: "digest-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// rev09102Request is a pinned promotion request whose target band exists in
// the demo catalog and whose current pay is that band's midpoint.
func rev09102Request(t *testing.T) (*structValue, demoworkforce.PayBandSpec) {
	t.Helper()
	return rev09102RequestNaming(t, "")
}

// rev09102RequestNaming is rev09102Request with an explicitly requested new
// manager ("" names none).
func rev09102RequestNaming(t *testing.T, managerRef string) (*structValue, demoworkforce.PayBandSpec) {
	t.Helper()
	specs, err := demoworkforce.PayBandSpecs()
	if err != nil || len(specs) == 0 {
		t.Fatalf("PayBandSpecs: %v", err)
	}
	spec := specs[0]
	target := map[string]any{"job_code": spec.JobCode, "grade": spec.Grade, "org_unit": "ou-care", "pay_zone": spec.PayZone,
		"position_id": rev09102TargetPosition}
	if managerRef != "" {
		target["manager_ref"] = managerRef
	}
	raw, err := json.Marshal(map[string]any{
		"worker_ref": "rev09102-subject",
		"current": map[string]any{
			"base": spec.Midpoint.Amount().String(), "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"effective_date": "2026-09-01", "revision_stream": "rev09102.worker", "revision_sequence": 1,
		},
		"target":            target,
		"effective_date":    "2026-10-01",
		"current_placement": map[string]any{"position_id": rev09102CurrentPosition, "org_unit": "ou-care"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload protomap.Struct
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return &payload, spec
}

func rev09102Bands(t *testing.T) rewards.PayBandCatalog {
	t.Helper()
	inputs, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	return inputs.Bands()
}

func rev09102SubjectRef(t *testing.T) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: demoworkforce.CompanyKey, Kind: people.KindWorker, Id: rev09102Subject}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	return ref
}

// rev09102Engine is a journey engine with only the review's ports: a
// locator naming the subject and its recorded manager, and an org reader
// in which the manager reports to nobody.
func rev09102Engine(t *testing.T) *journeyEngine {
	t.Helper()
	tenant := values.TenantId(demoworkforce.CompanyKey)
	subjectRow := workforce.WorkerRow{WorkerID: uuid.MustParse(rev09102Subject), WorkerKey: "rev09102-subject",
		PreferredName: "Aya Tanaka", ManagerRelationshipRef: "rev09102-manager"}
	managerRow := workforce.WorkerRow{WorkerID: uuid.MustParse(rev09102Manager), WorkerKey: "rev09102-manager",
		LegalName: "Dana Lee"}
	reportRow := workforce.WorkerRow{WorkerID: uuid.MustParse(rev09102Report), WorkerKey: "rev09102-report",
		PreferredName: "Sam Ortiz", ManagerRelationshipRef: "rev09102-subject"}
	locate := func(_ context.Context, _ values.TenantId, ref string) (WorkerLocation, bool, error) {
		switch ref {
		case "rev09102-subject", rev09102Subject:
			row := subjectRow
			return WorkerLocation{Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: rev09102Subject}, Key: row.WorkerKey, Created: &row}, true, nil
		case "rev09102-manager", rev09102Manager:
			row := managerRow
			return WorkerLocation{Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: rev09102Manager}, Key: row.WorkerKey, Created: &row}, true, nil
		case "rev09102-report", rev09102Report:
			row := reportRow
			return WorkerLocation{Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: rev09102Report}, Key: row.WorkerKey, Created: &row}, true, nil
		}
		return WorkerLocation{}, false, nil
	}
	return &journeyEngine{
		locate: locate,
		now:    func() time.Time { return rev09102At },
		review: journeyReviewPorts{
			bands:        rev09102Bands(t),
			managerFacts: rev09102OrgFacts{managerOf: map[string]string{rev09102Subject: rev09102Manager}},
		},
	}
}

// rev09102Positions is a position directory that names both positions.
type rev09102Positions struct{}

func (rev09102Positions) Directory(_ context.Context, tenant values.TenantId, _ position.AsOf) ([]positionfacts.DirectoryRow, error) {
	return []positionfacts.DirectoryRow{
		{Position: values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: rev09102CurrentPosition}, Title: "Care Coordinator", Organization: "Care Operations", OrgUnit: "ou-care"},
		{Position: values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: rev09102TargetPosition}, Title: "Care Team Lead", Organization: "Care Operations", OrgUnit: "ou-care"},
	}, nil
}

type rev09102OrgFacts struct{ managerOf map[string]string }

func (f rev09102OrgFacts) WorkerFactsAt(_ context.Context, q org.WorkerFactsQuery) (org.WorkerFactSet, error) {
	if q.Worker.Id != rev09102Subject && q.Worker.Id != rev09102Manager && q.Worker.Id != rev09102Report {
		return org.WorkerFactSet{Worker: q.Worker}, nil
	}
	watermark, err := values.NewSequenceRevision("rev09102.graph", 1)
	if err != nil {
		return org.WorkerFactSet{}, err
	}
	set := org.WorkerFactSet{Worker: q.Worker, Exists: true, Watermark: watermark, PolicyVersion: "rev09102.policy/v1"}
	manager, ok := f.managerOf[q.Worker.Id]
	if !ok {
		return set, nil
	}
	effective, _ := values.NewOpenInstantInterval(values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	knownAt, _ := values.NewKnownAt(values.NewInstant(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	recordedAt, _ := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)))
	revision, _ := values.NewSequenceRevision("rev09102.relationship."+q.Worker.Id, 1)
	set.Relationships = []org.ManagerRelationshipFact{{
		RelationshipID: "rel_" + q.Worker.Id, Type: org.RelationshipDirectManager,
		Worker:       q.Worker,
		Manager:      values.EntityRef{Tenant: q.Tenant, Kind: people.KindWorker, Id: manager},
		AssignmentID: "asg_" + q.Worker.Id, Effective: effective, KnownAt: knownAt, Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "rev09102-test", PolicyRef: "rev09102-test/v1"},
		Provenance: evidence.Provenance{Source: "rev09102-test", EvidenceRef: "evd_" + q.Worker.Id, RecordedAt: recordedAt},
	}}
	return set, nil
}

func TestTodo_REV_091_02(t *testing.T) {
	ctx := context.Background()
	payload, spec := rev09102Request(t)
	subject := rev09102SubjectRef(t)
	admin := rev09102Principal(t, "comp-admin-1", authz.RoleCompAdmin)
	at := values.NewInstant(rev09102At)

	t.Run("an authorized reviewer gets the domain's exact guardrail for the pinned request", func(t *testing.T) {
		got := journeyCompensationGuardrail(ctx, rev09102Bands(t), admin, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, nil, payload)
		if !got.Available() {
			t.Fatalf("guardrail = %+v, want AVAILABLE for a compensation administrator", got)
		}
		current, err := compensationSnapshot(payload, "current")
		if err != nil {
			t.Fatal(err)
		}
		effective, _ := values.ParseLocalDate("2026-10-01")
		want, err := promotion.EvaluateCompensationGuardrail(ctx, promotion.CompensationGuardrailRequest{
			Current: current, Catalog: rev09102Bands(t), Annualization: rewards.DefaultAnnualization(),
			Target: rewards.BandQuery{Tenant: demoworkforce.CompanyKey, JobCode: spec.JobCode, Grade: spec.Grade,
				PayZone: spec.PayZone, Currency: "USD", AsOf: effective},
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Canonical()) != string(want.Canonical()) {
			t.Fatalf("guardrail = %+v, want the domain's own %+v", got, want)
		}
		if got.MaximumAnnualized.Amount().String() != spec.Maximum.Amount().String() || got.BandPosition.String() != "IN_BAND" {
			t.Fatalf("guardrail max %s position %s, want the band maximum %s and IN_BAND",
				got.MaximumAnnualized.Amount(), got.BandPosition, spec.Maximum.Amount())
		}
	})

	t.Run("an unknown role band is BAND_UNRESOLVED, never a guessed range", func(t *testing.T) {
		broken, _ := rev09102Request(t)
		target := broken.GetFields()["target"].GetStructValue()
		target.GetFields()["job_code"].Kind = &protomap.StringValue{StringValue: "NO-SUCH-JOB"}
		got := journeyCompensationGuardrail(ctx, rev09102Bands(t), admin, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, nil, broken)
		if got.Available() || got.Reason != promotion.GuardrailReasonBandUnresolved || !got.MaximumAnnualized.Amount().IsZero() {
			t.Fatalf("guardrail = %+v, want an unavailable BAND_UNRESOLVED with no amounts", got)
		}
		if nilCatalog := journeyCompensationGuardrail(ctx, nil, admin, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, nil, payload); nilCatalog.Reason != promotion.GuardrailReasonBandUnresolved {
			t.Fatalf("no catalog = %+v, want BAND_UNRESOLVED", nilCatalog)
		}
	})

	t.Run("the recorded manager is certified by the org reader and named", func(t *testing.T) {
		engine := rev09102Engine(t)
		impact, name := engine.journeyManagementImpact(ctx, admin, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, payload)
		if !impact.Evaluated() || impact.TargetManager.Id != rev09102Manager || !impact.CycleSafe || name != "Dana Lee" {
			t.Fatalf("impact = %+v name = %q, want the certified manager Dana Lee", impact, name)
		}
	})

	t.Run("a named manager who reports to the worker closes a cycle and is not certified", func(t *testing.T) {
		// The request names the worker's own direct report as the new
		// manager: report -> subject already, so subject -> report is a loop.
		engine := rev09102Engine(t)
		engine.review.managerFacts = rev09102OrgFacts{managerOf: map[string]string{rev09102Subject: rev09102Manager, rev09102Report: rev09102Subject}}
		named, _ := rev09102RequestNaming(t, "rev09102-report")
		impact, name := engine.journeyManagementImpact(ctx, admin, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, named)
		if impact.TargetManager.Id != rev09102Report || name != "Sam Ortiz" {
			t.Fatalf("impact = %+v name = %q, want the named manager Sam Ortiz", impact, name)
		}
		if !impact.Evaluated() || impact.CycleSafe {
			t.Fatalf("impact = %+v, want an evaluated impact that is not cycle safe", impact)
		}
	})

	t.Run("the whole review is assembled from the stored request", func(t *testing.T) {
		engine := rev09102Engine(t)
		wire, err := protomap.MarshalDeterministic(payload)
		if err != nil {
			t.Fatal(err)
		}
		msg := rev09102Instance(wire)
		review := engine.journeyPromotionReview(ctx, admin, authz.PurposeCompensationReview, msg, subject, at, nil)
		if review == nil || !review.Guardrail.Available() || !review.ManagerDisclosed() || review.TargetManagerName != "Dana Lee" {
			t.Fatalf("review = %+v, want an available guardrail and a named manager", review)
		}
		if !review.ManagerUnchanged {
			t.Fatal("a request that names no new manager was not reported as keeping the current manager")
		}
		if review.CurrentPositionTitle != "" || review.TargetPositionTitle != "" {
			t.Fatalf("a cell with no position directory named positions: %+v", review)
		}
		engine.positions = rev09102Positions{}
		named, _ := rev09102RequestNaming(t, "rev09102-manager")
		labelled := engine.journeyPromotionReview(ctx, admin, authz.PurposeCompensationReview, rev09102Instance(mustWire(t, named)), subject, at, nil)
		if labelled.ManagerUnchanged {
			t.Fatal("a request naming a manager was reported as keeping the current one")
		}
		if labelled.CurrentPositionTitle != "Care Coordinator" || labelled.TargetPositionTitle != "Care Team Lead" ||
			labelled.TargetOrganizationName != "Care Operations" {
			t.Fatalf("labels = %q / %q / %q, want the directory's titles and organization name",
				labelled.CurrentPositionTitle, labelled.TargetPositionTitle, labelled.TargetOrganizationName)
		}
		if engine.journeyPromotionReview(ctx, admin, authz.PurposeCompensationReview, rev09102Instance([]byte{0xff, 0xff}), subject, at, nil) != nil {
			t.Fatal("an undecodable stored request produced a review")
		}
	})
}

func TestTodo_REV_091_02_Security(t *testing.T) {
	ctx := context.Background()
	payload, _ := rev09102Request(t)
	subject := rev09102SubjectRef(t)
	at := values.NewInstant(rev09102At)
	// An unassigned manager is refused the subject's compensation by the
	// bootstrap policy (see TestRecordedJourneyInspectionStillRequires...),
	// though the reporting line is ordinary directory information to them.
	// A principal from another tenant is refused the subject outright.
	outsider := rev09102Principal(t, "manager-9", authz.RoleManager)
	stranger, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "other-corp", Subject: "comp-admin-9", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "other", Roles: []string{string(authz.RoleCompAdmin)},
		Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-comp-admin-9",
		IssuedAt: rev09102At.Add(-time.Hour), ExpiresAt: rev09102At.Add(time.Hour), CredentialDigest: "digest-comp-admin-9",
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a viewer without compensation authority gets no amount at all", func(t *testing.T) {
		got := journeyCompensationGuardrail(ctx, rev09102Bands(t), outsider, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, nil, payload)
		if got.Available() || got.Reason != promotion.GuardrailReasonNotAuthorized {
			t.Fatalf("guardrail = %+v, want UNAVAILABLE NOT_AUTHORIZED", got)
		}
		var zero promotion.CompensationGuardrail
		zero.Status, zero.Reason = got.Status, got.Reason
		if string(got.Canonical()) != string(zero.Canonical()) || got.Currency != "" || got.BandID != "" ||
			!got.CurrentAnnualized.Amount().IsZero() || !got.MaximumAnnualized.Amount().IsZero() {
			t.Fatalf("unauthorized guardrail carried data: %+v", got)
		}
		if nobody := journeyCompensationGuardrail(ctx, rev09102Bands(t), nil, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, nil, payload); nobody.Reason != promotion.GuardrailReasonNotAuthorized {
			t.Fatalf("no principal = %+v, want NOT_AUTHORIZED", nobody)
		}
	})

	t.Run("a manager without pay authority sees the reporting line but no pay", func(t *testing.T) {
		engine := rev09102Engine(t)
		review := engine.journeyPromotionReview(ctx, outsider, authz.PurposeCompensationReview,
			rev09102Instance(mustWire(t, payload)), subject, at, nil)
		if review == nil || review.Guardrail.Available() || review.Guardrail.Reason != promotion.GuardrailReasonNotAuthorized ||
			!review.Guardrail.CurrentAnnualized.Amount().IsZero() {
			t.Fatalf("review = %+v, want the guardrail withheld as NOT_AUTHORIZED", review)
		}
	})

	t.Run("a viewer who may not see the reporting line is not told the manager", func(t *testing.T) {
		engine := rev09102Engine(t)
		impact, name := engine.journeyManagementImpact(ctx, stranger, authz.PurposeCompensationReview,
			demoworkforce.CompanyKey, subject, at, payload)
		if impact.Evaluated() || name != "" {
			t.Fatalf("impact = %+v name = %q, want nothing disclosed", impact, name)
		}
		review := engine.journeyPromotionReview(ctx, stranger, authz.PurposeCompensationReview,
			rev09102Instance(mustWire(t, payload)), subject, at, nil)
		if review == nil || review.Guardrail.Available() || review.ManagerDisclosed() || review.TargetManagerName != "" {
			t.Fatalf("review = %+v, want a withheld guardrail and no manager", review)
		}
	})

	t.Run("a withheld hop is a well-formed denial, not an error", func(t *testing.T) {
		decision := journeyManagerHopAuthorizer(stranger, authz.PurposeCompensationReview, at)(org.ManagerRelationshipFact{Worker: subject})
		if err := decision.Validate(); err != nil || decision.SubjectDisclosable {
			t.Fatalf("decision = %+v (%v), want a valid non-disclosable decision", decision, err)
		}
		if err := decision.Covers([]people.FieldID{people.FieldManagerRelation}); err != nil {
			t.Fatalf("withheld decision is silent about the manager relation: %v", err)
		}
	})
}

func rev09102Instance(wire []byte) *intentsv1.IntentInstance {
	return &intentsv1.IntentInstance{
		TenantId: demoworkforce.CompanyKey,
		Request:  &intentsv1.TypedPayload{ProtobufWireBytes: wire},
	}
}

func mustWire(t *testing.T, payload *structValue) []byte {
	t.Helper()
	wire, err := protomap.MarshalDeterministic(payload)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
