package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type shiftIdentityPort struct {
	ShiftSelfServicePort
	owner     values.EntityRef
	claim     values.EntityRef
	standings []schedopt.WorkerStanding
	changedAt time.Time
}

func (p *shiftIdentityPort) ClaimOpenShift(_ string, worker values.EntityRef, _ uint64, standings []schedopt.WorkerStanding, changedAt time.Time) (schedopt.ShiftSelfServiceResult, error) {
	p.claim, p.standings, p.changedAt = worker, append([]schedopt.WorkerStanding(nil), standings...), changedAt
	return schedopt.ShiftSelfServiceResult{}, nil
}

func (p *shiftIdentityPort) OfferOpenShift(_ string, _ string, owner values.EntityRef, _ uint64, _ string) (schedopt.ShiftOffer, error) {
	p.owner = owner
	return schedopt.ShiftOffer{ID: "offer"}, nil
}

func TestShiftSelfServiceActorUsesResolvedPrincipalWorker(t *testing.T) {
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "opaque-subject", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := values.EntityRef{Tenant: "tenant-a", Kind: "candidate", Id: uuid.NewString()}
	port := &shiftIdentityPort{}
	changedAt := now.UTC().Truncate(time.Second)
	actor := ShiftSelfServiceActor{
		Service: port, Now: func() time.Time { return changedAt },
		ResolveWorker: func(_ context.Context, got *trust.Principal) (values.EntityRef, error) {
			if got != principal {
				t.Fatalf("resolver received a different principal")
			}
			return worker, nil
		},
		ResolveStandings: func(_ context.Context, tenant values.TenantId) ([]schedopt.WorkerStanding, error) {
			if tenant != principal.Tenant() {
				t.Fatalf("standings resolved for tenant %s, want %s", tenant, principal.Tenant())
			}
			return []schedopt.WorkerStanding{{CandidateRef: worker, AccruedFatigueMinutes: 10}}, nil
		},
	}
	if _, err := actor.OfferOpenShift(context.Background(), principal, "offer", "assignment", 1, "pickup"); err != nil {
		t.Fatal(err)
	}
	if port.owner != worker {
		t.Fatalf("service actor = %v, want resolver-bound worker %v", port.owner, worker)
	}
	if _, err := actor.ClaimOpenShift(context.Background(), principal, "offer", 2); err != nil {
		t.Fatal(err)
	}
	if port.claim != worker || len(port.standings) != 1 || port.standings[0].CandidateRef != worker || !port.changedAt.Equal(changedAt) {
		t.Fatalf("claim identity/facts/time were not server-derived: worker=%v standings=%+v at=%s", port.claim, port.standings, port.changedAt)
	}
	actor.ResolveWorker = func(context.Context, *trust.Principal) (values.EntityRef, error) {
		return values.EntityRef{Tenant: "tenant-b", Kind: "candidate", Id: uuid.NewString()}, nil
	}
	if _, err := actor.OfferOpenShift(context.Background(), principal, "offer-2", "assignment", 1, "pickup"); !errors.Is(err, ErrShiftSelfServiceIdentity) {
		t.Fatalf("cross-tenant resolved identity = %v, want identity refusal", err)
	}
}
