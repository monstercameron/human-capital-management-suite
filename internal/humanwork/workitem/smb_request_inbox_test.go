package workitem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func smbRequestFixture() (EmployeeRequest, uuid.UUID, uuid.UUID) {
	tenant := uuid.New()
	requester := uuid.New()
	owner := uuid.New()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	return EmployeeRequest{
		TenantID: tenant, RequesterID: requester.String(), OwnerID: owner.String(), Type: RequestTypeIT,
		Subject: "VPN access", Details: "The new laptop cannot connect.", DueAt: now.Add(48 * time.Hour), CreatedAt: now,
	}, tenant, requester
}

func TestTodo_SMB_001(t *testing.T) {
	in, tenant, requester := smbRequestFixture()
	owner := in.OwnerID
	inbox := &RequestInbox{}
	created, err := inbox.Create(context.Background(), in, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != RequestStatusOpen || created.OwnerID != owner || created.RequesterID != requester.String() || created.RequestID == uuid.Nil {
		t.Fatalf("created request lost tracking fields: %+v", created)
	}
	if _, err := inbox.Reply(context.Background(), tenant, created.RequestID, owner, "IT is reviewing this.", in.CreatedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	updated, err := inbox.UpdateStatus(context.Background(), tenant, created.RequestID, owner, RequestStatusInProgress)
	if err != nil || updated.Status != RequestStatusInProgress {
		t.Fatalf("status update = %+v, err=%v", updated, err)
	}
	link := AuthorizedDocumentLink{TenantID: tenant, DocumentVersionID: "doc-v7", DocumentDigest: "sha256:doc-v7", AuthorizedViewerIDs: []string{requester.String(), owner}}
	if _, err := inbox.LinkDocument(context.Background(), tenant, created.RequestID, owner, link); err != nil {
		t.Fatal(err)
	}
	view := inbox.View(tenant, requester.String())
	if len(view) != 1 || view[0].Status != RequestStatusInProgress || len(view[0].Replies) != 1 || len(view[0].Documents) != 1 {
		t.Fatalf("requester projection = %+v", view)
	}
}

func TestTodo_SMB_001_Integration(t *testing.T) {
	in, tenant, requester := smbRequestFixture()
	inbox := &RequestInbox{}
	first, err := inbox.Create(context.Background(), in, "same-retry")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := inbox.Create(context.Background(), in, "same-retry")
	if err != nil || retry.RequestID != first.RequestID {
		t.Fatalf("idempotent retry = %+v, err=%v", retry, err)
	}
	in.RequestID = uuid.Nil
	in.Type = RequestTypeTeam
	second, err := inbox.Create(context.Background(), in, "second")
	if err != nil || second.RequestID == first.RequestID {
		t.Fatalf("independent request = %+v, err=%v", second, err)
	}
	if got := inbox.View(tenant, requester.String()); len(got) != 2 {
		t.Fatalf("requester should see both tracked requests, got %d", len(got))
	}
}

func TestTodo_SMB_001_Security(t *testing.T) {
	in, tenant, requester := smbRequestFixture()
	inbox := &RequestInbox{}
	created, err := inbox.Create(context.Background(), in, "secure")
	if err != nil {
		t.Fatal(err)
	}
	foreignTenant := uuid.New()
	if got := inbox.View(foreignTenant, requester.String()); len(got) != 0 {
		t.Fatalf("cross-tenant view disclosed %d requests", len(got))
	}
	if _, err := inbox.Reply(context.Background(), foreignTenant, created.RequestID, requester.String(), "leak", in.CreatedAt); !errors.Is(err, ErrWorkItem) {
		t.Fatalf("cross-tenant reply err=%v, want workitem refusal", err)
	}
	in.Details = "different details"
	if _, err := inbox.Create(context.Background(), in, "secure"); !errors.Is(err, ErrWorkItem) {
		t.Fatalf("mismatched idempotency retry err=%v, want workitem refusal", err)
	}
	badLink := AuthorizedDocumentLink{TenantID: tenant, DocumentVersionID: "private-v1", DocumentDigest: "digest", AuthorizedViewerIDs: []string{"other"}}
	if _, err := inbox.LinkDocument(context.Background(), tenant, created.RequestID, "other", badLink); !errors.Is(err, ErrWorkItem) {
		t.Fatalf("non-participant link err=%v, want workitem refusal", err)
	}
	in.Type = RequestTypeConfidentialHR
	in.OwnerID = requester.String()
	if _, err := inbox.Create(context.Background(), in, "confidential-invalid"); !errors.Is(err, ErrWorkItem) {
		t.Fatalf("self-owned confidential HR request err=%v, want refusal", err)
	}
}
