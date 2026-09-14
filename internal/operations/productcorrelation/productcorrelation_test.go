package productcorrelation_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/productcorrelation"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var t0 = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func operator(t *testing.T, tenant string, roles ...string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "operator:ana", SubjectKind: trust.SubjectKindHuman,
		Roles: roles, Purposes: []string{"operator_diagnostics"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "s", IssuedAt: t0.Add(-time.Hour), ExpiresAt: t0.Add(time.Hour), CredentialDigest: "c"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func allowlist(t *testing.T) *telemetry.Allowlist {
	t.Helper()
	defs := append(telemetry.DefaultAllowlistDefinitions(), telemetry.AttributeDefinition{
		Key: "latency_bucket", Class: telemetry.ClassOperationalPublic, Signals: []telemetry.SignalKind{telemetry.SignalMetric},
		MaxCardinality: 8, Description: "a metric-only label",
	})
	a, err := telemetry.NewAllowlist(defs...)
	if err != nil {
		t.Fatalf("allowlist: %v", err)
	}
	return a
}

func ev(surface productcorrelation.Surface, name, tenant, corr string, offset time.Duration, attrs map[string]string) productcorrelation.Event {
	return productcorrelation.Event{Surface: surface, Name: name, Tenant: values.TenantId(tenant), CorrelationID: corr,
		TraceID: "trace-" + corr, At: t0.Add(offset), Attributes: attrs}
}

// promotionEvents is one promotion crossing every surface, plus noise.
func promotionEvents() []productcorrelation.Event {
	return []productcorrelation.Event{
		ev(productcorrelation.SurfaceLedger, "ledger.append", "acme", "corr-1", 4*time.Second, map[string]string{"outcome": "SUCCESS", "latency_bucket": "fast"}),
		ev(productcorrelation.SurfaceUI, "ui.promote.submit", "acme", "corr-1", 0, map[string]string{"route": "/people/promote", "employee_email": "ana@example.com"}),
		ev(productcorrelation.SurfaceIntent, "intent.propose", "acme", "corr-1", time.Second, map[string]string{"operation": "ProposeJourney", "capability_id": "hcmnext.people.promote_worker"}),
		ev(productcorrelation.SurfaceWorkflow, "workflow.runtime.start", "acme", "corr-1", 2*time.Second, map[string]string{"workflow_node": "approve_finance", "salary": "125000.00", "status": "ok"}),
		ev(productcorrelation.SurfaceWorkflow, "workflow.runtime.advance", "acme", "corr-2", time.Second, map[string]string{"status": "contact ana@example.com"}),
		ev(productcorrelation.SurfaceUI, "ui.other", "globex", "corr-1", 0, map[string]string{"route": "/x"}),
		ev(productcorrelation.SurfaceIntent, "intent.orphan", "acme", "", 0, nil),
	}
}

// TestTodo_ALIGN_053 proves one promotion is reconstructed across UI, intent,
// workflow and ledger by its correlation id, in order, with only allowlisted
// non-payload attributes, and with drops counted by reason.
func TestTodo_ALIGN_053(t *testing.T) {
	report, err := productcorrelation.Correlate(operator(t, "acme", admin.OperatorRole), allowlist(t), promotionEvents())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Timelines) != 2 {
		t.Fatalf("timelines = %d, want corr-1 and corr-2", len(report.Timelines))
	}
	promo := report.Timelines[0]
	var names []string
	for _, s := range promo.Steps {
		names = append(names, s.Name)
	}
	if promo.CorrelationID != "corr-1" || !promo.Complete || strings.Join(names, ",") != "ui.promote.submit,intent.propose,workflow.runtime.start,ledger.append" {
		t.Fatalf("promotion timeline = %+v", promo)
	}
	for k, want := range map[string]int{
		productcorrelation.DropSignalNotAllowed: 1, productcorrelation.DropUnknownKey: 2, productcorrelation.DropPayloadShape: 1,
		productcorrelation.DropForeignTenant: 1, productcorrelation.DropUncorrelated: 1,
	} {
		if report.Dropped[k] != want {
			t.Errorf("dropped[%s] = %d, want %d (all: %v)", k, report.Dropped[k], want, report.Dropped)
		}
	}
	b, _ := json.Marshal(report)
	for _, leak := range []string{"ana@example.com", "125000.00", "globex"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("report leaks %q: %s", leak, b)
		}
	}
	if report.Timelines[1].Complete {
		t.Error("a single-surface timeline was marked complete")
	}
}

// TestTodo_ALIGN_053_Property proves the report is independent of event
// order and that sanitizing an already sanitized timeline changes nothing.
func TestTodo_ALIGN_053_Property(t *testing.T) {
	op := operator(t, "acme", admin.OperatorRole)
	allow := allowlist(t)
	events := promotionEvents()
	base, err := productcorrelation.Correlate(op, allow, events)
	if err != nil {
		t.Fatal(err)
	}
	for shift := 1; shift < len(events); shift++ {
		rotated := append(append([]productcorrelation.Event{}, events[shift:]...), events[:shift]...)
		got, _ := productcorrelation.Correlate(op, allow, rotated)
		if got.Digest() != base.Digest() {
			t.Fatalf("rotation %d changed the report", shift)
		}
	}
	var again []productcorrelation.Event
	for _, tl := range base.Timelines {
		for _, s := range tl.Steps {
			again = append(again, productcorrelation.Event{Surface: s.Surface, Name: s.Name, Tenant: "acme", CorrelationID: tl.CorrelationID, TraceID: s.TraceID, At: s.At, Attributes: s.Attributes})
		}
	}
	resanitized, _ := productcorrelation.Correlate(op, allow, again)
	if len(resanitized.Dropped) != 0 || fmt.Sprint(resanitized.Timelines) != fmt.Sprint(base.Timelines) {
		t.Fatalf("sanitization is not idempotent: dropped %v", resanitized.Dropped)
	}
}

// TestTodo_ALIGN_053_Golden pins the report encoding for one minimal action.
func TestTodo_ALIGN_053_Golden(t *testing.T) {
	report, err := productcorrelation.Correlate(operator(t, "acme", admin.OperatorRole), allowlist(t), []productcorrelation.Event{
		ev(productcorrelation.SurfaceUI, "ui.click", "acme", "corr-g", 0, map[string]string{"route": "/home"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(report)
	want := `{"tenant":"acme","timelines":[{"correlation_id":"corr-g","steps":[{"surface":"UI","name":"ui.click","at":"2026-09-14T12:00:00Z","trace_id":"trace-corr-g","attributes":{"route":"/home"}}],"surfaces":["UI"],"complete":false}],"dropped":{}}`
	if string(b) != want {
		t.Fatalf("report = %s\nwant     %s", b, want)
	}
}

// TestTodo_ALIGN_053_Security refuses non-operators and never discloses
// another tenant's events or protected values.
func TestTodo_ALIGN_053_Security(t *testing.T) {
	if _, err := productcorrelation.Correlate(operator(t, "acme"), allowlist(t), promotionEvents()); !errors.Is(err, productcorrelation.ErrUnauthorized) {
		t.Fatalf("non-operator = %v", err)
	}
	report, err := productcorrelation.Correlate(operator(t, "globex", admin.OperatorRole), allowlist(t), promotionEvents())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Timelines) != 1 || report.Timelines[0].Steps[0].Name != "ui.other" {
		t.Fatalf("globex operator sees %+v", report.Timelines)
	}
	for _, v := range []string{"ssn 123-45-6789", strings.Repeat("x", productcorrelation.MaxValueLen+1), "line\nbreak", "bob@corp.example"} {
		r, _ := productcorrelation.Correlate(operator(t, "acme", admin.OperatorRole), allowlist(t), []productcorrelation.Event{
			ev(productcorrelation.SurfaceIntent, "intent.x", "acme", "c", 0, map[string]string{"status": v}),
		})
		if len(r.Timelines[0].Steps[0].Attributes) != 0 || r.Dropped[productcorrelation.DropPayloadShape] != 1 {
			t.Errorf("payload-shaped value %q survived: %+v", v, r)
		}
	}
}

// TestTodo_ALIGN_053_Integration correlates events carrying the attribute
// keys the workflow engine's own telemetry emits, through the platform's
// default allowlist, and proves operational identifiers survive while
// nothing else does.
func TestTodo_ALIGN_053_Integration(t *testing.T) {
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	report, err := productcorrelation.Correlate(operator(t, "acme", admin.OperatorRole), allow, []productcorrelation.Event{
		ev(productcorrelation.SurfaceWorkflow, "workflow.lease.acquire", "acme", "corr-w", 0, map[string]string{"node_id": "approve_finance", "outcome": "SUCCESS", "tenant_id": "acme"}),
		ev(productcorrelation.SurfaceWorkflow, "workflow.timer.fire", "acme", "corr-w", time.Second, map[string]string{"timer_id": "t-1", "terminal_code": "OK"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range report.Timelines[0].Steps {
		for k := range s.Attributes {
			if def, ok := allow.Lookup(k); !ok || def.Class == telemetry.ClassProhibited {
				t.Errorf("retained non-allowlisted key %s", k)
			}
		}
	}
	if report.Timelines[0].Steps[0].Attributes["node_id"] != "approve_finance" || report.Timelines[0].Steps[1].Attributes["timer_id"] != "t-1" {
		t.Fatalf("operational ids lost: %+v", report.Timelines[0].Steps)
	}
}

// TestTodo_ALIGN_053_Fault rejects malformed events and wiring.
func TestTodo_ALIGN_053_Fault(t *testing.T) {
	op := operator(t, "acme", admin.OperatorRole)
	if _, err := productcorrelation.Correlate(op, nil, nil); !errors.Is(err, productcorrelation.ErrInvalid) {
		t.Fatalf("nil allowlist = %v", err)
	}
	for name, e := range map[string]productcorrelation.Event{
		"surface": {Surface: "EMAIL", Name: "x", Tenant: "acme", At: t0},
		"name":    {Surface: productcorrelation.SurfaceUI, Tenant: "acme", At: t0},
		"instant": {Surface: productcorrelation.SurfaceUI, Name: "x", Tenant: "acme"},
	} {
		if _, err := productcorrelation.Correlate(op, allowlist(t), []productcorrelation.Event{e}); !errors.Is(err, productcorrelation.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	empty, err := productcorrelation.Correlate(op, allowlist(t), nil)
	if err != nil || len(empty.Timelines) != 0 {
		t.Fatalf("no events = %+v, %v", empty, err)
	}
}

// TestTodo_ALIGN_053_Conformance pins the closed surface vocabulary and that
// same-instant steps order by surface.
func TestTodo_ALIGN_053_Conformance(t *testing.T) {
	if fmt.Sprint(productcorrelation.Surfaces()) != "[UI INTENT WORKFLOW LEDGER]" {
		t.Fatalf("surfaces = %v", productcorrelation.Surfaces())
	}
	report, _ := productcorrelation.Correlate(operator(t, "acme", admin.OperatorRole), allowlist(t), []productcorrelation.Event{
		ev(productcorrelation.SurfaceLedger, "b", "acme", "c", 0, nil), ev(productcorrelation.SurfaceUI, "a", "acme", "c", 0, nil),
	})
	if report.Timelines[0].Steps[0].Surface != productcorrelation.SurfaceUI {
		t.Fatalf("same-instant order = %+v", report.Timelines[0].Steps)
	}
}
