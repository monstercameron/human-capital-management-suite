package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/draftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func draftServedSpec(t *testing.T, tenant, intentID string, revision uint64, position string) intent.ProposalSpec {
	t.Helper()
	key, err := values.NewResourceKey(values.TenantId(tenant), values.Kind("assignment"), "employment", "9001", "primary")
	if err != nil {
		t.Fatal(err)
	}
	seq, err := values.NewSequenceRevision("people.employment.9001", 42)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewOpenInstantInterval(values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	subject := intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"}
	return intent.ProposalSpec{
		IntentID: intentID, Revision: revision, Tenant: values.TenantId(tenant),
		OrganizationScopeID: "org:" + tenant + ":engineering", LegalEntityID: "legal:" + tenant,
		Subjects:      []intent.SubjectReference{subject},
		EffectiveTime: iv,
		CurrentState:  []intent.StateAssertion{{Subject: subject, ResourceKey: key, FieldPath: "assignment.position_ref", CanonicalText: "position:senior"}},
		ProposedState: []intent.StateAssertion{{Subject: subject, ResourceKey: key, FieldPath: "assignment.position_ref", CanonicalText: position}},
		Writes: []intent.PlannedWrite{{Subject: subject, ResourceKey: key, FieldPath: "assignment.position_ref",
			CurrentCanonicalText: "position:senior", ProposedCanonicalText: position,
			SourceAuthorityDecision: "authority.local_master/v1", ExpectedRevision: seq}},
		RequiredApprovals: []intent.RequiredApproval{{RequirementID: "req.promotion_manager/v1", SeparationConstraint: "not_requester"}},
		SourceBaselines:   []intent.SourceBaseline{{StreamID: "people.employment.9001", ExpectedRevision: seq}},
		Purpose:           intent.PurposeDecision{Purpose: "promotion.annual_cycle", RecipientRef: "recipient:hr-ops", DestinationRef: "destination:internal", ResidencyRef: "residency:eu"},
		Revalidation:      intent.RevalidationPlan{Rules: []string{"promotion_execution_revalidation/v1"}},
		ControlSnapshots: intent.ControlSnapshots{CapabilityRegistryDigest: "cap-1", PolicyBundleDigest: "policy-1", LegalContextDigest: "legal-1",
			EntitlementDigest: "ent-1", ReferenceDataDigest: "ref-1", ClassificationTaxonomyDigest: "tax-1", ClassificationLabelSetDigest: "labels-1", DLPDecisionDigest: "dlp-1"},
		CreatedBy: intent.PrincipalReference{PrincipalID: "principal:ana", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance.mfa_session/v1"},
	}
}

func draftServedRevision(t *testing.T, def intent.Definition, d intent.Digester, spec intent.ProposalSpec, id string) intent.ProposalRevision {
	t.Helper()
	rev, err := intent.NewProposalRevision(spec, def, d, func() (string, error) { return id, nil }, func() values.Instant { return values.NewInstant(draftServedT0) })
	if err != nil {
		t.Fatalf("proposal revision: %v", err)
	}
	return rev
}

// TestTodo_ALIGN_026_Served proves a submitted product draft binds to exactly
// the immutable proposal revision it produced through the served alignment
// surface, and that the binding stops being current once a superseding
// revision exists or the stored draft changes.
func TestTodo_ALIGN_026_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	if surface.BindDraft == nil || surface.CheckBinding == nil {
		t.Fatal("served alignment surface omitted draft binding contracts")
	}
	ctx := context.Background()
	def := draftServedDef(t)
	dg, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatal(err)
	}
	ana := draftstore.Owner{Tenant: values.TenantId("acme"), PrincipalID: "principal:ana"}
	port := surface.NewMemoryDraftStore(draftstore.DefaultTTL)

	unsubmitted := newDraftServed(t, def, "acme", "principal:ana", "draft-served-26", intent.InputValue{Path: "employment_ref", CanonicalText: "employment:9001"})
	pending, err := port.Save(ctx, ana, unsubmitted, def, 0, draftServedT0)
	if err != nil {
		t.Fatalf("served save = %v", err)
	}
	rev1 := draftServedRevision(t, def, dg, draftServedSpec(t, "acme", "intent:served-26", 1, "position:staff"), "rev-served-1")
	if _, err := surface.BindDraft(pending, rev1, dg); !errors.Is(err, draftstore.ErrBinding) {
		t.Fatalf("served bind of unsubmitted draft = %v, want ErrBinding", err)
	}

	rec, err := port.MarkSubmitted(ctx, ana, "draft-served-26", pending.Revision, "intent:served-26", draftServedT0.Add(time.Minute))
	if err != nil {
		t.Fatalf("served submit = %v", err)
	}
	b, err := surface.BindDraft(rec, rev1, dg)
	if err != nil || b.ProposalRevisionID != "rev-served-1" || b.DraftRevision != rec.Revision {
		t.Fatalf("served bind = %+v, %v", b, err)
	}
	if s := surface.CheckBinding(b, rec, rev1, dg); s != draftstore.BindingCurrent {
		t.Fatalf("served fresh binding = %s, want CURRENT", s)
	}

	foreign := draftServedRevision(t, def, dg, draftServedSpec(t, "beta", "intent:served-26", 1, "position:staff"), "rev-served-beta")
	if _, err := surface.BindDraft(rec, foreign, dg); !errors.Is(err, draftstore.ErrBinding) {
		t.Fatalf("served cross-tenant bind = %v, want ErrBinding", err)
	}

	prior := "rev-served-1"
	superseding := draftServedSpec(t, "acme", "intent:served-26", 2, "position:principal")
	superseding.SupersedesRevisionID = &prior
	rev2 := draftServedRevision(t, def, dg, superseding, "rev-served-2")
	if s := surface.CheckBinding(b, rec, rev2, dg); s != draftstore.BindingSuperseded {
		t.Fatalf("served binding after superseding revision = %s, want SUPERSEDED", s)
	}

	edited := rec
	edited.Revision++
	if s := surface.CheckBinding(b, edited, rev1, dg); s != draftstore.BindingDraftChanged {
		t.Fatalf("served binding after draft edit = %s, want DRAFT_CHANGED", s)
	}
}
