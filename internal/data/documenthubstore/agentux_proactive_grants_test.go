package documenthubstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAgentUXProactive_ServiceDocumentGrants_Security_Integration(t *testing.T) {
	hub, _ := documentFixture(t)
	ctx := context.Background()
	const tenant = "proactive-grants"
	first, err := hub.CreateDocument(ctx, tenant, "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hub.CreateDocument(ctx, tenant, "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := hub.CreateDocument(ctx, "another-tenant", "owner", "COMPANY")
	if err != nil {
		t.Fatal(err)
	}
	if err = hub.SetAnnouncementDocuments(ctx, tenant, "outsider", "service", "ann", []string{first}); !errors.Is(err, ErrDenied) {
		t.Fatalf("unshareable document granted: %v", err)
	}
	if err = hub.SetAnnouncementDocuments(ctx, tenant, "owner", "service", "ann", []string{foreign}); err == nil {
		t.Fatal("foreign document granted")
	}
	for range 2 {
		if err = hub.SetAnnouncementDocuments(ctx, tenant, "owner", "service", "ann", []string{first, second}); err != nil {
			t.Fatal(err)
		}
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, first, "service", "ann"); err != nil {
		t.Fatal(err)
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, first, "service", "other"); !errors.Is(err, ErrDenied) {
		t.Fatalf("other announcement borrowed grant: %v", err)
	}
	if err = hub.SetAnnouncementDocuments(ctx, tenant, "owner", "service", "other", []string{first}); err != nil {
		t.Fatal(err)
	}
	if err = hub.SetAnnouncementDocuments(ctx, tenant, "owner", "service", "ann", []string{second}); err != nil {
		t.Fatal(err)
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, first, "service", "ann"); !errors.Is(err, ErrDenied) {
		t.Fatalf("removed document still authorized: %v", err)
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, first, "service", "other"); err != nil {
		t.Fatalf("one announcement revoked another's grant: %v", err)
	}
	if err = hub.SetAnnouncementDocuments(ctx, tenant, "owner", "service", "ann", nil); err != nil {
		t.Fatal(err)
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, second, "service", "ann"); !errors.Is(err, ErrDenied) {
		t.Fatalf("deleted announcement still authorized: %v", err)
	}
	version, err := hub.SubmitCandidate(ctx, tenant, Version{DocumentID: first, CreatorID: "owner", Title: "Holidays", Markdown: "Upcoming holidays", Classification: "INTERNAL"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = hub.RecordReview(ctx, tenant, ReviewInput{DocumentID: first, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ReviewerID: "reviewer", Authority: "policy-owner", Decision: ReviewApproved})
	if err != nil {
		t.Fatal(err)
	}
	_, err = hub.PlaceDocument(ctx, tenant, PlaceInput{DocumentID: first, VersionID: version.ID, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", CustodianID: "owner", ReviewDueAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err = hub.SetAnnouncementDocuments(ctx, tenant, "owner", "service", "placed", []string{first}, "general"); err != nil {
		t.Fatal(err)
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, first, "service", "placed"); err != nil {
		t.Fatal(err)
	}
	_, err = hub.Withdraw(ctx, tenant, WithdrawInput{DocumentID: first, ScopeKind: "placement", ScopeID: "general", ActorID: "owner", ExpectedLive: version.ID, Reason: "Withdrawn"})
	if err != nil {
		t.Fatal(err)
	}
	if err = hub.AuthorizeAnnouncementDocument(ctx, tenant, first, "service", "placed"); !errors.Is(err, ErrDenied) {
		t.Fatalf("withdrawn placement still authorized: %v", err)
	}
}
