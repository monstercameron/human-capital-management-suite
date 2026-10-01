package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	capauthority "github.com/monstercameron/human-capital-management-suite/internal/capability/authority"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type productionClockWorkers struct{}

func (productionClockWorkers) ResolveWorker(context.Context, string, string) (string, bool, error) {
	return "worker-1", true, nil
}
func (productionClockWorkers) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	return "project-1", "site-1", true, nil
}

type productionClockEvaluator struct {
	allow bool
	calls []string
}

func (e *productionClockEvaluator) ResolveClockAuthority(_ context.Context, _ *trust.Principal, _, capability, subject, assignment string, _ time.Time) (capauthority.Authority, error) {
	e.calls = append(e.calls, capability+":"+subject+":"+assignment)
	if !e.allow {
		return capauthority.Authority{EffectiveRoles: []string{}}, nil
	}
	return capauthority.Authority{}, nil
}

func productionRegistry(t *testing.T, ids ClockCapabilityIDs) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	for _, id := range []string{ids.Punch, ids.DeviceAdmin, ids.SupervisorOverride, ids.MissingPunchRequest, ids.MissingPunchDecision} {
		def := capability.Definition{ID: id, Version: 1, OwnerDomain: "time", RequestSchema: capability.SchemaRef{SchemaID: id + ".request", Version: 1, ProtobufFullName: "x"}, ResponseSchema: capability.SchemaRef{SchemaID: id + ".response", Version: 1, ProtobufFullName: "x"}, ErrorSchema: capability.SchemaRef{SchemaID: id + ".error", Version: 1, ProtobufFullName: "x"}, EffectClass: capability.EffectReadOnly, RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:time.read", LegalBasisRef: "legal.v1", EntitlementRef: "entitlement.v1", SLOClassRef: "slo.v1", TestRef: "test/v1"}
		if err := r.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func productionAuthority(t *testing.T, evaluator *productionClockEvaluator) ProductionClockAuthority {
	t.Helper()
	ids := ClockCapabilityIDs{"clock.punch", "clock.device", "clock.supervisor", "clock.missing.request", "clock.missing.decision"}
	a, err := NewProductionClockAuthority(ProductionClockDependencies{Workers: productionClockWorkers{}, Registry: productionRegistry(t, ids), Facts: evaluator, Capabilities: ids, Clock: func() time.Time { return time.Unix(200, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type productionOriginal struct{}

func (productionOriginal) ResolveOriginalClockIn(context.Context, string, string) (string, error) {
	return "observation-1", nil
}

type productionSessions struct{}

func (productionSessions) GetMissingPunchSession(context.Context, string, string) (clockservice.MissingPunchSession, error) {
	return clockservice.MissingPunchSession{TenantID: "tenant-1", ID: "session-1", WorkerRef: "worker-1", Status: "OPEN", Revision: 3, MissingOut: true}, nil
}

type productionObservations struct{}

func (productionObservations) GetMissingPunchObservation(context.Context, string, string) (clockservice.MissingPunchObservation, error) {
	return clockservice.MissingPunchObservation{ID: "observation-1", TenantID: "tenant-1", Observation: clockdomain.TimeObservation{WorkerRef: "worker-1", EventType: clockdomain.EventClockIn, Accepted: true, OccurredAt: time.Unix(10, 0)}}, nil
}

type productionReviews struct{}

func (productionReviews) GetMissingPunchRequest(context.Context, string, string) (clockservice.MissingPunchRecord, error) {
	return clockservice.MissingPunchRecord{ID: "request-1", TenantID: "tenant-1", WorkerRef: "worker-1"}, nil
}

func productionPrincipal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-1"), Subject: subject, SubjectKind: trust.SubjectKindHuman, Purposes: []string{"timekeeping"}, IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(300, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProductionClockAuthority_UsesCanonicalScopeAndDeniesUnallowed(t *testing.T) {
	evaluator := &productionClockEvaluator{allow: true}
	authority := productionAuthority(t, evaluator)
	delegated, err := authority.AuthorizePunch(context.Background(), productionPrincipal(t, "worker-1"), "tenant-1", "alias", "assignment-1")
	if err != nil || delegated {
		t.Fatalf("authorize punch = %v, %v", delegated, err)
	}
	if len(evaluator.calls) != 1 || evaluator.calls[0] != "hcmnext.time.clock_punch:worker-1:assignment-1" {
		t.Fatalf("evaluation call = %#v", evaluator.calls)
	}
	evaluator.allow = false
	if _, err := authority.AuthorizePunch(context.Background(), productionPrincipal(t, "worker-1"), "tenant-1", "worker-1", "assignment-1"); !errors.Is(err, errClockAuthorityDenied) {
		t.Fatalf("denied punch error = %v", err)
	}
}

func TestProductionClockAuthority_MissingPunchSeparatesSelfRequestAndReview(t *testing.T) {
	evaluator := &productionClockEvaluator{allow: true}
	authority := productionAuthority(t, evaluator)
	if err := authority.AuthorizeRequest(context.Background(), productionPrincipal(t, "worker-1"), "tenant-1", "worker-1"); err != nil {
		t.Fatal(err)
	}
	if err := authority.AuthorizeDecision(context.Background(), productionPrincipal(t, "worker-1"), "tenant-1", "worker-1"); !errors.Is(err, errClockAuthorityDenied) {
		t.Fatalf("self review error = %v", err)
	}
	if err := authority.AuthorizeDecision(context.Background(), productionPrincipal(t, "manager-1"), "tenant-1", "worker-1"); err != nil {
		t.Fatal(err)
	}
}

func TestProductionMissingPunchResolver_AuthorizesBeforeReturningOriginalClockIn(t *testing.T) {
	evaluator := &productionClockEvaluator{allow: true}
	authority := productionAuthority(t, evaluator)
	resolver := ProductionMissingPunchResolver{Sessions: productionSessions{}, Reviews: productionReviews{}, Observations: productionObservations{}, Original: productionOriginal{}, Authorization: authority}
	facts, err := resolver.ResolveMissingPunchSession(context.Background(), productionPrincipal(t, "worker-1"), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if facts.WorkerRef != "worker-1" || facts.OriginalObservationID != "observation-1" || facts.ExpectedRevision != 3 {
		t.Fatalf("facts = %+v", facts)
	}
	if _, err := resolver.ResolveMissingPunchRequest(context.Background(), productionPrincipal(t, "manager-1"), "request-1"); err != nil {
		t.Fatal(err)
	}
}

func TestNewProductionClockAuthority_RejectsMissingPort(t *testing.T) {
	_, err := NewProductionClockAuthority(ProductionClockDependencies{Workers: productionClockWorkers{}})
	if !errors.Is(err, errClockAuthorityUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
