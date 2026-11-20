package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func issue3780Extensions(t *testing.T, role string) (*ChatExtensions, context.Context) {
	t.Helper()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "person", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
	if err != nil {
		t.Fatal(err)
	}
	records := &chatrecords.Service{Repo: chatrecords.NewMemoryRepository(), Auth: ChatRecordAuthority{}}
	ctx := trust.WithPrincipal(context.Background(), principal)
	// Governed writes arrive through the live conversation decision.
	write := func(actor, record, action, reason string) {
		wctx := context.WithValue(ctx, recordAuthorizationKey{}, recordAuthorization{tenant: "host", actor: actor, conversation: "general", revision: 1})
		if _, err := records.AppendRecord(wctx, actor, chatrecords.Record{TenantID: "host", ConversationID: "general", RecordID: record, Kind: chatrecords.KindPost, Revision: 1}, action, reason); err != nil {
			t.Fatal(err)
		}
	}
	write("ana", "post:1", "chat.post.send", "chat.post.send")
	write("bob", "post:2", "chat.post.delete", "chat.post.delete")
	return &ChatExtensions{Records: records, Grants: NewChatCompanyGrants(chatAdminFacts{role: role}, chatauthority.New(nil))}, ctx
}

func TestIssue3780_AdminReadsAuditLog_Integration(t *testing.T) {
	x, ctx := issue3780Extensions(t, "hcm_admin")
	p := chatcore.Principal{TenantID: "host", SubjectID: "person"}
	page, err := x.ListAudit(ctx, p, chatrecords.AuditFilter{}, "", 10)
	if err != nil || len(page.Entries) != 2 || page.Entries[0].Action != "chat.post.delete" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	body, n, err := x.ExportAudit(ctx, p, chatrecords.AuditFilter{ActorID: "ana"})
	if err != nil || n != 1 || !strings.Contains(string(body), "chat.post.send") || strings.Contains(string(body), "chat.post.delete") {
		t.Fatalf("export n=%d err=%v body=%s", n, err, body)
	}
	exported, err := x.ListAudit(ctx, p, chatrecords.AuditFilter{Action: "audit.export"}, "", 10)
	if err != nil || len(exported.Entries) != 1 || exported.Entries[0].ActorID != "person" {
		t.Fatalf("the export must appear in the log: %+v err=%v", exported, err)
	}
}

func TestIssue3780_NonAdminAndOtherTenantRefused_Security(t *testing.T) {
	x, ctx := issue3780Extensions(t, "employee")
	p := chatcore.Principal{TenantID: "host", SubjectID: "person"}
	if _, err := x.ListAudit(ctx, p, chatrecords.AuditFilter{}, "", 10); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("an employee read the log: %v", err)
	}
	if body, _, err := x.ExportAudit(ctx, p, chatrecords.AuditFilter{}); err == nil || len(body) != 0 {
		t.Fatalf("an employee exported the log: %v", err)
	}
	admin, actx := issue3780Extensions(t, "hcm_admin")
	if _, err := admin.ListAudit(actx, chatcore.Principal{TenantID: "elsewhere", SubjectID: "person"}, chatrecords.AuditFilter{}, "", 10); err == nil {
		t.Fatal("a principal naming another tenant read the log")
	}
	if _, err := admin.ListAudit(actx, chatcore.Principal{TenantID: "host", SubjectID: "someone-else"}, chatrecords.AuditFilter{}, "", 10); err == nil {
		t.Fatal("a principal that is not the verified caller read the log")
	}
	var none *ChatExtensions
	if _, err := none.ListAudit(actx, p, chatrecords.AuditFilter{}, "", 10); err == nil {
		t.Fatal("unavailable service answered")
	}
}

func issue3780Post(t *testing.T, h http.Handler, ctx context.Context, action string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, ChatAuditPath+action, bytes.NewReader(raw))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func TestIssue3780_AuditHTTP(t *testing.T) {
	x, ctx := issue3780Extensions(t, "hcm_admin")
	ctx = transport.WithInvocation(ctx, &transport.Invocation{})
	h := ChatAuditHTTP{Service: x}

	w := issue3780Post(t, h, ctx, "list", map[string]any{"Actor": "bob", "Limit": 5})
	var page chatrecords.AuditPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Entries) != 1 || page.Entries[0].ActorID != "bob" {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "policy") || strings.Contains(w.Body.String(), "digest") {
		t.Fatalf("internal evidence leaked: %s", w.Body)
	}
	if got := issue3780Post(t, h, ctx, "list", map[string]any{"FromDate": "2024-02-30"}); got.Code != 400 {
		t.Fatalf("bad date: %d", got.Code)
	}
	if got := issue3780Post(t, h, ctx, "list", map[string]any{"FromDate": "2025-02-02", "ToDate": "2025-01-01"}); got.Code != 400 {
		t.Fatalf("inverted dates: %d", got.Code)
	}
	if got := issue3780Post(t, h, ctx, "list", map[string]any{"Cursor": "junk"}); got.Code != 400 {
		t.Fatalf("junk cursor: %d", got.Code)
	}
	if got := issue3780Post(t, h, ctx, "list", map[string]any{"Surprise": 1}); got.Code != 400 {
		t.Fatalf("unknown field: %d", got.Code)
	}
	w = issue3780Post(t, h, ctx, "export", map[string]any{})
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("export: %d %v", w.Code, w.Header())
	}
	if got := issue3780Post(t, h, ctx, "nope", map[string]any{}); got.Code != 404 {
		t.Fatalf("unknown action: %d", got.Code)
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, ChatAuditPath+"list", nil).WithContext(ctx))
	if get.Code != 405 {
		t.Fatalf("GET: %d", get.Code)
	}
	noCaller := httptest.NewRecorder()
	h.ServeHTTP(noCaller, httptest.NewRequest(http.MethodPost, ChatAuditPath+"list", strings.NewReader("{}")))
	if noCaller.Code != 403 && noCaller.Code != 401 {
		t.Fatalf("no verified caller: %d", noCaller.Code)
	}

	employee, ectx := issue3780Extensions(t, "employee")
	ectx = transport.WithInvocation(ectx, &transport.Invocation{})
	for _, action := range []string{"list", "export"} {
		if got := issue3780Post(t, ChatAuditHTTP{Service: employee}, ectx, action, map[string]any{}); got.Code != 403 || strings.Contains(got.Body.String(), "ana") {
			t.Fatalf("%s as employee: %d %s", action, got.Code, got.Body)
		}
	}
}

func TestIssue3780_OverlayAdmitsBeforeServing_Security(t *testing.T) {
	x, _ := issue3780Extensions(t, "hcm_admin")
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached = true; w.WriteHeader(http.StatusTeapot) })
	h := OverlayChatAudit(next, x, transport.Config{})

	other := httptest.NewRecorder()
	h.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/api/chat/other", nil))
	if !reached || other.Code != http.StatusTeapot {
		t.Fatalf("a path outside the audit log must pass through: reached=%v code=%d", reached, other.Code)
	}

	for _, action := range []string{"list", "export"} {
		reached = false
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatAuditPath+action, strings.NewReader("{}")))
		if reached || w.Code < 400 || strings.Contains(w.Body.String(), "ana") || strings.Contains(w.Body.String(), "chat.post") {
			t.Fatalf("%s without a verified caller: reached=%v code=%d body=%s", action, reached, w.Code, w.Body)
		}
	}
}
