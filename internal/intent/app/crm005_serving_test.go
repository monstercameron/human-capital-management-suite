package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/crm"
	identity "github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type crm005ServingIdentityOwner struct {
	links []identity.IdentityLink
}

func (o *crm005ServingIdentityOwner) LoadIdentityLinks(_ context.Context, tenant values.TenantId, purpose, system, externalID string) ([]identity.IdentityLink, error) {
	for _, link := range o.links {
		if link.TenantRef == string(tenant) && link.PurposeScope == purpose && link.ExternalSystem == system && link.ExternalID == externalID {
			return append([]identity.IdentityLink(nil), o.links...), nil
		}
	}
	return nil, errors.New("identity not found")
}

func crm005ServedRef(tenant values.TenantId, kind values.Kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: kind, Id: id}
}

func crm005ServedAt(text string) values.Instant {
	parsed, _ := time.Parse(time.RFC3339, text)
	return values.NewInstant(parsed)
}

func crm005ServedProspect(t *testing.T) crm.ProspectRevision {
	t.Helper()
	tenant := values.TenantId("tenant-crm-005")
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenLocalDateInterval(start, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("crm-005", 1)
	if err != nil {
		t.Fatal(err)
	}
	return crm.ProspectRevision{
		ProspectID: crm005ServedRef(tenant, "prospect", "00000000-0000-4000-8000-000000000001"),
		Revision:   revision,
		Attribution: crm.ProspectSourceAttribution{
			Source:   crm.SourceAttribution{System: "ats", Reference: "ats-prospect-1", RecordedBy: crm005ServedRef(tenant, "principal", "00000000-0000-4000-8000-000000000002")},
			Campaign: "spring-2026", Channel: "referral",
		},
		Consent: crm.ProspectConsent{
			Authority: crm005ServedRef(tenant, "processing_authority", "00000000-0000-4000-8000-000000000003"),
			Basis:     crm.ProspectConsentBasis, Evidence: crm005ServedRef(tenant, "evidence", "00000000-0000-4000-8000-000000000004"),
			GrantedAt: crm005ServedAt("2026-01-01T00:00:00Z"), ExpiresAt: crm005ServedAt("2026-02-01T00:00:00Z"),
		},
		Effective: effective,
		Owner:     crm005ServedRef(tenant, "owner", "00000000-0000-4000-8000-000000000005"),
	}
}

func crm005ServedInvocation(t *testing.T) (CRM005ConversionInvocation, *crm005ServingIdentityOwner) {
	t.Helper()
	prospect := crm005ServedProspect(t)
	tenant := prospect.ProspectID.Tenant
	at := crm005ServedAt("2026-01-03T00:00:00Z")
	candidate := crm005ServedRef(tenant, "candidate", "00000000-0000-4000-8000-000000000006")
	application := crm005ServedRef(tenant, "application", "00000000-0000-4000-8000-000000000007")
	linkRole := crm005ServedRef(tenant, "identity_link", "00000000-0000-4000-8000-000000000008")
	proposal, _, err := crm.PrepareProspectConversion(prospect, candidate, application, linkRole, crm.CRM005IntentType, crm.CRM005IntentVersion, at)
	if err != nil {
		t.Fatal(err)
	}
	person := crm005ServedRef(tenant, "person", "00000000-0000-4000-8000-000000000009")
	effective, err := values.NewInstantInterval(crm005ServedAt("2026-01-01T00:00:00Z"), crm005ServedAt("2026-02-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	owner := &crm005ServingIdentityOwner{links: []identity.IdentityLink{{
		LinkRef: "identity-link-1", ExternalSystem: "ats", ExternalID: "ats-person-1", CanonicalRef: person.String(),
		MatchKey: "ATS_PERSON_ID", Confidence: 1, Effective: effective, TenantRef: string(tenant), PurposeScope: "candidate_conversion",
		EvidenceRef: "identity-evidence-1", SourceAuthorityRef: "identity-authority-1",
	}}}
	return CRM005ConversionInvocation{
		Proposal: proposal, IdentityOwner: owner, Tenant: string(tenant), Purpose: "candidate_conversion",
		ExternalSystem: "ats", ExternalID: "ats-person-1", Fence: crm.ConversionCommitFence{ProspectRevision: prospect.Revision, Token: "crm-005-fence-1"},
		IdempotencyKey: "crm-005-conversion-1",
	}, owner
}

// TestTodo_CRM_005_Served proves the shipped application registry publishes
// CRM-005 and routes its governed application step to identity resolution and
// one atomic recruiting write. The gateway refusal is also checked because an
// internal mutation must not accidentally become a P1A side effect.
func TestTodo_CRM_005_Served(t *testing.T) {
	store := crm.NewConversionStore()
	handlers := &domainHandlers{crm005: NewCRM005ConversionService(store)}
	registry, err := newCapabilityRegistry(handlers)
	if err != nil {
		t.Fatal(err)
	}
	key := capability.Key{ID: CRM005CapabilityID, Version: 1}
	record, ok := registry.Lookup(key)
	if !ok || record.Definition.EffectClass != capability.EffectInternalMutation || record.Definition.WriteData.DataDomains == nil {
		t.Fatalf("CRM-005 record=%+v published=%v", record, ok)
	}
	in, _ := crm005ServedInvocation(t)
	firstAny, err := handlers.handlerFor(CRM005CapabilityID)(context.Background(), in)
	if err != nil {
		t.Fatalf("first served conversion: %v", err)
	}
	first, ok := firstAny.(crm.ConversionResult)
	if !ok || first.Replayed {
		t.Fatalf("first result=%T %+v, want non-replay conversion", firstAny, firstAny)
	}
	candidates, applications, links := store.ConversionCounts()
	if candidates != 1 || applications != 1 || links != 1 {
		t.Fatalf("role counts=(%d,%d,%d), want one candidate/application/link", candidates, applications, links)
	}
	stored, ok := store.Conversion(in.Proposal.Prospect.ProspectID.Tenant, in.IdempotencyKey)
	if !ok || stored.Candidate.Source.Campaign != "spring-2026" || stored.Application.Consent.Authority != in.Proposal.Consent.Authority || stored.IdentityLink.Person.Kind != "person" {
		t.Fatalf("stored conversion=%+v, want complete lineage", stored)
	}
	secondAny, err := handlers.handlerFor(CRM005CapabilityID)(context.Background(), in)
	if err != nil {
		t.Fatalf("replayed served conversion: %v", err)
	}
	second := secondAny.(crm.ConversionResult)
	if !second.Replayed {
		t.Fatalf("replay result=%+v, want replay", second)
	}
	candidates, applications, links = store.ConversionCounts()
	if candidates != 1 || applications != 1 || links != 1 {
		t.Fatalf("replay role counts=(%d,%d,%d), want unchanged", candidates, applications, links)
	}

	sink := NewMemoryEvidenceSink()
	gateway := capability.NewGateway(registry, sink)
	_, err = gateway.Invoke(context.Background(), capability.InvokeRequest{Capability: key, Payload: in, Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:recruiting.candidate_conversion"}, Tenant: string(in.Proposal.Prospect.ProspectID.Tenant)}})
	var gatewayErr *capability.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != capability.CodeWriteEffectRefusedP1A {
		t.Fatalf("P1A gateway error=%v, want write-effect refusal", err)
	}
	candidates, applications, links = store.ConversionCounts()
	if candidates != 1 || applications != 1 || links != 1 {
		t.Fatalf("P1A refusal changed role counts=(%d,%d,%d)", candidates, applications, links)
	}
}
