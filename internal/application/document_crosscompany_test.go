package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// TestTodo_HUB_015 is the PRIMARY test for HUB-015 at the application
// boundary: proposing, accepting and then authorizing a cross-company read
// through documentService requires both halves of the gate.
func TestTodo_HUB_015(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, consumer = "tenant-xc", "u-owner", "vendor-co"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	terms := documenthubstore.CrossCompanyGrantTerms{
		DocumentID: id, HostTenant: tenant, ConsumerTenant: consumer,
		Classification: "confidential", Residency: "US", ExpiresAt: time.Now().Add(time.Hour),
	}
	proposal, err := svc.ProposeDocumentCrossCompanyGrant(ctx, tenant, owner, terms)
	if err != nil {
		t.Fatalf("propose refused: %v", err)
	}
	if err := svc.AuthorizeDocumentCrossCompanyRead(ctx, tenant, id, consumer, "u-vendor", documenthubstore.ActionRead); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("unaccepted proposal authorized read: %v", err)
	}
	if _, err := svc.AcceptDocumentCrossCompanyGrant(ctx, tenant, proposal.ID, consumer); err != nil {
		t.Fatalf("accept refused: %v", err)
	}
	if err := svc.AuthorizeDocumentCrossCompanyRead(ctx, tenant, id, consumer, "u-vendor", documenthubstore.ActionRead); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("bilateral grant alone authorized read without an explicit document grant: %v", err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "company", SubjectID: consumer, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AuthorizeDocumentCrossCompanyRead(ctx, tenant, id, consumer, "u-vendor", documenthubstore.ActionRead); err != nil {
		t.Fatalf("bilateral+explicit grant refused: %v", err)
	}
}

// TestTodo_HUB_015_Security is the SECURITY test for HUB-015 at the
// application boundary: revoking the bilateral grant through
// documentService closes egress immediately.
func TestTodo_HUB_015_Security(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, consumer = "tenant-xc-sec", "u-owner", "vendor-co"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	terms := documenthubstore.CrossCompanyGrantTerms{
		DocumentID: id, HostTenant: tenant, ConsumerTenant: consumer,
		Classification: "confidential", Residency: "US", ExpiresAt: time.Now().Add(time.Hour),
	}
	proposal, err := svc.ProposeDocumentCrossCompanyGrant(ctx, tenant, owner, terms)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptDocumentCrossCompanyGrant(ctx, tenant, proposal.ID, consumer); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectKind: "company", SubjectID: consumer, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AuthorizeDocumentCrossCompanyRead(ctx, tenant, id, consumer, "u-vendor", documenthubstore.ActionRead); err != nil {
		t.Fatalf("accepted+granted read refused before revoke: %v", err)
	}
	if err := svc.RevokeDocumentCrossCompanyGrant(ctx, tenant, proposal.ID, owner); err != nil {
		t.Fatal(err)
	}
	if err := svc.AuthorizeDocumentCrossCompanyRead(ctx, tenant, id, consumer, "u-vendor", documenthubstore.ActionRead); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("revoked bilateral grant still authorized: %v", err)
	}
}

// TestTodo_HUB_015_Integration is the INTEGRATION test for HUB-015 at the
// application boundary: the accepted grant persists across independent
// documentService calls against the same fixture store.
func TestTodo_HUB_015_Integration(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, consumer = "tenant-xc-int", "u-owner", "vendor-co"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	terms := documenthubstore.CrossCompanyGrantTerms{
		DocumentID: id, HostTenant: tenant, ConsumerTenant: consumer,
		Classification: "confidential", Residency: "US", ExpiresAt: time.Now().Add(time.Hour),
	}
	proposal, err := svc.ProposeDocumentCrossCompanyGrant(ctx, tenant, owner, terms)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.AcceptDocumentCrossCompanyGrant(ctx, tenant, proposal.ID, consumer)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted.AcceptedByConsumer || !accepted.AcceptedByHost {
		t.Fatalf("accepted grant not bilaterally consented: %+v", accepted)
	}
	if _, err := svc.AcceptDocumentCrossCompanyGrant(ctx, tenant, proposal.ID, consumer); !errors.Is(err, documenthubstore.ErrCrossCompanyTerms) {
		t.Fatalf("double acceptance accepted: %v", err)
	}
}
