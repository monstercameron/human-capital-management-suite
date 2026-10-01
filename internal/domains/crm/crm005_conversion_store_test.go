package crm

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func crm005StoreInput(t *testing.T) (CandidateConversionProposal, CanonicalPersonBinding, ConversionCommitFence) {
	t.Helper()
	prospect := validProspect(t)
	proposal, _, err := PrepareProspectConversion(
		prospect,
		crmRef("candidate", "c-1"),
		crmRef("application", "a-1"),
		crmRef("identity_link", "i-1"),
		CRM005IntentType,
		CRM005IntentVersion,
		conversionAt(t, "2026-01-03T00:00:00Z"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return proposal, CanonicalPersonBinding{
		Person:             crm005Person(prospect.ProspectID.Tenant, "11111111-1111-4111-8111-111111111111"),
		LinkRef:            "identity-link-1",
		EvidenceRef:        "identity-evidence-1",
		SourceAuthorityRef: "identity-authority-1",
		Tenant:             prospect.ProspectID.Tenant,
		Purpose:            "candidate_conversion",
	}, ConversionCommitFence{ProspectRevision: prospect.Revision, Token: "fence-1"}
}

func TestCRM005ConversionStoreAtomicReplayAndLineage(t *testing.T) {
	proposal, identity, fence := crm005StoreInput(t)
	store := NewConversionStore()
	first, err := ExecuteProspectConversionWithIdentity(context.Background(), proposal, identity, store, store, fence, "conversion-1")
	if err != nil || first.Replayed {
		t.Fatalf("first conversion=%+v err=%v", first, err)
	}
	second, err := ExecuteProspectConversionWithIdentity(context.Background(), proposal, identity, store, store, fence, "conversion-1")
	if err != nil || !second.Replayed || second.Candidate != first.Candidate || second.Application != first.Application || second.IdentityLink != first.IdentityLink {
		t.Fatalf("replay=%+v err=%v, want original result marked replayed", second, err)
	}
	candidate, application, link := store.ConversionCounts()
	if candidate != 1 || application != 1 || link != 1 {
		t.Fatalf("role counts=(%d,%d,%d), want one each", candidate, application, link)
	}
	record, ok := store.Conversion(proposal.Prospect.ProspectID.Tenant, "conversion-1")
	if !ok || record.Candidate.Source != proposal.Source || record.Application.Consent != proposal.Consent || record.IdentityLink.Person != identity.Person {
		t.Fatalf("record=%+v, want source/consent/person lineage", record)
	}
}

func TestCRM005ConversionStoreRejectsMutationBeforeAdditionalWrites(t *testing.T) {
	proposal, identity, fence := crm005StoreInput(t)
	store := NewConversionStore()
	if _, err := ExecuteProspectConversionWithIdentity(context.Background(), proposal, identity, store, store, fence, "conversion-1"); err != nil {
		t.Fatal(err)
	}
	mutated := proposal
	mutated.Source.Campaign = "forged-campaign"
	if _, err := ExecuteProspectConversionWithIdentity(context.Background(), mutated, identity, store, store, fence, "conversion-2"); !errors.Is(err, ErrCRM005Rejected) {
		t.Fatalf("mutated proposal error=%v, want CRM_005_REJECTED", err)
	}
	if candidate, application, link := store.ConversionCounts(); candidate != 1 || application != 1 || link != 1 {
		t.Fatalf("mutation changed role counts=(%d,%d,%d)", candidate, application, link)
	}
	forgedIdentity := identity
	forgedIdentity.Person = crm005Person(values.TenantId("tenant-other"), "22222222-2222-4222-8222-222222222222")
	if _, err := ExecuteProspectConversionWithIdentity(context.Background(), proposal, forgedIdentity, store, store, fence, "conversion-3"); !errors.Is(err, ErrCRM005Rejected) {
		t.Fatalf("cross-tenant identity error=%v, want CRM_005_REJECTED", err)
	}
	if candidate, application, link := store.ConversionCounts(); candidate != 1 || application != 1 || link != 1 {
		t.Fatalf("identity mutation changed role counts=(%d,%d,%d)", candidate, application, link)
	}
}
