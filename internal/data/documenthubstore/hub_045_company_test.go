package documenthubstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestTodo_HUB_045_CompanyGrantNeedsBilateral closes the path the HUB-045
// adversarial pass found: a company-scoped document_grant written directly,
// with no propose/accept exchange, must not open any surface that authorizes
// through the shared check. It opens only while an accepted, unexpired and
// unrevoked bilateral grant exists for that document and consumer tenant.
func TestTodo_HUB_045_CompanyGrantNeedsBilateral(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	at := time.Now()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "company", SubjectID: "vendor", Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "company", "vendor", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("company grant without a bilateral grant authorized: %v", err)
	}

	terms := crossCompanyTerms()
	terms.DocumentID = docID
	terms.ExpiresAt = at.Add(time.Hour)
	proposal, err := s.ProposeCrossCompanyGrant(ctx, "tenant-a", "u-owner", terms, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "company", "vendor", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("a proposal the consumer never accepted authorized: %v", err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, "tenant-a", proposal.ID, terms.ConsumerTenant, at); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "company", "vendor", ActionRead); err != nil {
		t.Fatalf("company grant inside an accepted bilateral grant refused: %v", err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "company", "other-vendor", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("a bilateral grant for one consumer opened another: %v", err)
	}

	if err := s.RevokeCrossCompanyGrant(ctx, "tenant-a", proposal.ID, "u-owner", at); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "company", "vendor", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("company grant still authorized after the bilateral grant was revoked: %v", err)
	}
}
