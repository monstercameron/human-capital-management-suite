package chatextensions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
)

type stubService struct{ Service }
type fullService struct{ Service }

func (fullService) Install(_ context.Context, p chatcore.Principal, c string, m chatapps.Manifest, _ []string) (chatapps.Installation, error) {
	return chatapps.Installation{ID: "install", Tenant: p.TenantID, Conversation: c, AppID: m.AppID}, nil
}
func (fullService) ListInstallations(context.Context, chatcore.Principal, string) ([]chatapps.Installation, error) {
	return []chatapps.Installation{{ID: "install"}}, nil
}
func (fullService) ChangeStatus(_ context.Context, _ chatcore.Principal, _, id string, st chatapps.Status) (chatapps.Installation, error) {
	return chatapps.Installation{ID: id, Status: st}, nil
}
func (fullService) Invoke(context.Context, chatcore.Principal, string, string, chatapps.Callback) (chatapps.CallbackResult, error) {
	return chatapps.CallbackResult{Accepted: true}, nil
}
func (fullService) Agent(context.Context, chatcore.Principal, string, string) (chatapps.Agent, error) {
	return chatapps.Agent{ID: "agent"}, nil
}
func (fullService) ProposeIntent(context.Context, chatcore.Principal, string, chatapps.Proposal) (chatapps.ProposalReceipt, error) {
	return chatapps.ProposalReceipt{IntentID: "intent"}, nil
}
func (fullService) Report(context.Context, chatcore.Principal, chatrecords.Report) error { return nil }
func (fullService) Moderate(context.Context, chatcore.Principal, string, string, string, string, string, string) error {
	return nil
}
func (fullService) IssueEventCursor(context.Context, chatcore.Principal, string, string, int64) (string, error) {
	return "cursor", nil
}
func (fullService) PullEvents(context.Context, chatcore.Principal, string, string, int) ([]chatapps.Event, string, error) {
	return []chatapps.Event{{ID: "event"}}, "next", nil
}
func (fullService) ProposeGrant(context.Context, string, string, string, string, string, time.Time) (string, error) {
	return "grant", nil
}
func (fullService) AcceptGrant(context.Context, string, string, string, string) error { return nil }
func (fullService) RevokeGrant(context.Context, string, string, string, string) error { return nil }
func (fullService) SetChannelPolicy(context.Context, string, string, []string, []string, []string, []string, chatpolicy.RoleMode, string, string, uint64) error {
	return nil
}

type recipientService struct {
	Service
	follow  chatrecipient.Follow
	sidebar chatrecipient.Sidebar
	quiet   chatrecipient.QuietHours
	got     chatcore.Principal
	host    string
}

func (s *recipientService) Sidebar(_ context.Context, p chatcore.Principal) (chatrecipient.Sidebar, error) {
	s.got = p
	return s.sidebar, nil
}
func (s *recipientService) PutSidebar(_ context.Context, p chatcore.Principal, x chatrecipient.Sidebar, expected uint64) (chatrecipient.Sidebar, error) {
	s.got = p
	if expected != 1 {
		return chatrecipient.Sidebar{}, chatcore.ErrConflict
	}
	x.Revision = 2
	s.sidebar = x
	return x, nil
}
func (s *recipientService) QuietHours(_ context.Context, p chatcore.Principal) (chatrecipient.QuietHours, error) {
	s.got = p
	return s.quiet, nil
}
func (s *recipientService) PutQuietHours(_ context.Context, p chatcore.Principal, x chatrecipient.QuietHours, expected uint64) (chatrecipient.QuietHours, error) {
	s.got = p
	if expected != 1 {
		return chatrecipient.QuietHours{}, chatcore.ErrConflict
	}
	x.Revision = 2
	s.quiet = x
	return x, nil
}

func (s *recipientService) Counts(_ context.Context, p chatcore.Principal, host, conversation string) (chatrecipient.Counts, error) {
	s.got = p
	s.host = host
	if conversation != "conv" {
		return chatrecipient.Counts{}, chatcore.ErrNotFound
	}
	return chatrecipient.Counts{Unread: 3, Mentions: 1}, nil
}
func (s *recipientService) PutThreadFollow(_ context.Context, p chatcore.Principal, host, conversation string, f chatrecipient.Follow, expected uint64) (chatrecipient.Follow, error) {
	s.got = p
	s.host = host
	if expected != 1 || conversation != "conv" {
		return chatrecipient.Follow{}, chatcore.ErrConflict
	}
	f.Revision = 2
	s.follow = f
	return f, nil
}
func (s *recipientService) ThreadFollow(_ context.Context, p chatcore.Principal, host, conversation, root string) (chatrecipient.Follow, error) {
	s.got = p
	s.host = host
	if conversation != "conv" || root != s.follow.RootPostID {
		return chatrecipient.Follow{}, chatcore.ErrNotFound
	}
	return s.follow, nil
}

func grantContext(t *testing.T, tenant string) context.Context {
	t.Helper()
	now := time.Now()
	p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if e != nil {
		t.Fatal(e)
	}
	return transport.WithInvocation(trust.WithPrincipal(context.Background(), p), &transport.Invocation{})
}

func TestTodo_CHAT_040_TransportRequiresAuthenticatedContext(t *testing.T) {
	s := &server{deps: Dependencies{Service: stubService{}}}
	_, err := s.ListApps(context.Background(), &chatv1.ListAppsRequest{ConversationId: "conv"})
	if err == nil {
		t.Fatal("untrusted request accepted")
	}
	if e, ok := envelope.As(err); !ok || e.Code() != envelope.CodeUnauthenticated {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestTodo_CHAT_012_ExtensionRoutesRegistered(t *testing.T) {
	g := grpc.NewServer()
	Register(g, Dependencies{Service: stubService{}})
	if _, ok := g.GetServiceInfo()["hcmnext.chat.v1.ChatExtensionsService"]; !ok {
		t.Fatal("gRPC extension service missing")
	}
	h := NewHandler(Dependencies{Service: stubService{}})
	r := httptest.NewRequest(http.MethodPost, chatv1.ChatExtensionsService_ProposeCompanyGrant_FullMethodName, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusNotFound {
		t.Fatal("HTTP extension route missing")
	}
}
func TestTodo_CHAT_024_RecipientRPCBindsTrustedIdentityAndRoundTrips(t *testing.T) {
	svc := &recipientService{}
	s := &server{deps: Dependencies{Service: svc}}
	counts := &chatv1.GetCountsRequest{TenantId: "host", ConversationId: "conv"}
	if _, e := s.GetCounts(context.Background(), counts); e == nil {
		t.Fatal("anonymous recipient read accepted")
	}
	ctx := grantContext(t, "home")
	v, e := s.GetCounts(ctx, counts)
	if e != nil || v.GetCounts().GetUnreadCount() != 3 || v.GetCounts().GetSubjectId() != "admin" || v.GetCounts().GetHomeTenantId() != "home" || svc.host != "host" {
		t.Fatalf("counts identity: %#v %v", v, e)
	}
	put := &chatv1.PutThreadFollowRequest{Follow: &chatv1.ThreadFollow{TenantId: "host", ConversationId: "conv", SubjectId: "forged", HomeTenantId: "forged", RootPostId: "root", Followed: true}, ExpectedRevision: 1}
	w, e := s.PutThreadFollow(ctx, put)
	if e != nil || !w.GetFollow().GetFollowed() || w.GetFollow().GetSubjectId() != "admin" {
		t.Fatalf("put follow: %#v %v", w, e)
	}
	x, e := s.GetThreadFollow(ctx, &chatv1.GetThreadFollowRequest{TenantId: "host", ConversationId: "conv", RootPostId: "root"})
	if e != nil || x.GetFollow().GetRevision() != 2 || !x.GetFollow().GetFollowed() {
		t.Fatalf("follow roundtrip: %#v %v", x, e)
	}
	side, e := s.PutSidebar(ctx, &chatv1.PutSidebarRequest{Sidebar: &chatv1.SidebarState{TenantId: "forged", SubjectId: "forged", HomeTenantId: "forged", LayoutJson: `{"sections":[]}`}, ExpectedRevision: 1})
	if e != nil || side.GetSidebar().GetSubjectId() != "admin" || side.GetSidebar().GetRevision() != 2 {
		t.Fatalf("sidebar write: %#v %v", side, e)
	}
	sideRead, e := s.GetSidebar(ctx, &chatv1.GetSidebarRequest{})
	if e != nil || sideRead.GetSidebar().GetLayoutJson() != `{"sections":[]}` || sideRead.GetSidebar().GetHomeTenantId() != "home" {
		t.Fatalf("sidebar read: %#v %v", sideRead, e)
	}
	quiet, e := s.PutQuietHours(ctx, &chatv1.PutQuietHoursRequest{QuietHours: &chatv1.QuietHours{TenantId: "forged", SubjectId: "forged", HomeTenantId: "forged", Timezone: "UTC", StartMinute: 60, EndMinute: 120, Enabled: true}, ExpectedRevision: 1})
	if e != nil || quiet.GetQuietHours().GetSubjectId() != "admin" || quiet.GetQuietHours().GetRevision() != 2 {
		t.Fatalf("quiet write: %#v %v", quiet, e)
	}
	quietRead, e := s.GetQuietHours(ctx, &chatv1.GetQuietHoursRequest{})
	if e != nil || quietRead.GetQuietHours().GetStartMinute() != 60 || quietRead.GetQuietHours().GetHomeTenantId() != "home" {
		t.Fatalf("quiet read: %#v %v", quietRead, e)
	}
}
func TestTodo_CHAT_040_ExtensionMethodProjection(t *testing.T) {
	s := &server{deps: Dependencies{Service: fullService{}}}
	ctx := grantContext(t, "tenant")
	if v, e := s.InstallApp(ctx, &chatv1.InstallAppRequest{ConversationId: "conv", ManifestJson: []byte(`{"app_id":"app","version":1}`)}); e != nil || !strings.Contains(string(v.GetJson()), "install") {
		t.Fatalf("install: %#v %v", v, e)
	}
	if v, e := s.ListApps(ctx, &chatv1.ListAppsRequest{ConversationId: "conv"}); e != nil || !strings.Contains(string(v.GetJson()), "install") {
		t.Fatalf("list: %#v %v", v, e)
	}
	if v, e := s.ChangeAppStatus(ctx, &chatv1.ChangeAppStatusRequest{ConversationId: "conv", InstallationId: "install", Status: "SUSPENDED"}); e != nil || !strings.Contains(string(v.GetJson()), "SUSPENDED") {
		t.Fatalf("status: %#v %v", v, e)
	}
	if v, e := s.InvokeApp(ctx, &chatv1.InvokeAppRequest{ConversationId: "conv", InstallationId: "install", Command: "run", IdempotencyKey: "k"}); e != nil || !strings.Contains(string(v.GetJson()), "true") {
		t.Fatalf("invoke: %#v %v", v, e)
	}
	if v, e := s.GetAgent(ctx, &chatv1.GetAgentRequest{ConversationId: "conv", InstallationId: "install"}); e != nil || !strings.Contains(string(v.GetJson()), "agent") {
		t.Fatalf("agent: %#v %v", v, e)
	}
	if v, e := s.ProposeAgentIntent(ctx, &chatv1.ProposeAgentIntentRequest{ConversationId: "conv", InstallationId: "install", IntentType: "type", IdempotencyKey: "k"}); e != nil || !strings.Contains(string(v.GetJson()), "intent") {
		t.Fatalf("intent: %#v %v", v, e)
	}
	if v, e := s.ReportAbuse(ctx, &chatv1.ReportAbuseRequest{ConversationId: "conv", ReportId: "report", TargetId: "post", Reason: "spam"}); e != nil || !strings.Contains(string(v.GetJson()), "true") {
		t.Fatalf("report: %#v %v", v, e)
	}
	if v, e := s.ModerateAbuse(ctx, &chatv1.ModerateAbuseRequest{ConversationId: "conv", CaseId: "case", TargetId: "post", Action: "block", Reason: "spam", EvidenceRef: "ref"}); e != nil || !strings.Contains(string(v.GetJson()), "true") {
		t.Fatalf("moderate: %#v %v", v, e)
	}
	if v, e := s.IssueEventCursor(ctx, &chatv1.IssueEventCursorRequest{ConversationId: "conv"}); e != nil || !strings.Contains(string(v.GetJson()), "cursor") {
		t.Fatalf("cursor: %#v %v", v, e)
	}
	if v, e := s.PullAppEvents(ctx, &chatv1.PullAppEventsRequest{ConversationId: "conv", Cursor: "cursor"}); e != nil || !strings.Contains(string(v.GetJson()), "event") {
		t.Fatalf("pull: %#v %v", v, e)
	}
	if v, e := s.ProposeCompanyGrant(ctx, &chatv1.ProposeCompanyGrantRequest{HostTenantId: "tenant", ConsumerTenantId: "other", ConversationId: "conv", ExpiresAtUnix: time.Now().Add(time.Hour).Unix()}); e != nil || v.GetGrantId() != "grant" {
		t.Fatalf("grant: %#v %v", v, e)
	}
	// Consent is the consumer's to give, so this one is called as "other".
	if v, e := s.AcceptCompanyGrant(grantContext(t, "other"), &chatv1.AcceptCompanyGrantRequest{HostTenantId: "tenant", ConsumerTenantId: "other", ConversationId: "conv", GrantId: "grant"}); e != nil || !v.GetAccepted() {
		t.Fatalf("accept: %#v %v", v, e)
	}
	if v, e := s.RevokeCompanyGrant(ctx, &chatv1.RevokeCompanyGrantRequest{HostTenantId: "tenant", ConsumerTenantId: "other", ConversationId: "conv", GrantId: "grant"}); e != nil || !v.GetRevoked() {
		t.Fatalf("revoke: %#v %v", v, e)
	}
	if v, e := s.SetChannelPolicy(ctx, &chatv1.SetChannelPolicyRequest{HostTenantId: "tenant", ConversationId: "conv", ExpectedRevision: 1}); e != nil || !v.GetUpdated() {
		t.Fatalf("policy: %#v %v", v, e)
	}
}
