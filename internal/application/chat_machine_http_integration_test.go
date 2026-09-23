package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatappstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatrecordstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/chatresource"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_CHAT_043_Integration_InstalledAgentHTTP(t *testing.T) {
	store := streamIntegrationStore(t)
	clock := func() time.Time { return time.Now().UTC() }
	ctx := context.Background()
	appRepo := chatappstore.NewChatStore(store.Store)
	service := chatcore.NewService(store, clock)
	service.SetAuthority(newChatCurrentAuthority(chatAdminFacts{role: "employee"}, store, chatauthority.New(store.Store), newChatAuthorityCache(time.Minute, clock), appRepo))
	records := &chatrecords.Service{Repo: chatrecords.NewMemoryRepository(), Auth: ChatRecordAuthority{}}
	audited := &auditedChatService{ConversationService: service, records: records, atomicCore: true}
	apps := &chatapps.Service{Repo: appRepo, Authority: ChatAppAuthority{Conversations: audited}, Now: clock}
	extensions := &ChatExtensions{Conversations: audited, Apps: apps, Records: records}
	reader := chatServiceReader{service: audited, membership: store, events: store}
	runtime, err := NewChatStreamRuntime(ChatStreamRuntimeConfig{CursorKey: "machine-http-integration-cursor-key", Reader: reader, Authorizer: chatServiceStreamAuthorizer{service: audited, membership: store}, PollInterval: 10 * time.Millisecond, PageLimit: 20, QueueSize: 32, ReplayLimit: 20, CursorTTL: time.Minute, Budgets: defaultChatAdmissionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	composed := &streamingChatService{ConversationService: audited, runtime: runtime, membership: store}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(strings.Repeat("k", 32)), Issuer: "chat-test", Audience: "chat-test", Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	h := chatresource.NewHandler(transport.Config{Verifier: verifier}, composed)
	human := func(subject string) context.Context {
		t.Helper()
		at := clock()
		p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: subject + "-session", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: subject + "-digest"})
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(ctx, p)
	}
	token := func(tenant, subject string) string {
		t.Helper()
		at := clock()
		value, err := verifier.Issue(trust.Claims{Issuer: "chat-test", Audience: "chat-test", Subject: subject, SubjectKind: "agent", Tenant: tenant, Purposes: []string{"chat_integration"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: subject + "-session", IssuedAtUnix: at.Add(-time.Minute).Unix(), ExpiresAtUnix: at.Add(15 * time.Minute).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	requestPath := func(method, path, bearer, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	request := func(method, conversation, bearer, body, key string) *httptest.ResponseRecorder {
		return requestPath(method, "/v1/conversations/"+conversation+"/posts?page_size=10", bearer, body, key)
	}
	for _, kind := range []chatcore.ConversationKind{chatcore.PublicChannel, chatcore.PrivateChannel, chatcore.Group, chatcore.Direct} {
		t.Run(string(kind), func(t *testing.T) {
			id := strings.ToLower(string(kind))
			c := chatcore.Conversation{ID: id, TenantID: "tenant-a", Kind: kind, Name: id, OwnerID: "owner", Revision: 1}
			members := []chatcore.Membership{
				{ConversationID: id, TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "owner", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
				{ConversationID: id, TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "manager", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
				{ConversationID: id, TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "member", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
			}
			if _, err := store.CreateConversation(ctx, c, members, "create-"+id); err != nil {
				t.Fatal(err)
			}
			uninstalled := c
			uninstalled.ID = "uninstalled-" + id
			if _, err := store.CreateConversation(ctx, uninstalled, []chatcore.Membership{{ConversationID: uninstalled.ID, TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "owner", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory}}, "create-uninstalled-"+id); err != nil {
				t.Fatal(err)
			}
			otherTenant := c
			otherTenant.ID = "other-tenant-" + id
			otherTenant.TenantID = "tenant-b"
			if _, err := store.CreateConversation(ctx, otherTenant, []chatcore.Membership{{ConversationID: otherTenant.ID, TenantID: "tenant-b", HomeTenantID: "tenant-b", SubjectID: "owner", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory}}, "create-other-tenant-"+id); err != nil {
				t.Fatal(err)
			}
			p := chatcore.Principal{TenantID: "tenant-a", SubjectID: "manager"}
			manifest := chatapps.Manifest{AppID: "agent-a", Version: 1, Scopes: []string{"chat.posts.read", "chat.posts.write"}, Agent: &chatapps.AgentManifest{DisplayName: "Agent A", Description: "Conversation assistant"}}
			if _, err := extensions.Install(human("member"), chatcore.Principal{TenantID: "tenant-a", SubjectID: "member"}, id, manifest, manifest.Scopes); err != chatcore.ErrPermissionDenied {
				t.Fatalf("ordinary member installed: %v", err)
			}
			install, err := extensions.Install(human("manager"), p, id, manifest, manifest.Scopes)
			if err != nil || install.ID != "tenant-a:"+id+":agent-a" || install.Approver != "manager" {
				t.Fatalf("manager install=%+v err=%v", install, err)
			}
			bearer := token("tenant-a", "agent-a")
			body := fmt.Sprintf("Agent reply in %s", id)
			w := request(http.MethodPost, id, bearer, fmt.Sprintf(`{"body":%q}`, body), "send-"+id)
			if w.Code != http.StatusCreated {
				t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
			}
			var posted map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &posted); err != nil || posted["author_id"] != "agent-a" || posted["conversation_id"] != id {
				t.Fatalf("post=%+v err=%v", posted, err)
			}
			w = request(http.MethodPost, id, bearer, fmt.Sprintf(`{"body":%q}`, body), "send-"+id)
			var retried map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &retried); w.Code != http.StatusCreated || err != nil || retried["id"] != posted["id"] {
				t.Fatalf("idempotent retry status=%d post=%+v err=%v", w.Code, retried, err)
			}
			w = request(http.MethodGet, id, bearer, "", "")
			if w.Code != http.StatusOK || strings.Count(w.Body.String(), body) != 1 {
				t.Fatalf("scoped read status=%d body=%s", w.Code, w.Body.String())
			}
			eventPath := "/v1/conversations/" + id + "/events?max_events=1&wait_ms=1000"
			w = requestPath(http.MethodGet, eventPath, bearer, "", "")
			var firstPage struct {
				Events []struct {
					Kind string         `json:"kind"`
					Post map[string]any `json:"post"`
				} `json:"events"`
				Cursor string `json:"resume_cursor"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &firstPage); w.Code != http.StatusOK || err != nil || len(firstPage.Events) != 1 || firstPage.Events[0].Kind != "POST_CREATED" || firstPage.Events[0].Post["id"] != posted["id"] || firstPage.Cursor == "" {
				t.Fatalf("events status=%d page=%+v err=%v body=%s", w.Code, firstPage, err, w.Body.String())
			}
			if _, err := strconv.ParseUint(firstPage.Cursor, 10, 64); err == nil {
				t.Fatalf("event cursor disclosed raw sequence: %q", firstPage.Cursor)
			}
			resumePath := "/v1/conversations/" + id + "/events?max_events=1&wait_ms=20&resume_cursor=" + url.QueryEscape(firstPage.Cursor)
			w = requestPath(http.MethodGet, resumePath, bearer, "", "")
			var resumed struct {
				Events []json.RawMessage `json:"events"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resumed); w.Code != http.StatusOK || err != nil || len(resumed.Events) != 0 {
				t.Fatalf("resume status=%d page=%+v err=%v body=%s", w.Code, resumed, err, w.Body.String())
			}
			for _, denied := range []struct{ name, tenant, agent, conversation string }{
				{"wrong tenant", "tenant-b", "agent-a", otherTenant.ID},
				{"wrong agent", "tenant-a", "agent-other", id},
				{"wrong conversation", "tenant-a", "agent-a", "uninstalled-" + id},
			} {
				t.Run(denied.name, func(t *testing.T) {
					credential := token(denied.tenant, denied.agent)
					for _, method := range []string{http.MethodGet, http.MethodPost} {
						w := request(method, denied.conversation, credential, `{"body":"unauthorized"}`, "denied-"+id)
						if w.Code != http.StatusNotFound {
							t.Fatalf("%s %s admitted: status=%d body=%s", denied.name, method, w.Code, w.Body.String())
						}
					}
				})
			}
			if kind != chatcore.PublicChannel {
				for _, suffix := range []string{"", "/posts", "/events?wait_ms=20"} {
					for _, method := range []string{http.MethodGet, http.MethodPost} {
						if method == http.MethodPost && suffix != "/posts" {
							continue
						}
						existing := requestPath(method, "/v1/conversations/"+uninstalled.ID+suffix, bearer, `{"body":"denied"}`, "denied-existing-"+id)
						absent := requestPath(method, "/v1/conversations/absent-"+id+suffix, bearer, `{"body":"denied"}`, "denied-absent-"+id)
						if existing.Code != http.StatusNotFound || absent.Code != existing.Code || existing.Body.String() != absent.Body.String() {
							t.Fatalf("existence oracle %s %s: existing=%d %s absent=%d %s", method, suffix, existing.Code, existing.Body.String(), absent.Code, absent.Body.String())
						}
					}
				}
			}
			if _, err := extensions.ChangeStatus(human("manager"), p, id, install.ID, chatapps.Revoked); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				w := request(method, id, bearer, `{"body":"After revoke"}`, "after-revoke-"+id)
				if w.Code != http.StatusNotFound {
					t.Fatalf("revoked %s status=%d body=%s", method, w.Code, w.Body.String())
				}
			}
			w = requestPath(http.MethodGet, resumePath, bearer, "", "")
			if w.Code != http.StatusNotFound {
				t.Fatalf("revoked events status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	events, err := chatrecordstore.New(store.Store).Events(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	var posts int
	for _, event := range events {
		if event.ActorID == "agent-a" && event.Action == "post.created" {
			posts++
		}
	}
	if posts != 4 {
		t.Fatalf("agent post audit events=%d, want 4", posts)
	}
}
