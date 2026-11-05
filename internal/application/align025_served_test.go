package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/draftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var draftServedT0 = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func draftServedDef(t *testing.T) intent.Definition {
	t.Helper()
	reg, err := definitions.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	def, err := reg.ResolveText("hcmnext.people.promote_worker/v1")
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func newDraftServed(t *testing.T, def intent.Definition, tenant, principal, id string, inputs ...intent.InputValue) intent.AuthoringDraft {
	t.Helper()
	d, err := intent.NewAuthoringDraft(intent.DraftSpec{Tenant: values.TenantId(tenant), Definition: def.Ref, Author: intent.PrincipalReference{PrincipalID: principal, Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance.mfa_session/v1"}, Inputs: inputs},
		def, nil, func() (string, error) { return id, nil }, func() values.Instant { return values.NewInstant(draftServedT0) })
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestTodo_ALIGN_025_Served proves the durable draft persistence semantics
// on the served path. The A-F reachability audit (2026-09-24) reopened
// ALIGN-025 because internal/intent/draftstore was absent from the
// shipped-command dependency closure; the served alignment surface must
// therefore expose the draft Port so create-at-zero, compare-and-set,
// owner isolation and submission semantics are proven through the same
// surface shipped commands compose.
func TestTodo_ALIGN_025_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	if surface.NewMemoryDraftStore == nil {
		t.Fatal("served alignment surface omitted draft contracts")
	}
	ctx := context.Background()
	def := draftServedDef(t)
	ana := draftstore.Owner{Tenant: values.TenantId("acme"), PrincipalID: "principal:ana"}
	foreign := draftstore.Owner{Tenant: values.TenantId("acme"), PrincipalID: "principal:bob"}

	port := surface.NewMemoryDraftStore(draftstore.DefaultTTL)
	d := newDraftServed(t, def, "acme", "principal:ana", "draft-served-1", intent.InputValue{Path: "employment_ref", CanonicalText: "employment:1"})
	if _, err := port.Save(ctx, ana, d, def, 1, draftServedT0); !errors.Is(err, draftstore.ErrConflict) {
		t.Fatalf("served create at revision 1 = %v, want ErrConflict", err)
	}
	r1, err := port.Save(ctx, ana, d, def, 0, draftServedT0)
	if err != nil || r1.Revision != 1 {
		t.Fatalf("served create = %+v, %v", r1, err)
	}
	if _, err := port.Save(ctx, ana, d, def, 0, draftServedT0); !errors.Is(err, draftstore.ErrConflict) {
		t.Fatalf("served stale writer = %v, want ErrConflict", err)
	}
	got, err := port.Resume(ctx, ana, "draft-served-1", def, draftServedT0.Add(time.Minute))
	if err != nil || got.Outcome != draftstore.OutcomeCurrent {
		t.Fatalf("served resume = %+v, %v", got, err)
	}
	// Another principal sees the draft as not found, never denied.
	if _, err := port.Resume(ctx, foreign, "draft-served-1", def, draftServedT0.Add(time.Minute)); !errors.Is(err, draftstore.ErrNotFound) {
		t.Fatalf("served foreign resume = %v, want ErrNotFound", err)
	}
	stamped, err := port.MarkSubmitted(ctx, ana, "draft-served-1", r1.Revision, "intent:served-1", draftServedT0.Add(2*time.Minute))
	if err != nil || stamped.Draft.SubmittedIntentID != "intent:served-1" {
		t.Fatalf("served submit = %+v, %v", stamped, err)
	}
	if _, err := port.MarkSubmitted(ctx, ana, "draft-served-1", r1.Revision, "intent:served-2", draftServedT0.Add(3*time.Minute)); !errors.Is(err, draftstore.ErrConflict) && !errors.Is(err, draftstore.ErrAlreadySubmitted) {
		t.Fatalf("served second submit = %v, want conflict", err)
	}
}
