package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/availability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestPersonaStarterDomainHandlersFailClosedWithoutTrustedTenant(t *testing.T) {
	worker := values.EntityRef{Tenant: values.TenantId("tenant-a"), Id: "11111111-1111-4111-8111-111111111111"}
	if err := personaWorker(context.Background(), worker); !errors.Is(err, errPersonaStarterBinding) {
		t.Fatalf("personaWorker error = %v, want starter binding refusal", err)
	}
	if err := personaWorker(context.Background(), values.EntityRef{Tenant: values.TenantId("bad tenant"), Id: "11111111-1111-4111-8111-111111111111"}); !errors.Is(err, errPersonaStarterBinding) {
		t.Fatalf("invalid tenant error = %v, want starter binding refusal", err)
	}
}

type personaStarterDomainPinFake struct{}

func (personaStarterDomainPinFake) TaskStates(context.Context, values.EntityRef) ([]PersonaOnboardingTaskState, error) {
	return nil, nil
}

func (personaStarterDomainPinFake) Checklist(context.Context, values.EntityRef) (workerlifecycle.ReadinessResolution, error) {
	return workerlifecycle.ReadinessResolution{}, nil
}
func (personaStarterDomainPinFake) Tasks(context.Context, values.EntityRef) ([]workerlifecycle.ChildIntent, error) {
	return nil, nil
}
func (personaStarterDomainPinFake) DraftWelcome(context.Context, values.EntityRef) (string, error) {
	return "welcome", nil
}
func (personaStarterDomainPinFake) PrepareWelcomePost(context.Context, *trust.Principal, values.EntityRef, string, string) (PersonaWelcomePostIntent, error) {
	return PersonaWelcomePostIntent{}, nil
}
func (personaStarterDomainPinFake) Schedule(context.Context, values.EntityRef) (availability.VersionedWorkSchedule, error) {
	return availability.VersionedWorkSchedule{}, nil
}
func (personaStarterDomainPinFake) DraftSwap(context.Context, *trust.Principal, string, string, string, uint64, string) (PersonaShiftSwapProposal, error) {
	return PersonaShiftSwapProposal{}, nil
}
func (personaStarterDomainPinFake) SubmitShiftChange(context.Context, *trust.Principal, string, uint64) (schedopt.ShiftSelfServiceResult, error) {
	return schedopt.ShiftSelfServiceResult{}, nil
}

func TestPersonaStarterDomainBinderPins(t *testing.T) {
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	fake := personaStarterDomainPinFake{}
	got, err := BindPersonaOnboardingSkills(caps, skills, fake)
	if err != nil {
		t.Fatalf("bind onboarding: %v", err)
	}
	schedulePins, err := BindPersonaScheduleSkills(caps, skills, fake)
	if err != nil {
		t.Fatalf("bind schedule: %v", err)
	}
	got = append(got, schedulePins...)
	if len(got) != 7 {
		t.Fatalf("pins = %d, want 7", len(got))
	}
	for _, pin := range got {
		t.Logf("verified pin %s@%d=%s", pin.ID, pin.Version, pin.Digest)
	}
}

func TestPersonaScheduleActorPortDraftRefusesMissingPublishedFacts(t *testing.T) {
	at := time.Unix(1, 0)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "human", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: at, ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	port := PersonaScheduleActorPort{}
	proposal, err := port.DraftSwap(context.Background(), principal, "offer-1", "assignment-a", "assignment-b", 3, "coverage")
	if !errors.Is(err, ErrShiftSelfServiceFacts) || proposal.OfferID != "" {
		t.Fatalf("draft without a current grounded source = %#v, %v", proposal, err)
	}
}
