package draftstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/draftstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var t0 = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func promoteDef(t *testing.T) intent.Definition {
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

func author(id string) intent.PrincipalReference {
	return intent.PrincipalReference{PrincipalID: id, Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance.mfa_session/v1"}
}

func newDraft(t *testing.T, def intent.Definition, tenant, principal, id string, inputs ...intent.InputValue) intent.AuthoringDraft {
	t.Helper()
	d, err := intent.NewAuthoringDraft(intent.DraftSpec{Tenant: values.TenantId(tenant), Definition: def.Ref, Author: author(principal), Inputs: inputs},
		def, nil, func() (string, error) { return id, nil }, func() values.Instant { return values.NewInstant(t0) })
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func owner(tenant, principal string) draftstore.Owner {
	return draftstore.Owner{Tenant: values.TenantId(tenant), PrincipalID: principal}
}

func in(path, text string) intent.InputValue {
	return intent.InputValue{Path: path, CanonicalText: text}
}

// conformance is the semantics every durable draft store must keep; it runs
// against the reference store here and against any adapter that adopts it.
func conformance(t *testing.T, newPort func() draftstore.Port) {
	ctx := context.Background()
	def := promoteDef(t)
	ana := owner("acme", "principal:ana")

	t.Run("create expects revision zero and every save is a compare-and-set", func(t *testing.T) {
		p := newPort()
		d := newDraft(t, def, "acme", "principal:ana", "draft-1", in("employment_ref", "employment:1"))
		if _, err := p.Save(ctx, ana, d, def, 1, t0); !errors.Is(err, draftstore.ErrConflict) {
			t.Fatalf("create at revision 1 = %v", err)
		}
		r1, err := p.Save(ctx, ana, d, def, 0, t0)
		if err != nil || r1.Revision != 1 {
			t.Fatalf("create = %+v, %v", r1, err)
		}
		if _, err := p.Save(ctx, ana, d, def, 0, t0); !errors.Is(err, draftstore.ErrConflict) {
			t.Fatalf("second create = %v", err)
		}
		edited, _ := d.WithInput(in("target_position_ref", "position:staff"), def, func() values.Instant { return values.NewInstant(t0) })
		r2, err := p.Save(ctx, ana, edited, def, 1, t0.Add(time.Minute))
		if err != nil || r2.Revision != 2 || r2.InputDigest == r1.InputDigest {
			t.Fatalf("edit = %+v, %v", r2, err)
		}
		if _, err := p.Save(ctx, ana, d, def, 1, t0.Add(2*time.Minute)); !errors.Is(err, draftstore.ErrConflict) {
			t.Fatalf("stale writer = %v", err)
		}
		got, err := p.Resume(ctx, ana, "draft-1", def, t0.Add(3*time.Minute))
		if err != nil || got.Outcome != draftstore.OutcomeCurrent || len(got.Record.Draft.Inputs) != 2 {
			t.Fatalf("resume = %+v, %v", got, err)
		}
	})

	t.Run("expiry withholds inputs", func(t *testing.T) {
		p := newPort()
		d := newDraft(t, def, "acme", "principal:ana", "draft-exp", in("employment_ref", "employment:1"))
		rec, err := p.Save(ctx, ana, d, def, 0, t0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.Resume(ctx, ana, "draft-exp", def, rec.ExpiresAt)
		if err != nil || got.Outcome != draftstore.OutcomeExpired || len(got.Record.Draft.Inputs) != 0 {
			t.Fatalf("expired resume = %+v, %v", got, err)
		}
		if list, _ := p.List(ctx, ana, nil, rec.ExpiresAt); len(list) != 0 {
			t.Fatalf("expired draft listed: %+v", list)
		}
		if _, err := p.MarkSubmitted(ctx, ana, "draft-exp", rec.Revision, "intent:1", rec.ExpiresAt); !errors.Is(err, draftstore.ErrNotFound) {
			t.Fatalf("submitting an expired draft = %v", err)
		}
	})

	t.Run("definition drift requires rebase", func(t *testing.T) {
		p := newPort()
		d := newDraft(t, def, "acme", "principal:ana", "draft-v1", in("employment_ref", "employment:1"))
		if _, err := p.Save(ctx, ana, d, def, 0, t0); err != nil {
			t.Fatal(err)
		}
		v2 := def
		v2.Ref.Version = def.Ref.Version + 1
		got, err := p.Resume(ctx, ana, "draft-v1", v2, t0)
		if err != nil || got.Outcome != draftstore.OutcomeRebaseRequired || got.Record.Draft.Definition != def.Ref {
			t.Fatalf("drifted resume = %+v, %v", got, err)
		}
	})

	t.Run("submission stamps once and freezes the draft", func(t *testing.T) {
		p := newPort()
		d := newDraft(t, def, "acme", "principal:ana", "draft-sub", in("employment_ref", "employment:1"))
		rec, _ := p.Save(ctx, ana, d, def, 0, t0)
		sub, err := p.MarkSubmitted(ctx, ana, "draft-sub", rec.Revision, "intent:42", t0)
		if err != nil || sub.Draft.SubmittedIntentID != "intent:42" || sub.Revision != rec.Revision+1 {
			t.Fatalf("mark submitted = %+v, %v", sub, err)
		}
		if _, err := p.MarkSubmitted(ctx, ana, "draft-sub", sub.Revision, "intent:43", t0); !errors.Is(err, draftstore.ErrAlreadySubmitted) {
			t.Fatalf("second submission = %v", err)
		}
		if _, err := p.Save(ctx, ana, d, def, sub.Revision, t0); !errors.Is(err, draftstore.ErrAlreadySubmitted) {
			t.Fatalf("editing a submitted draft = %v", err)
		}
		got, _ := p.Resume(ctx, ana, "draft-sub", def, t0)
		if got.Outcome != draftstore.OutcomeSubmitted {
			t.Fatalf("resume submitted = %+v", got)
		}
	})
}

// TestTodo_ALIGN_025 is the primary proof: the reference store keeps every
// durable draft semantic end to end.
func TestTodo_ALIGN_025(t *testing.T) {
	conformance(t, func() draftstore.Port { return draftstore.NewMemory(0) })
}

// TestTodo_ALIGN_025_Conformance runs the same semantics against a store with
// a short TTL and concurrent writers, the shape a durable adapter must survive.
func TestTodo_ALIGN_025_Conformance(t *testing.T) {
	conformance(t, func() draftstore.Port { return draftstore.NewMemory(time.Hour) })
	ctx := context.Background()
	def := promoteDef(t)
	p := draftstore.NewMemory(0)
	d := newDraft(t, def, "acme", "principal:ana", "draft-race", in("employment_ref", "employment:1"))
	var wg sync.WaitGroup
	wins := make(chan uint64, 16)
	for range 16 {
		wg.Go(func() {
			if r, err := p.Save(ctx, owner("acme", "principal:ana"), d, def, 0, t0); err == nil {
				wins <- r.Revision
			}
		})
	}
	wg.Wait()
	close(wins)
	if len(wins) != 1 {
		t.Fatalf("%d concurrent creates won, want exactly 1", len(wins))
	}
}

// TestTodo_ALIGN_025_Property proves revision monotonicity and digest
// stability over save sequences: each accepted save advances the revision by
// one, and the input digest depends only on the set of inputs.
func TestTodo_ALIGN_025_Property(t *testing.T) {
	ctx := context.Background()
	def := promoteDef(t)
	clock := func() values.Instant { return values.NewInstant(t0) }
	for n := 1; n <= 5; n++ {
		p := draftstore.NewMemory(0)
		ana := owner("acme", "principal:ana")
		d := newDraft(t, def, "acme", "principal:ana", fmt.Sprintf("draft-p%d", n))
		rev := uint64(0)
		for i := 0; i < n; i++ {
			var err error
			d, err = d.WithInput(in("employment_ref", fmt.Sprintf("employment:%d", i)), def, clock)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := p.Save(ctx, ana, d, def, rev, t0.Add(time.Duration(i)*time.Minute))
			if err != nil || rec.Revision != rev+1 {
				t.Fatalf("n=%d i=%d save = %+v, %v", n, i, rec, err)
			}
			rev = rec.Revision
		}
	}
	a := []intent.InputValue{in("employment_ref", "e"), in("target_position_ref", "p")}
	b := []intent.InputValue{in("target_position_ref", "p"), in("employment_ref", "e")}
	if draftstore.InputDigest(a) != draftstore.InputDigest(b) || draftstore.InputDigest(a) == draftstore.InputDigest(a[:1]) {
		t.Fatal("input digest is not a function of the input set")
	}
}

// TestTodo_ALIGN_025_Golden pins the safe listing projection.
func TestTodo_ALIGN_025_Golden(t *testing.T) {
	ctx := context.Background()
	def := promoteDef(t)
	p := draftstore.NewMemory(0)
	ana := owner("acme", "principal:ana")
	if _, err := p.Save(ctx, ana, newDraft(t, def, "acme", "principal:ana", "draft-g", in("employment_ref", "employment:9001")), def, 0, t0); err != nil {
		t.Fatal(err)
	}
	list, err := p.List(ctx, ana, func(r intent.Ref) (intent.Definition, bool) { return def, r == def.Ref }, t0)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(list)
	want := `[{"draft_id":"draft-g","definition":"hcmnext.people.promote_worker/v1","revision":1,"input_digest":"` +
		draftstore.InputDigest([]intent.InputValue{in("employment_ref", "employment:9001")}) +
		`","missing_inputs":` + fmt.Sprint(len(newDraft(t, def, "acme", "principal:ana", "x", in("employment_ref", "employment:9001")).MissingRequiredInputs(def))) +
		`,"expires_at":"2026-10-14T12:00:00Z"}]`
	if string(b) != want {
		t.Fatalf("listing = %s\nwant      %s", b, want)
	}
	if strings.Contains(string(b), "employment:9001") {
		t.Fatal("the safe listing leaked an input value")
	}
}

// TestTodo_ALIGN_025_Security proves ownership is enforced without an
// existence oracle and that server-owned or undeclared inputs never persist.
func TestTodo_ALIGN_025_Security(t *testing.T) {
	ctx := context.Background()
	def := promoteDef(t)
	p := draftstore.NewMemory(0)
	ana := owner("acme", "principal:ana")
	d := newDraft(t, def, "acme", "principal:ana", "draft-s", in("employment_ref", "employment:1"))
	rec, err := p.Save(ctx, ana, d, def, 0, t0)
	if err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]draftstore.Owner{
		"other principal": owner("acme", "principal:mallory"),
		"other tenant":    owner("globex", "principal:ana"),
	} {
		if _, err := p.Resume(ctx, o, "draft-s", def, t0); !errors.Is(err, draftstore.ErrNotFound) {
			t.Errorf("%s resume = %v, want not found", name, err)
		}
		if _, err := p.MarkSubmitted(ctx, o, "draft-s", rec.Revision, "intent:x", t0); !errors.Is(err, draftstore.ErrNotFound) {
			t.Errorf("%s submit = %v", name, err)
		}
		if list, _ := p.List(ctx, o, nil, t0); len(list) != 0 {
			t.Errorf("%s listed %+v", name, list)
		}
	}
	if _, err := p.Save(ctx, owner("acme", "principal:mallory"), d, def, rec.Revision, t0); !errors.Is(err, draftstore.ErrNotFound) {
		t.Errorf("saving someone else's draft = %v", err)
	}
	forged := d
	forged.Inputs = append(forged.Inputs, in("tenant_id", "globex"))
	if _, err := p.Save(ctx, ana, forged, def, rec.Revision, t0); !errors.Is(err, draftstore.ErrInvalid) {
		t.Errorf("server-owned input persisted: %v", err)
	}
	undeclared := d
	undeclared.Inputs = append(undeclared.Inputs, in("salary_override", "1"))
	if _, err := p.Save(ctx, ana, undeclared, def, rec.Revision, t0); !errors.Is(err, draftstore.ErrInvalid) {
		t.Errorf("undeclared input persisted: %v", err)
	}
	for name, call := range map[string]func() error{
		"no owner":   func() error { _, err := p.Save(ctx, draftstore.Owner{}, d, def, 0, t0); return err },
		"no instant": func() error { _, err := p.Save(ctx, ana, d, def, 0, time.Time{}); return err },
		"pre-stamped": func() error {
			s := d
			s.SubmittedIntentID = "intent:x"
			_, err := p.Save(ctx, ana, s, def, rec.Revision, t0)
			return err
		},
		"resume no owner":  func() error { _, err := p.Resume(ctx, draftstore.Owner{}, "draft-s", def, t0); return err },
		"submit no intent": func() error { _, err := p.MarkSubmitted(ctx, ana, "draft-s", rec.Revision, " ", t0); return err },
		"submit no owner":  func() error { _, err := p.MarkSubmitted(ctx, draftstore.Owner{}, "draft-s", 1, "i", t0); return err },
		"list no owner":    func() error { _, err := p.List(ctx, draftstore.Owner{}, nil, t0); return err },
		"submit stale":     func() error { _, err := p.MarkSubmitted(ctx, ana, "draft-s", rec.Revision+5, "i", t0); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
