package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3778Repo adds the policy a records service keeps to the memory repository.
type issue3778Repo struct {
	*chatrecords.MemoryRepository
	mu     sync.Mutex
	policy *chatrecords.RetentionPolicy
}

func (r *issue3778Repo) GetRetentionPolicy(_ context.Context, tenant, kind string) (chatrecords.RetentionPolicy, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policy == nil || r.policy.TenantID != tenant || r.policy.Kind != kind {
		return chatrecords.RetentionPolicy{}, false, nil
	}
	return *r.policy, true, nil
}

func (r *issue3778Repo) PutRetentionPolicy(_ context.Context, p chatrecords.RetentionPolicy, _ uint64) (chatrecords.RetentionPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy = &p
	return p, nil
}

func issue3778Extensions(t *testing.T, role string) (*ChatExtensions, context.Context) {
	t.Helper()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "person", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
	if err != nil {
		t.Fatal(err)
	}
	repo := &issue3778Repo{MemoryRepository: chatrecords.NewMemoryRepository()}
	records := &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}
	ctx := trust.WithPrincipal(context.Background(), principal)
	old := at.AddDate(0, 0, -40)
	for _, r := range []chatrecords.Record{
		{TenantID: "host", ConversationID: "general", RecordID: "post:1", Kind: chatrecords.KindPost, Revision: 1, CreatedAt: old},
		{TenantID: "host", ConversationID: "general", RecordID: "file:1", Kind: chatrecords.KindFile, Revision: 1, CreatedAt: old, HoldIDs: []string{"h1"}},
		{TenantID: "host", ConversationID: "general", RecordID: "post:2", Kind: chatrecords.KindPost, Revision: 1, CreatedAt: at},
		{TenantID: "other", ConversationID: "x", RecordID: "post:9", Kind: chatrecords.KindPost, Revision: 1, CreatedAt: old},
	} {
		if err := repo.Append(ctx, r, chatrecords.AuditEvent{TenantID: r.TenantID, EventID: "e-" + r.RecordID, Action: "x"}, chatrecords.OutboxEvent{ID: "e-" + r.RecordID, EventID: "e-" + r.RecordID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.PutHold(ctx, chatrecords.Hold{TenantID: "host", HoldID: "h1", Reason: "matter", PlacedBy: "legal"}); err != nil {
		t.Fatal(err)
	}
	repo.policy = &chatrecords.RetentionPolicy{TenantID: "host", Mode: chatrecords.RetentionAge, AgeDays: 30, Revision: 1}
	return &ChatExtensions{Records: records, Grants: NewChatCompanyGrants(chatAdminFacts{role: role}, chatauthority.New(nil))}, ctx
}

func TestIssue3778_AdminPreviewsRetentionAndNothingChanges_Integration(t *testing.T) {
	x, ctx := issue3778Extensions(t, "hcm_admin")
	p := chatcore.Principal{TenantID: "host", SubjectID: "person"}
	report, err := x.PreviewRetention(ctx, p)
	if err != nil || !report.PolicyConfigured || report.WouldRemoveTotal != 1 || report.HeldSkipped != 1 || report.Scanned != 3 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if len(report.WouldRemove) != 1 || report.WouldRemove[0].Kind != chatrecords.KindPost {
		t.Fatalf("counts by kind: %+v", report.WouldRemove)
	}
	records, _ := x.Records.Repo.List(ctx, "host")
	events, _ := x.Records.Repo.Events(ctx, "host")
	if len(records) != 3 || len(events) != 3 {
		t.Fatalf("a preview wrote: records=%d events=%d", len(records), len(events))
	}
}

func TestIssue3778_NonAdminAndOtherTenantRefusedPreview_Security(t *testing.T) {
	x, ctx := issue3778Extensions(t, "employee")
	if _, err := x.PreviewRetention(ctx, chatcore.Principal{TenantID: "host", SubjectID: "person"}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("an employee previewed retention: %v", err)
	}
	admin, actx := issue3778Extensions(t, "hcm_admin")
	if _, err := admin.PreviewRetention(actx, chatcore.Principal{TenantID: "elsewhere", SubjectID: "person"}); err == nil {
		t.Fatal("a principal naming another tenant previewed retention")
	}
	if _, err := admin.PreviewRetention(actx, chatcore.Principal{TenantID: "host", SubjectID: "someone-else"}); err == nil {
		t.Fatal("a principal that is not the verified caller previewed retention")
	}
	var none *ChatExtensions
	if _, err := none.PreviewRetention(actx, chatcore.Principal{TenantID: "host", SubjectID: "person"}); err == nil {
		t.Fatal("unavailable service answered")
	}
}

func TestIssue3778_PreviewHTTPAnswersCountsOnly(t *testing.T) {
	x, ctx := issue3778Extensions(t, "hcm_admin")
	ctx = transport.WithInvocation(ctx, &transport.Invocation{})
	h := ChatRetentionPreviewHTTP{Service: x}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatRetentionPreviewPath, nil).WithContext(ctx))
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(body, `"total":1`) || !strings.Contains(body, `"held_skipped":1`) || !strings.Contains(body, `"kind":"POST"`) {
		t.Fatalf("preview: %d %s", w.Code, body)
	}
	for _, leak := range []string{"post:1", "file:1", "general", "h1"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the preview named %q: %s", leak, body)
		}
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, ChatRetentionPreviewPath, nil).WithContext(ctx))
	if get.Code != 405 {
		t.Fatalf("GET: %d", get.Code)
	}
	noCaller := httptest.NewRecorder()
	h.ServeHTTP(noCaller, httptest.NewRequest(http.MethodPost, ChatRetentionPreviewPath, nil))
	if noCaller.Code != 403 {
		t.Fatalf("no verified caller: %d", noCaller.Code)
	}
	employee, ectx := issue3778Extensions(t, "employee")
	ectx = transport.WithInvocation(ectx, &transport.Invocation{})
	denied := httptest.NewRecorder()
	ChatRetentionPreviewHTTP{Service: employee}.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, ChatRetentionPreviewPath, nil).WithContext(ectx))
	if denied.Code != 403 || strings.Contains(denied.Body.String(), "total") {
		t.Fatalf("employee: %d %s", denied.Code, denied.Body)
	}
}
