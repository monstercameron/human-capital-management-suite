package draftstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/draftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func digester(t *testing.T) intent.Digester {
	t.Helper()
	d, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func proposalSpec(t *testing.T, tenant, intentID string, revision uint64, position string) intent.ProposalSpec {
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
		CreatedBy: author("principal:ana"),
	}
}

func revision(t *testing.T, def intent.Definition, spec intent.ProposalSpec, id string) intent.ProposalRevision {
	t.Helper()
	rev, err := intent.NewProposalRevision(spec, def, digester(t), func() (string, error) { return id, nil }, func() values.Instant { return values.NewInstant(t0) })
	if err != nil {
		t.Fatalf("proposal revision: %v", err)
	}
	return rev
}

// submitted stores a draft for tenant acme and stamps it with intentID.
func submitted(t *testing.T, p draftstore.Port, def intent.Definition, id, intentID string) draftstore.Record {
	t.Helper()
	ctx := context.Background()
	ana := owner("acme", "principal:ana")
	rec, err := p.Save(ctx, ana, newDraft(t, def, "acme", "principal:ana", id, in("employment_ref", "employment:9001")), def, 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	rec, err = p.MarkSubmitted(ctx, ana, id, rec.Revision, intentID, t0)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// TestTodo_ALIGN_026 proves a submitted draft binds to exactly the immutable
// proposal revision it produced, and that the binding stops being current the
// moment a superseding revision exists.
func TestTodo_ALIGN_026(t *testing.T) {
	def := promoteDef(t)
	d := digester(t)
	p := draftstore.NewMemory(0)
	rec := submitted(t, p, def, "draft-b", "intent:b")
	rev1 := revision(t, def, proposalSpec(t, "acme", "intent:b", 1, "position:staff"), "rev-1")
	b, err := draftstore.Bind(rec, rev1, d)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if b.DraftRevision != rec.Revision || b.InputDigest != rec.InputDigest || b.MaterialDigest != rev1.MaterialDigest.Digest || b.ProposalRevisionID != "rev-1" {
		t.Fatalf("binding = %+v", b)
	}
	if s := b.Check(rec, rev1, d); s != draftstore.BindingCurrent {
		t.Fatalf("fresh binding = %s", s)
	}
	superseding := proposalSpec(t, "acme", "intent:b", 2, "position:principal")
	prior := "rev-1"
	superseding.SupersedesRevisionID = &prior
	rev2 := revision(t, def, superseding, "rev-2")
	if s := b.Check(rec, rev2, d); s != draftstore.BindingSuperseded {
		t.Fatalf("after a superseding revision = %s", s)
	}
	b2, err := draftstore.Bind(rec, rev2, d)
	if err != nil || b2.Check(rec, rev2, d) != draftstore.BindingCurrent || b2.MaterialDigest == b.MaterialDigest {
		t.Fatalf("rebind = %+v, %v", b2, err)
	}
}

// TestTodo_ALIGN_026_Property proves the binding is a function of the pinned
// facts: binding the same record and revision twice is identical, and any
// change to the proposed state changes the material digest it pins.
func TestTodo_ALIGN_026_Property(t *testing.T) {
	def := promoteDef(t)
	d := digester(t)
	p := draftstore.NewMemory(0)
	rec := submitted(t, p, def, "draft-p", "intent:p")
	seen := map[string]string{}
	for _, pos := range []string{"position:staff", "position:principal", "position:distinguished"} {
		rev := revision(t, def, proposalSpec(t, "acme", "intent:p", 1, pos), "rev-"+pos)
		a, err := draftstore.Bind(rec, rev, d)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := draftstore.Bind(rec, rev, d)
		if a != again {
			t.Fatalf("binding is not deterministic: %+v vs %+v", a, again)
		}
		if other, dup := seen[a.MaterialDigest]; dup {
			t.Fatalf("%s and %s pin the same material digest", pos, other)
		}
		seen[a.MaterialDigest] = pos
	}
}

// TestTodo_ALIGN_026_Golden pins the binding encoding.
func TestTodo_ALIGN_026_Golden(t *testing.T) {
	b := draftstore.ProposalBinding{DraftID: "draft-g", DraftRevision: 2, InputDigest: "sha256:in", IntentID: "intent:g",
		ProposalRevisionID: "rev-g", ProposalRevision: 1, MaterialDigest: "abc", Tenant: "acme"}
	got, _ := json.Marshal(b)
	want := `{"draft_id":"draft-g","draft_revision":2,"input_digest":"sha256:in","intent_id":"intent:g","proposal_revision_id":"rev-g","proposal_revision":1,"material_digest":"abc","tenant":"acme"}`
	if string(got) != want {
		t.Fatalf("binding bytes = %s", got)
	}
}

// TestTodo_ALIGN_026_Security refuses bindings across tenants or intents, to
// unsubmitted drafts, and to revisions whose content was altered after the
// kernel minted their digest -- at bind time and at check time.
func TestTodo_ALIGN_026_Security(t *testing.T) {
	def := promoteDef(t)
	d := digester(t)
	p := draftstore.NewMemory(0)
	rec := submitted(t, p, def, "draft-s", "intent:s")
	rev := revision(t, def, proposalSpec(t, "acme", "intent:s", 1, "position:staff"), "rev-s")

	unsubmitted, err := p.Save(context.Background(), owner("acme", "principal:ana"), newDraft(t, def, "acme", "principal:ana", "draft-u"), def, 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	tampered := rev
	tampered.ProposedState = append([]intent.StateAssertion(nil), rev.ProposedState...)
	tampered.ProposedState[0].CanonicalText = "position:ceo"
	foreign := revision(t, def, proposalSpec(t, "globex", "intent:s", 1, "position:staff"), "rev-f")
	otherIntent := revision(t, def, proposalSpec(t, "acme", "intent:other", 1, "position:staff"), "rev-o")

	for name, tc := range map[string]struct {
		rec draftstore.Record
		rev intent.ProposalRevision
		d   intent.Digester
	}{
		"unsubmitted draft": {unsubmitted, rev, d},
		"unstored draft":    {draftstore.Record{Draft: rec.Draft}, rev, d},
		"other intent":      {rec, otherIntent, d},
		"other tenant":      {rec, foreign, d},
		"tampered revision": {rec, tampered, d},
		"unminted revision": {rec, intent.ProposalRevision{IntentID: "intent:s", Tenant: "acme"}, d},
		"no digester":       {rec, rev, nil},
	} {
		if _, err := draftstore.Bind(tc.rec, tc.rev, tc.d); !errors.Is(err, draftstore.ErrBinding) {
			t.Errorf("%s: bind = %v", name, err)
		}
	}
	b, err := draftstore.Bind(rec, rev, d)
	if err != nil {
		t.Fatal(err)
	}
	if s := b.Check(rec, tampered, d); s != draftstore.BindingTampered {
		t.Errorf("tampered content at check = %s", s)
	}
	swapped := rev
	swapped.MaterialDigest.Digest = strings.Repeat("0", 64)
	if s := b.Check(rec, swapped, d); s != draftstore.BindingTampered {
		t.Errorf("swapped digest at check = %s", s)
	}
	older := rev
	older.ProposalRevisionID, older.Revision = "rev-older", 0
	if s := b.Check(rec, older, d); s != draftstore.BindingTampered {
		t.Errorf("a different revision at the same or lower number = %s", s)
	}
	changed := rec
	changed.Revision++
	if s := b.Check(changed, rev, d); s != draftstore.BindingDraftChanged {
		t.Errorf("changed draft = %s", s)
	}
	if s := b.Check(rec, rev, nil); s != draftstore.BindingTampered {
		t.Errorf("check without digester = %s", s)
	}
}

// TestTodo_ALIGN_026_Conformance pins the closed status vocabulary.
func TestTodo_ALIGN_026_Conformance(t *testing.T) {
	statuses := []draftstore.BindingStatus{draftstore.BindingCurrent, draftstore.BindingDraftChanged, draftstore.BindingSuperseded, draftstore.BindingTampered}
	seen := map[draftstore.BindingStatus]bool{}
	for _, s := range statuses {
		if s == "" || seen[s] {
			t.Fatalf("status vocabulary is not closed and distinct: %v", statuses)
		}
		seen[s] = true
	}
}
