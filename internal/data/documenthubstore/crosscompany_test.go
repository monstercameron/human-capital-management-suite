package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func crossCompanyTerms() CrossCompanyGrantTerms {
	return CrossCompanyGrantTerms{
		DocumentID: "doc-1", HostTenant: "tenant-a", ConsumerTenant: "vendor",
		Classification: "confidential", Residency: "US", ExpiresAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestTodo_HUB_015_Golden is the GOLDEN test for HUB-015: the canonical
// bilateral proposal terms are pinned for audit consumers.
func TestTodo_HUB_015_Golden(t *testing.T) {
	terms := crossCompanyTerms()
	terms.DocumentID = "doc-fixed"
	canonical, err := json.Marshal(map[string]string{
		"classification": terms.Classification, "consumer_tenant": terms.ConsumerTenant,
		"document_id": terms.DocumentID, "expires_at": terms.ExpiresAt.Format(time.RFC3339),
		"host_tenant": terms.HostTenant, "residency": terms.Residency,
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub015_crosscompany_grant.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("cross-company grant terms changed:\n got %s\nwant %s", canonical, want)
	}
}

// TestTodo_HUB_015 is the PRIMARY test for HUB-015: a cross-company read or
// export needs both a bilaterally accepted grant and an explicit document
// grant; neither alone suffices, and their intersection admits the action.
func TestTodo_HUB_015(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	at := time.Now()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	terms := crossCompanyTerms()
	terms.DocumentID = docID
	terms.ExpiresAt = at.Add(time.Hour)

	// Bilateral grant alone, no explicit document_grant: refused.
	proposal, err := s.ProposeCrossCompanyGrant(ctx, "tenant-a", "u-owner", terms, at)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.AcceptCrossCompanyGrant(ctx, "tenant-a", proposal.ID, terms.ConsumerTenant, at)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted.Current(at, terms) {
		t.Fatal("accepted bilateral grant not current")
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID, terms.ConsumerTenant, "u-vendor-1", ActionRead, at); !errors.Is(err, ErrDenied) {
		t.Fatalf("bilateral grant alone authorized read: %v", err)
	}

	// Explicit document grant alone, no bilateral acceptance: refused.
	docID2, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID2, SubjectKind: "company", SubjectID: terms.ConsumerTenant, Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID2, terms.ConsumerTenant, "u-vendor-1", ActionRead, at); !errors.Is(err, ErrDenied) {
		t.Fatalf("explicit grant alone authorized cross-company read: %v", err)
	}

	// Both together: admitted.
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "company", SubjectID: terms.ConsumerTenant, Action: ActionRead, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID, terms.ConsumerTenant, "u-vendor-1", ActionRead, at); err != nil {
		t.Fatalf("bilateral+explicit grant refused: %v", err)
	}

	// Revocation closes egress even though the bilateral grant remains
	// accepted.
	if err := s.RevokeCrossCompanyGrant(ctx, "tenant-a", proposal.ID, "u-owner", at); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID, terms.ConsumerTenant, "u-vendor-1", ActionRead, at); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked bilateral grant still authorized: %v", err)
	}
}

// TestTodo_HUB_015_Security is the SECURITY test for HUB-015: unbounded
// terms are refused outright, a proposal cannot be filed against a foreign
// host tenant, only the named consumer may accept, and export follows the
// same gate as read.
func TestTodo_HUB_015_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	at := time.Now()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*CrossCompanyGrantTerms)
	}{
		{"unbounded expiry", func(v *CrossCompanyGrantTerms) { v.ExpiresAt = time.Time{} }},
		{"missing classification", func(v *CrossCompanyGrantTerms) { v.Classification = "" }},
		{"missing residency", func(v *CrossCompanyGrantTerms) { v.Residency = "" }},
		{"same tenant", func(v *CrossCompanyGrantTerms) { v.ConsumerTenant = v.HostTenant }},
		{"already expired", func(v *CrossCompanyGrantTerms) { v.ExpiresAt = at.Add(-time.Hour) }},
	} {
		terms := crossCompanyTerms()
		terms.DocumentID = docID
		terms.ExpiresAt = at.Add(time.Hour)
		tt.mutate(&terms)
		if _, err := s.ProposeCrossCompanyGrant(ctx, "tenant-a", "u-owner", terms, at); !errors.Is(err, ErrCrossCompanyTerms) {
			t.Fatalf("%s: accepted, err=%v", tt.name, err)
		}
	}
	terms := crossCompanyTerms()
	terms.DocumentID = docID
	terms.ExpiresAt = at.Add(time.Hour)
	if _, err := s.ProposeCrossCompanyGrant(ctx, "tenant-b", "u-owner", terms, at); !errors.Is(err, ErrCrossCompanyTerms) {
		t.Fatalf("proposal filed against foreign host tenant: %v", err)
	}
	proposal, err := s.ProposeCrossCompanyGrant(ctx, "tenant-a", "u-owner", terms, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, "tenant-a", proposal.ID, "impostor-tenant", at); !errors.Is(err, ErrCrossCompanyTerms) {
		t.Fatalf("wrong consumer accepted: %v", err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "company", SubjectID: terms.ConsumerTenant, Action: ActionExport, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID, terms.ConsumerTenant, "u-vendor-1", ActionExport, at); !errors.Is(err, ErrDenied) {
		t.Fatalf("export authorized before bilateral acceptance: %v", err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, "tenant-a", proposal.ID, terms.ConsumerTenant, at); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID, terms.ConsumerTenant, "u-vendor-1", ActionExport, at); err != nil {
		t.Fatalf("export refused once both gates are satisfied: %v", err)
	}
	if err := s.AuthorizeCrossCompanyRead(ctx, "tenant-a", docID, "", "u-vendor-1", ActionRead, at); !errors.Is(err, ErrDenied) {
		t.Fatalf("blank consumer tenant authorized: %v", err)
	}
}

// TestTodo_HUB_015_Integration is the INTEGRATION test for HUB-015: the
// bilateral grant is durable, re-acceptance is refused, and a drifted term
// (residency changed after acceptance) invalidates Current without
// touching the stored row's own fields.
func TestTodo_HUB_015_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	at := time.Now()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	terms := crossCompanyTerms()
	terms.DocumentID = docID
	terms.ExpiresAt = at.Add(time.Hour)
	proposal, err := s.ProposeCrossCompanyGrant(ctx, "tenant-a", "u-owner", terms, at)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.AcceptCrossCompanyGrant(ctx, "tenant-a", proposal.ID, terms.ConsumerTenant, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptCrossCompanyGrant(ctx, "tenant-a", proposal.ID, terms.ConsumerTenant, at); !errors.Is(err, ErrCrossCompanyTerms) {
		t.Fatalf("double acceptance accepted: %v", err)
	}
	drifted := terms
	drifted.Residency = "EU"
	if accepted.Current(at, drifted) {
		t.Fatal("grant admitted after residency drift")
	}
	if !accepted.Current(at, terms) {
		t.Fatal("unmodified terms no longer current")
	}
	if err := s.RevokeCrossCompanyGrant(ctx, "tenant-a", proposal.ID, "u-owner", at); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeCrossCompanyGrant(ctx, "tenant-a", proposal.ID, "u-owner", at); !errors.Is(err, ErrDenied) {
		t.Fatalf("double revoke accepted: %v", err)
	}
}
