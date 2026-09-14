package productquery_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const refetchSubjectA = "00000000-0000-4000-8000-0000000000a1"
const refetchSubjectB = "00000000-0000-4000-8000-0000000000b2"

// refetchEnvelope projects the two authorized workers at a given source
// position and salary value.
func refetchEnvelope(t *testing.T, watermark, sequence uint64, salary string) productquery.Envelope {
	t.Helper()
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	a, b := allowedCandidate(refetchSubjectA), allowedCandidate(refetchSubjectB)
	a.Fields[authz.FieldBaseSalary] = productquery.Cell{State: productquery.ValuePresent, Value: salary}
	r := request(p, []productquery.Candidate{a, b})
	r.Projection.Watermark, r.Projection.SourceSequence = watermark, sequence
	e, err := productquery.Project(r)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func invalidation(t *testing.T, sequence, watermark uint64, ids ...string) productquery.InvalidationMessage {
	t.Helper()
	p := principal(t, []string{string(authz.RoleCompAdmin)}, []string{authz.PurposeCompensationReview})
	req := productquery.InvalidationRequest{Principal: p, Purpose: authz.PurposeCompensationReview, EffectiveAt: testEffective,
		Fields: []authz.FieldID{authz.FieldWorkerNumber}, Projection: "worker_summary", SourceSequence: sequence, Watermark: watermark}
	for i, id := range ids {
		s := subject("acme", id)
		req.Targets = append(req.Targets, productquery.InvalidationTarget{Subject: s, Revision: sequence + uint64(i), Decision: authorizedDecision(t, p, s)})
	}
	m, ok, err := productquery.EmitInvalidation(req)
	if err != nil || !ok {
		t.Fatalf("emit invalidation: ok=%v err=%v", ok, err)
	}
	return m
}

func salaryOf(e productquery.Envelope, id string) string {
	for _, row := range e.Rows {
		if row.Subject.Id == id {
			for _, f := range row.Fields {
				if f.ID == authz.FieldBaseSalary {
					return f.Value
				}
			}
		}
	}
	return ""
}

// TestTodo_ALIGN_024 proves an invalidation forces an authoritative refetch:
// the hint marks the view REFETCH_REQUIRED without touching its values, a
// refetch that has not applied the announced source position is refused, and
// only a caught-up projection makes the view current again.
func TestTodo_ALIGN_024(t *testing.T) {
	view, err := productquery.NewView(refetchEnvelope(t, 10, 12, "125000.00"))
	if err != nil {
		t.Fatal(err)
	}
	if view.State() != productquery.DisplayCurrent || view.Plan().Required() {
		t.Fatalf("fresh view = %s %+v", view.State(), view.Plan())
	}
	plan, err := view.Apply(invalidation(t, 15, 12, refetchSubjectA))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Required() || plan.MinWatermark != 15 || plan.Projection != "worker_summary" || len(plan.Subjects) != 1 || !strings.HasSuffix(plan.Subjects[0], refetchSubjectA) {
		t.Fatalf("refetch plan = %+v", plan)
	}
	held, state := view.Envelope()
	if state != productquery.DisplayRefetchRequired || salaryOf(held, refetchSubjectA) != "125000.00" {
		t.Fatalf("after the hint the view is %s with salary %q; want REFETCH_REQUIRED and the old value untouched", state, salaryOf(held, refetchSubjectA))
	}
	if err := view.Accept(refetchEnvelope(t, 14, 15, "130000.00")); !errors.Is(err, productquery.ErrRefetchStale) {
		t.Fatalf("lagging refetch = %v, want ErrRefetchStale", err)
	}
	if view.State() != productquery.DisplayRefetchRequired {
		t.Fatal("a refused refetch cleared the obligation")
	}
	if err := view.Accept(refetchEnvelope(t, 15, 15, "130000.00")); err != nil {
		t.Fatalf("caught-up refetch: %v", err)
	}
	current, state := view.Envelope()
	if state != productquery.DisplayCurrent || salaryOf(current, refetchSubjectA) != "130000.00" || view.Plan().Required() {
		t.Fatalf("after refetch: %s salary %q plan %+v", state, salaryOf(current, refetchSubjectA), view.Plan())
	}
}

// TestTodo_ALIGN_024_Property proves, over interleavings of hints and
// refetches, that the view is never CURRENT while any accepted hint is newer
// than its envelope's watermark, and that obligations only grow until an
// accepted refetch reaches them.
func TestTodo_ALIGN_024_Property(t *testing.T) {
	type step struct {
		hint      uint64 // 0 means a refetch at watermark
		watermark uint64
	}
	scripts := [][]step{
		{{hint: 13}, {hint: 11}, {watermark: 12}, {watermark: 13}},
		{{hint: 20}, {hint: 18}, {watermark: 19}, {hint: 21}, {watermark: 20}, {watermark: 21}},
		{{watermark: 12}, {hint: 12}, {hint: 14}, {watermark: 14}},
	}
	for i, script := range scripts {
		view, err := productquery.NewView(refetchEnvelope(t, 10, 12, "1"))
		if err != nil {
			t.Fatal(err)
		}
		highest, have := uint64(10), uint64(10)
		for _, s := range script {
			if s.hint > 0 {
				if _, err := view.Apply(invalidation(t, s.hint, 10, refetchSubjectB)); err != nil {
					t.Fatal(err)
				}
				if s.hint > have && s.hint > highest {
					highest = s.hint
				}
			} else {
				err := view.Accept(refetchEnvelope(t, s.watermark, s.watermark, "1"))
				if s.watermark >= highest {
					if err != nil {
						t.Fatalf("script %d: refetch at %d refused with obligation %d: %v", i, s.watermark, highest, err)
					}
					have, highest = s.watermark, s.watermark
				} else if !errors.Is(err, productquery.ErrRefetchStale) {
					t.Fatalf("script %d: lagging refetch at %d (need %d) = %v", i, s.watermark, highest, err)
				}
			}
			pending := highest > have
			if pending != (view.State() == productquery.DisplayRefetchRequired) {
				t.Fatalf("script %d: state %s but pending=%v (have %d need %d)", i, view.State(), pending, have, highest)
			}
			if pending && view.Plan().MinWatermark != highest {
				t.Fatalf("script %d: plan watermark %d, want %d", i, view.Plan().MinWatermark, highest)
			}
		}
	}
}

// TestTodo_ALIGN_024_Golden pins the refetch plan encoding.
func TestTodo_ALIGN_024_Golden(t *testing.T) {
	view, err := productquery.NewView(refetchEnvelope(t, 10, 12, "1"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := view.Apply(invalidation(t, 16, 12, refetchSubjectB, refetchSubjectA))
	if err != nil {
		t.Fatal(err)
	}
	b, err := plan.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"projection":"worker_summary","min_watermark":16,"subjects":["eref:v1:acme:worker:` + refetchSubjectA + `","eref:v1:acme:worker:` + refetchSubjectB + `"]}`
	if string(b) != want {
		t.Fatalf("plan bytes = %s\nwant        %s", b, want)
	}
	empty, _ := productquery.RefetchPlan{Projection: "worker_summary"}.CanonicalBytes()
	if string(empty) != `{"projection":"worker_summary","min_watermark":0,"subjects":[]}` {
		t.Fatalf("empty plan bytes = %s", empty)
	}
}

// TestTodo_ALIGN_024_Security proves a hint cannot inject data or reach across
// tenants or projections: another tenant's or projection's hint changes
// nothing, a refetch for another tenant or projection is refused, and no hint
// value ever appears in the retained envelope.
func TestTodo_ALIGN_024_Security(t *testing.T) {
	base := refetchEnvelope(t, 10, 12, "125000.00")
	view, err := productquery.NewView(base)
	if err != nil {
		t.Fatal(err)
	}
	foreign := invalidation(t, 30, 12, refetchSubjectA)
	foreign.Tenant = "other"
	for i := range foreign.Items {
		foreign.Items[i].Subject.Tenant = "other"
	}
	if plan, err := view.Apply(foreign); err != nil || plan.Required() {
		t.Fatalf("foreign-tenant hint = %+v, %v", plan, err)
	}
	otherProjection := invalidation(t, 30, 12, refetchSubjectA)
	otherProjection.Projection = "payroll_summary"
	if plan, err := view.Apply(otherProjection); err != nil || plan.Required() {
		t.Fatalf("other-projection hint = %+v, %v", plan, err)
	}
	if before, _ := view.Envelope(); before.Digest() != base.Digest() {
		t.Fatal("a hint mutated the retained envelope")
	}
	if _, err := view.Apply(invalidation(t, 30, 12, refetchSubjectA)); err != nil {
		t.Fatal(err)
	}
	wrongTenant := refetchEnvelope(t, 30, 30, "1")
	wrongTenant.Tenant = "other"
	for i := range wrongTenant.Rows {
		wrongTenant.Rows[i].Subject.Tenant = "other"
	}
	if err := view.Accept(wrongTenant); !errors.Is(err, productquery.ErrRefetchMismatch) {
		t.Fatalf("foreign refetch = %v", err)
	}
	wrongProjection := refetchEnvelope(t, 30, 30, "1")
	wrongProjection.Projection.Name = "payroll_summary"
	if err := view.Accept(wrongProjection); !errors.Is(err, productquery.ErrRefetchMismatch) {
		t.Fatalf("other-projection refetch = %v", err)
	}
	if view.State() != productquery.DisplayRefetchRequired {
		t.Fatal("a refused foreign refetch cleared the obligation")
	}
}

// TestTodo_ALIGN_024_Integration runs the whole loop: a source change emits
// an authorized invalidation, the surface applies it, refetches through
// Project and accepts the caught-up envelope.
func TestTodo_ALIGN_024_Integration(t *testing.T) {
	view, err := productquery.NewView(refetchEnvelope(t, 10, 12, "125000.00"))
	if err != nil {
		t.Fatal(err)
	}
	hint := invalidation(t, 18, 12, refetchSubjectA, refetchSubjectB)
	if err := hint.Validate(); err != nil {
		t.Fatal(err)
	}
	plan, err := view.Apply(hint)
	if err != nil || len(plan.Subjects) != 2 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	refetched := refetchEnvelope(t, plan.MinWatermark, plan.MinWatermark, "140000.00")
	if err := view.Accept(refetched); err != nil {
		t.Fatal(err)
	}
	got, state := view.Envelope()
	if state != productquery.DisplayCurrent || got.Digest() != refetched.Digest() {
		t.Fatalf("integrated view = %s digest %s want %s", state, got.Digest(), refetched.Digest())
	}
}

// TestTodo_ALIGN_024_Fault covers malformed hints and envelopes and replayed
// or out-of-order hints.
func TestTodo_ALIGN_024_Fault(t *testing.T) {
	if _, err := productquery.NewView(productquery.Envelope{}); err == nil {
		t.Fatal("an invalid envelope was retained")
	}
	view, err := productquery.NewView(refetchEnvelope(t, 10, 12, "1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := view.Apply(productquery.InvalidationMessage{}); err == nil {
		t.Fatal("a malformed hint was applied")
	}
	// A hint the view has already reflected is a no-op.
	if plan, err := view.Apply(invalidation(t, 10, 10, refetchSubjectA)); err != nil || plan.Required() {
		t.Fatalf("already-reflected hint = %+v, %v", plan, err)
	}
	// Out-of-order hints never lower the obligation.
	if _, err := view.Apply(invalidation(t, 20, 12, refetchSubjectA)); err != nil {
		t.Fatal(err)
	}
	if plan, _ := view.Apply(invalidation(t, 14, 12, refetchSubjectA)); plan.MinWatermark != 20 {
		t.Fatalf("older hint lowered the obligation to %d", plan.MinWatermark)
	}
	if err := view.Accept(productquery.Envelope{}); err == nil {
		t.Fatal("a malformed refetch was accepted")
	}
}

// TestTodo_ALIGN_024_Conformance pins that the refetch contract carries no
// value-bearing field: a plan names only a projection, a watermark and
// subject references.
func TestTodo_ALIGN_024_Conformance(t *testing.T) {
	typ := reflect.TypeOf(productquery.RefetchPlan{})
	allowed := map[string]bool{"Projection": true, "MinWatermark": true, "Subjects": true}
	for i := range typ.NumField() {
		if !allowed[typ.Field(i).Name] {
			t.Errorf("RefetchPlan carries %s; a refetch obligation must not carry data", typ.Field(i).Name)
		}
	}
	for _, s := range []productquery.DisplayState{productquery.DisplayCurrent, productquery.DisplayRefetchRequired} {
		if s == "" {
			t.Error("empty display state")
		}
	}
}
