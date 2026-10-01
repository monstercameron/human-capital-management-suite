package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaStarterFakeCatalog struct {
	calls  int
	record rewards.BandRecord
}

type personaStarterFakeCompa struct{ calls int }

func (f *personaStarterFakeCompa) PayBandPosition(context.Context, values.EntityRef, values.Instant) (rewards.PayBandEvaluation, error) {
	f.calls++
	return rewards.PayBandEvaluation{}, nil
}

type personaStarterFakeScenario struct{ calls int }

func (f *personaStarterFakeScenario) SimulateCompensation(context.Context, rewards.SimulateCompensationInput) (rewards.SimulateCompensationResult, error) {
	f.calls++
	return rewards.SimulateCompensationResult{}, nil
}

func (f *personaStarterFakeCatalog) LookupBand(context.Context, rewards.BandQuery) (rewards.BandRecord, error) {
	f.calls++
	return f.record, nil
}

func TestPersonaStarterCompensationBindingPublishesCapabilitySkills(t *testing.T) {
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	fake := &personaStarterFakeCatalog{}
	pins, err := BindPersonaCompensationSkills(caps, skills, fake, &personaStarterFakeCompa{}, &personaStarterFakeScenario{})
	if err != nil {
		t.Fatalf("bind compensation skills: %v", err)
	}
	if len(pins) != 3 {
		t.Fatalf("pins = %d, want 3", len(pins))
	}
	for _, pin := range pins {
		t.Logf("verified pin %s@%d=%s", pin.ID, pin.Version, pin.Digest)
	}
	for _, id := range []string{personaCompBandCapabilityID, personaCompaCapabilityID, personaCompScenarioCapabilityID} {
		if _, ok := caps.Lookup(capability.Key{ID: id, Version: 1}); !ok {
			t.Fatalf("capability %s was not registered", id)
		}
	}
	for _, id := range []string{"hcmnext.skill.comp_band_read", "hcmnext.skill.compa_ratio_read", "hcmnext.skill.comp_scenario_draft"} {
		record, ok := skills.Lookup(agentskills.SkillKey{ID: id, Version: 1})
		if !ok || len(record.ResolvedOperations) != 1 || record.ResolvedOperations[0].Reference.Kind != agentskills.OperationCapability {
			t.Fatalf("skill %s was not bound to a capability: %#v", id, record)
		}
		if len(record.Digest) != 64 {
			t.Fatalf("skill %s digest = %q, want sha256 hex", id, record.Digest)
		}
	}
}

func TestPersonaStarterCompensationHandlersValidateBeforePort(t *testing.T) {
	fake := &personaStarterFakeCatalog{}
	h := personaCompBandHandler{port: fake}
	date, err := values.NewLocalDate(2026, time.January, 2)
	if err != nil {
		t.Fatal(err)
	}
	query := rewards.BandQuery{Tenant: values.TenantId("tenant-a"), JobCode: "ENG", Grade: "G7", PayZone: "US", Currency: "USD", AsOf: date}
	_, err = h.handle(context.Background(), PersonaCompBandCall{Query: query})
	if err == nil || !errors.Is(err, errPersonaStarterBinding) {
		t.Fatalf("missing principal error = %v, want binding error", err)
	}
	if fake.calls != 0 {
		t.Fatalf("port calls = %d after invalid query, want 0", fake.calls)
	}

	want := rewards.BandRecord{CatalogVersion: "test"}
	fake.record = want
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "human", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.handle(trust.WithPrincipal(context.Background(), principal), PersonaCompBandCall{Query: query})
	if err != nil {
		t.Fatalf("valid query: %v", err)
	}
	if got.(rewards.BandRecord).CatalogVersion != want.CatalogVersion || fake.calls != 1 {
		t.Fatalf("got %#v with %d port calls, want fake record and one call", got, fake.calls)
	}
}

func TestPersonaStarterCompensationBindingRequiresRealPort(t *testing.T) {
	if _, err := BindPersonaCompensationSkills(capability.NewRegistry(), agentskills.NewRegistry(capability.NewRegistry()), nil, nil, nil); !errors.Is(err, errPersonaStarterBinding) {
		t.Fatalf("nil port error = %v, want binding sentinel", err)
	}
}

func TestPersonaStarterCompaHandlerUsesCanonicalWorkerSourceAndTenant(t *testing.T) {
	source := &personaStarterFakeCompa{}
	h := personaCompaHandler{source: source}
	worker := values.EntityRef{Tenant: values.TenantId("tenant-a"), Kind: people.KindWorker, Id: "11111111-1111-4111-8111-111111111111"}
	asOf := values.Instant{}
	if _, err := h.handle(context.Background(), PersonaCompaRatioCall{Worker: worker, AsOf: asOf}); !errors.Is(err, errPersonaStarterBinding) {
		t.Fatalf("missing principal error = %v, want binding sentinel", err)
	}
	if source.calls != 0 {
		t.Fatalf("source calls = %d before authority, want 0", source.calls)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "human", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.handle(trust.WithPrincipal(context.Background(), principal), PersonaCompaRatioCall{Worker: worker, AsOf: asOf})
	if err == nil || source.calls != 0 {
		t.Fatalf("invalid worker/as-of returned %v with %d source calls, want validation and no source call", err, source.calls)
	}
	validAsOf := values.NewInstant(time.Unix(10, 0))
	got, err := h.handle(trust.WithPrincipal(context.Background(), principal), PersonaCompaRatioCall{Worker: worker, AsOf: validAsOf})
	if err != nil {
		t.Fatalf("valid canonical compa source: %v", err)
	}
	if _, ok := got.(rewards.PayBandEvaluation); !ok || source.calls != 1 {
		t.Fatalf("source result = %#v with %d calls, want PayBandEvaluation and one call", got, source.calls)
	}
}
