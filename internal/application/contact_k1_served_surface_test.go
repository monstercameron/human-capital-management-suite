package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedContactSubject() values.EntityRef {
	return values.EntityRef{Tenant: "tenant-contact-served", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}
}

func TestTodo_CONTACT_001_Served(t *testing.T) {
	surface := application.NewServedContactSurface()
	if surface.ContractID != contact.ServingContractID || surface.Version == nil || surface.NewEndpointRevision == nil || surface.MarkVerified == nil || surface.IssueChallenge == nil || surface.IssueVerification == nil || surface.VerifyVerification == nil {
		t.Fatal("served contact surface is incomplete")
	}

	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	subject := servedContactSubject()
	endpoint, err := surface.NewEndpointRevision(subject, "endpoint-served", contact.EndpointEmail, "Person@example.com", "account-recovery", 1, "worker-profile")
	if err != nil {
		t.Fatalf("served endpoint: %v", err)
	}
	verified, err := surface.MarkVerified(endpoint)
	if err != nil || verified.Verification != contact.Verified {
		t.Fatalf("served verification revision=%+v err=%v", verified, err)
	}
	challenge, token, err := surface.IssueChallenge("challenge-served", subject, endpoint, "account-recovery", "123456", now, time.Hour, 2)
	if err != nil || token != "123456" || challenge.TokenDigest == "" {
		t.Fatalf("served challenge=%+v token=%q err=%v", challenge, token, err)
	}
	answered, status, err := challenge.Respond(token, now.Add(time.Minute))
	if err != nil || status != contact.ContactChallengeVerified || answered.Status != contact.ContactChallengeVerified {
		t.Fatalf("served challenge response=%+v status=%s err=%v", answered, status, err)
	}

	legacy, token, err := surface.IssueVerification(subject, "endpoint-served", "account-recovery", "654321", now, time.Hour)
	if err != nil {
		t.Fatalf("served legacy challenge: %v", err)
	}
	if got := surface.VerifyVerification(&legacy, subject, "endpoint-served", "account-recovery", token, now.Add(time.Minute)); got != contact.ChallengeVerified {
		t.Fatalf("served legacy verification status=%s", got)
	}
}

func TestTodo_CONTACT_003_Served(t *testing.T) {
	surface := application.NewServedContactSurface()
	if surface.NewMemoryStore == nil || surface.PutEndpointRevision == nil || surface.PutChallenge == nil || surface.CodeOf == nil {
		t.Fatal("served contact store surface is incomplete")
	}
	ctx := context.Background()
	tenant := values.TenantId("tenant-contact-served")
	subject := servedContactSubject()
	endpoint, err := surface.NewEndpointRevision(subject, "endpoint-store-served", contact.EndpointEmail, "Person@example.com", "recovery", 1, "profile")
	if err != nil {
		t.Fatal(err)
	}
	store := surface.NewMemoryStore()
	if err := surface.PutEndpointRevision(store, ctx, tenant, endpoint); err != nil {
		t.Fatalf("served endpoint write: %v", err)
	}
	if got := surface.CodeOf(surface.PutEndpointRevision(store, ctx, tenant, endpoint)); got != contact.StoreDuplicateCode {
		t.Fatalf("served duplicate endpoint code=%q", got)
	}
	challenge, _, err := surface.IssueChallenge("challenge-store-served", subject, endpoint, "recovery", "123456", time.Unix(100, 0), time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := surface.PutChallenge(store, ctx, tenant, challenge); err != nil {
		t.Fatalf("served challenge write: %v", err)
	}
	updated, _, err := challenge.Respond("wrong", time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got := surface.CodeOf(surface.PutChallenge(store, ctx, tenant, updated, "sha256:stale")); got != contact.StoreStaleCASCode {
		t.Fatalf("served stale challenge code=%q", got)
	}
	if err := surface.PutChallenge(store, ctx, tenant, updated, challenge.CanonicalDigest); err != nil {
		t.Fatalf("served challenge update: %v", err)
	}
	loaded, err := store.GetChallenge(ctx, tenant, challenge.ChallengeID)
	if err != nil || len(loaded.Events) != 2 {
		t.Fatalf("served loaded challenge=%+v err=%v", loaded, err)
	}

	var nilApp *application.App
	if exposed := nilApp.Contact(); exposed.Version != nil || exposed.NewMemoryStore != nil {
		t.Fatal("nil application exposed contact capabilities")
	}
}
