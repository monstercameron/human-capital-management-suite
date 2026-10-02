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

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chatextensions"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type integrate1IconAdministrator struct{}

func (integrate1IconAdministrator) AuthorizeAgentIconAdministrator(ctx context.Context, tenant values.TenantId, actor string) error {
	p, ok := trust.FromContext(ctx)
	if !ok || p.Tenant() != tenant || p.Subject() != actor || actor != "user:owner" {
		return transport.ErrAgentIconDenied
	}
	return nil
}

func integrate1Admission(t *testing.T, tenant, subject string, now func() time.Time) (transport.Config, string) {
	t.Helper()
	v, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte("integration-chat-test-key-32bytes"), Issuer: "integration", Audience: "chat", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	token, err := v.Issue(trust.Claims{Issuer: "integration", Audience: "chat", Tenant: tenant, Subject: subject, SubjectKind: "human", AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "integration-session", IssuedAtUnix: now().Add(-time.Minute).Unix(), ExpiresAtUnix: now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return transport.Config{Verifier: v}, "Bearer " + token
}

func TestIntegrate1ServedWritingStyles(t *testing.T) {
	s, _, model, _, _, _ := chattoneFixture(t)
	admission, bearer := integrate1Admission(t, "tenant", "person", s.Now)
	assembly := &agentServedAssembly{WritingStyles: s}
	h := assembly.Overlay(http.NotFoundHandler(), admission)
	for _, tc := range []struct {
		path, method, body string
		want               int
	}{
		{ChattonePath + "/suggestion?conversation_id=room", "GET", "", 200},
		{ChattonePath + "/rewrite", "POST", `{"conversation_id":"room","draft":"Please retain @Dana and 42.","style_id":"friendly"}`, 200},
		{"/unrelated", "GET", "", 404},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", bearer)
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	if model.calls != 1 {
		t.Fatalf("governed preview calls = %d", model.calls)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", ChattonePath+"/rewrite", strings.NewReader(`{}`)))
	if w.Code != 401 || model.calls != 1 {
		t.Fatal("unadmitted writer reached model", w.Code, model.calls)
	}
}

func TestIntegrate1ServedAssemblyBindings(t *testing.T) {
	f := newAgentActionFixture(t, true)
	core, err := pgxadapter.NewPool(t.Context(), f.db.URL, map[string]string{"search_path": f.db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	personas, _, _, _, db := agentIconApplicationFixture(t)
	agents := commonAgentOpenIntegrationStore(t, db)
	styles, _, _, _, _, _ := chattoneFixture(t)
	assembly, err := composeAgentServedAssembly(agentServedAssemblyInput{Core: core, AgentDatabase: composedAgentDatabase{store: agents, personas: personas}, Cell: f.cell, ActionAuthority: f.authority, WritingStyles: styles})
	if err != nil || assembly == nil || assembly.WritingStyles != styles {
		t.Fatalf("writing-style input lost in real assembly: %+v %v", assembly, err)
	}
	icons, ok := assembly.Icons.(*AgentIconSurface)
	if !ok || icons.Store != personas {
		t.Fatal("real persona store not bound to icon commands", assembly.Icons)
	}
	administrator, ok := icons.Administrators.(AgentIconRoleAdministrator)
	if !ok || administrator.Roles != f.cell.RoleAccess {
		t.Fatal("current cell role authority not bound", icons.Administrators)
	}
	admission, bearer := integrate1Admission(t, "tenant", "person", styles.Now)
	request := httptest.NewRequest(http.MethodGet, ChattonePath+"/suggestion?conversation_id=room", nil)
	request.Header.Set("Authorization", bearer)
	response := httptest.NewRecorder()
	assembly.Overlay(http.NotFoundHandler(), admission).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatal("assembled writing-style route", response.Code, response.Body.String())
	}
}

func TestIntegrate1NewRoutesRequireBrowserCSRF(t *testing.T) {
	verifications := 0
	admission := transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) {
		verifications++
		return nil, trust.ErrNoCredential
	})}
	handler := (&agentServedAssembly{browserLogin: true}).Overlay(http.NotFoundHandler(), admission)
	for _, path := range []string{ChattonePath + "/rewrite", transport.AgentIconPath + "/shuffle"} {
		request := httptest.NewRequest(http.MethodPost, "http://cell.test"+path, strings.NewReader(`{}`))
		request.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: "fixture"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || verifications != 0 {
			t.Fatal("new mutation route admitted before browser proof", path, response.Code, verifications)
		}
	}
}

func TestIntegrate1ServedIcons(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "policy-helper")
	admission, bearer := integrate1Admission(t, "tenant-a", "user:owner", func() time.Time { return now })
	surface := &AgentIconSurface{Store: store, Administrators: integrate1IconAdministrator{}, Now: func() time.Time { return now }}
	h := (&agentServedAssembly{Icons: surface}).Overlay(http.NotFoundHandler(), admission)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", transport.AgentIconPath+"/preview", strings.NewReader(`{"persona_id":"policy-helper","expected_revision":1,"action":"regenerate"}`))
	r.Header.Set("Authorization", bearer)
	h.ServeHTTP(w, r)
	var result transport.AgentIconReply
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Icon.Valid() || result.Revision != 1 {
		t.Fatal(w.Code, w.Body)
	}
	unchanged, err := scoped.GetIcon(ctx, "policy-helper")
	if err != nil || unchanged.Revision != 1 {
		t.Fatal("preview persisted", unchanged, err)
	}
	unauthenticated := httptest.NewRecorder()
	h.ServeHTTP(unauthenticated, httptest.NewRequest("POST", transport.AgentIconPath+"/shuffle", strings.NewReader(`{"persona_id":"policy-helper","expected_revision":1}`)))
	if unauthenticated.Code != 401 {
		t.Fatal("icon admission bypass", unauthenticated.Code)
	}
}

func TestIntegrate1IconCatalogComposition(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "catalog-wired")
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: personaAdminCatalogStoreAdapter{store: store}, Authorizer: &catalogAuth{}, Skills: agentIconCatalogSkills{}, Targets: catalogTargets{}, Grants: &catalogGrants{allowed: true}})
	if err != nil {
		t.Fatal(err)
	}
	reader := client.(readOnlyPersonaAdminCatalog)
	versions, err := reader.service.Versions.ListPersonaCatalogVersions(ctx, "tenant-a")
	icon, iconErr := scoped.GetIcon(ctx, "catalog-wired")
	if err != nil || iconErr != nil || len(versions) != 1 || versions[0].Icon != icon.Value || versions[0].IconRevision != icon.Revision {
		t.Fatal("production catalog omitted icon", versions, err, iconErr)
	}
}

func TestIntegrate1AssistantPreparationIcon(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(t.Context(), db.SQL); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	tenant := values.TenantId(localAgentDemoTenant)
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, mapper(tenant))
	agents := commonAgentOpenIntegrationStore(t, db)
	personas, err := agentpersonastore.New(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := personas.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	starter, _ := agenttemplate.PersonaStarterFor(localAgentDemoAssistantStarterID, 1)
	manifest, err := ensureLocalAgentDemoAssistantManifest(t.Context(), agents, mapper(tenant), starter)
	if err != nil {
		t.Fatal(err)
	}
	policy := validPersonaProfileForLifecycleTest(t, localAgentDemoAdmin).Profile
	now := time.Now().UTC()
	first, _, err := ensureLocalAgentDemoAssistantVersion(t.Context(), scoped, starter, manifest, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	icon, err := scoped.GetIcon(t.Context(), first.PersonaID)
	if err != nil || !icon.Value.Valid() || icon.Revision != 1 {
		t.Fatal("raw preparation omitted persisted icon", icon, err)
	}
	if _, _, err = ensureLocalAgentDemoAssistantVersion(t.Context(), scoped, starter, manifest, policy, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	replayed, err := scoped.GetIcon(t.Context(), first.PersonaID)
	if err != nil || replayed.Value != icon.Value || replayed.Revision != icon.Revision {
		t.Fatal("preparation replaced identity", replayed, err)
	}
}

func TestIntegrate1ChannelWidgetsIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	adapter := chatstore.NewModeratedAdapter(chatstore.NewAdapter(raw))
	service := chat.NewService(adapter, time.Now)
	service.SetAuthority(servedPersonaChatAuthority{})
	p := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	if _, err := service.CreateConversation(context.Background(), chat.CreateConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: "general", Kind: chat.PublicChannel, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	admission, bearer := integrate1Admission(t, p.TenantID, p.SubjectID, time.Now)
	ctx, _, denied := transport.Admit(context.Background(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(http.Header{"Authorization": {bearer}}), Method: "/api/chat/widgets", Kind: transport.KindHTTPEdge})
	if denied != nil {
		t.Fatal(denied)
	}
	service.SetAuthority(newChatCurrentAuthority(&mutableChatFacts{principal: chatpolicy.Principal{Active: true, Roles: []string{"employee"}, AuthorityRevision: 1}}, adapter, chatauthority.New(raw), newChatAuthorityCache(time.Second, time.Now)))
	ext := &ChatExtensions{Conversations: chatFilterBoundary(service), TodoStore: raw}
	widgets, err := ext.ChannelWidgets(ctx, p, p.TenantID, "general")
	if err != nil || widgets.Team.Revision != 1 || widgets.Project.Revision != 1 || len(widgets.Team.Members) != 1 {
		t.Fatalf("served widget path: %+v %v", widgets, err)
	}
	if _, err = ext.ChannelWidgets(ctx, p, "other", "general"); err == nil {
		t.Fatal("cross-tenant widgets read")
	}
	// Exercise the actual extension wrapper and wire response that the wasm
	// client validates, in addition to the current-authority store path.
	handler := transportchat.NewHandler(transportchat.Dependencies{Service: integrate1ChatExtensions(composedChat{extensions: ext})})
	request := httptest.NewRequest(http.MethodPost, chatv1.ChatExtensionsService_GetChannelWidgets_FullMethodName, strings.NewReader(`{"hostTenantId":"tenant","conversationId":"general"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request.WithContext(ctx))
	var wire struct {
		Team struct {
			ConversationID string `json:"conversationId"`
		} `json:"team"`
		Project struct {
			ConversationID string `json:"conversationId"`
		} `json:"project"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &wire) != nil || wire.Team.ConversationID != "general" || wire.Project.ConversationID != "general" {
		t.Fatalf("served widget response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestIntegrate1ServedModerationRouteAndSaved(t *testing.T) {
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(t.Context(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	moderated := chatstore.NewModeratedAdapter(chatstore.NewAdapter(raw))
	core := chat.NewService(moderated, time.Now)
	core.SetAuthority(servedPersonaChatAuthority{})
	p := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	if _, err = core.CreateConversation(t.Context(), chat.CreateConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", Kind: chat.PublicChannel, Name: "room"}); err != nil {
		t.Fatal(err)
	}
	post, err := core.SendPost(t.Context(), chat.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", Body: "Private original", IdempotencyKey: "original"})
	if err != nil {
		t.Fatal(err)
	}
	if err = raw.RunTenantTx(t.Context(), p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `UPDATE chat_conversation SET route_shard='chat-default',route_epoch=1 WHERE tenant_id=$1 AND id=$2`, p.TenantID, "room")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	directory := chatrouting.NewMemoryDirectory()
	route, err := directory.Reserve(t.Context(), chatrouting.ReserveRequest{ConversationID: "room", HostTenantID: p.TenantID, ShardID: "chat-default", IdempotencyKey: "route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = directory.Activate(t.Context(), "room", p.TenantID, route.Epoch); err != nil {
		t.Fatal(err)
	}
	ports := &chatremoveRoutedStore{ModerationStore: moderated, Permissions: moderated, Directory: directory, Cache: chatrouting.NewRouteCache(time.Second), Now: time.Now}
	runtime := composedChat{service: core, extensions: &ChatExtensions{Conversations: core, TodoStore: raw}, moderationStore: moderated, moderationPermissions: ports, moderation: &chat.ModerationService{Store: ports}}
	admission, bearer := integrate1Admission(t, p.TenantID, p.SubjectID, time.Now)
	handler := overlayIntegrate1Chat(http.NotFoundHandler(), runtime, VoiceService{}, admission)
	call := func(method, path string, value any, authorized bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		if value != nil {
			if err := json.NewEncoder(&body).Encode(value); err != nil {
				t.Fatal(err)
			}
		}
		request := httptest.NewRequest(method, path, &body)
		request.Header.Set("Content-Type", "application/json")
		if authorized {
			request.Header.Set("Authorization", bearer)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := call(http.MethodGet, SavedMessagesPath, nil, true); response.Code != 200 {
		t.Fatal("Saved route", response.Code, response.Body)
	}
	if response := call(http.MethodPost, SavedMessagesPath, SavedMessageCommand{Action: "save", ConversationID: "room", PostID: post.ID}, true); response.Code != 200 {
		t.Fatal("save", response.Code, response.Body)
	}
	input := chatremoveInput{Removal: chat.RemovalRequest{Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{post.ID}}, ReasonCode: "spam", Action: "remove"}}
	preview := call(http.MethodPost, ChatModerationPath+"/preview", input, true)
	if preview.Code != 200 {
		t.Fatal("preview", preview.Code, preview.Body)
	}
	var result chat.RemovalPreview
	if err = json.Unmarshal(preview.Body.Bytes(), &result); err != nil || result.Count != 1 {
		t.Fatal(result, err)
	}
	input.Removal.Confirmation = result.Confirmation
	input.Removal.ConfirmedCount = result.Count
	if response := call(http.MethodPost, ChatModerationPath+"/apply", input, false); response.Code != 401 {
		t.Fatal("unadmitted mutation", response.Code)
	}
	applied := call(http.MethodPost, ChatModerationPath+"/apply", input, true)
	if applied.Code != 200 {
		t.Fatal("leased removal", applied.Code, applied.Body)
	}
	page, err := core.ListPosts(t.Context(), chat.ListPostsRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", Page: chat.Page{PageSize: 10}})
	if err != nil || len(page.Posts) != 1 || !page.Posts[0].Deleted || strings.Contains(page.Posts[0].Body, "Private original") {
		t.Fatal("reader disclosed retained body", page, err)
	}
	if response := call(http.MethodGet, SavedMessagesPath, nil, true); response.Code != 200 || strings.Contains(response.Body.String(), "Private original") {
		t.Fatal("Saved disclosed removed body", response.Code, response.Body)
	}
	if response := call(http.MethodGet, ChatModerationPagePath, nil, true); response.Code != 200 || !strings.Contains(response.Body.String(), `data-chatremove-close="true"`) || strings.Contains(response.Body.String(), "<style") {
		t.Fatal("admitted moderation page", response.Code, response.Body)
	}
	if response := call(http.MethodPost, ChatVoicePath+"read", chat.TranscriptionRequest{}, false); response.Code != 401 {
		t.Fatal("voice route bypassed admission", response.Code)
	}
	if _, err = directory.BeginMove(t.Context(), "room", p.TenantID, route.Epoch, "other"); err != nil {
		t.Fatal(err)
	}
	if _, err = ports.CommitRemoval(t.Context(), input.Removal, time.Now()); err == nil {
		t.Fatal("moving route accepted mutation")
	}
	if ext, ok := integrate1ChatExtensions(runtime).(*ChatModerationExtensions); !ok || ext.Moderation != runtime.moderation {
		t.Fatal("extension moderation bypass")
	}
}

func TestIntegrate1ServedVoiceAdmission(t *testing.T) {
	access := &chatvoiceAccessFixture{allow: true}
	decoder := &chatvoiceDecoderFixture{duration: 1000}
	media := &chatvoiceMediaFixture{}
	writer := &chatvoiceWriterFixture{}
	store := &chatvoiceStoreFixture{}
	voice := VoiceService{Access: access, Decoder: decoder, Media: media, Messages: writer, Transcripts: store}
	admission, bearer := integrate1Admission(t, "tenant", "alice", time.Now)
	handler := overlayIntegrate1Chat(http.NotFoundHandler(), composedChat{}, voice, admission)
	input := VoiceSendRequest{TenantID: "tenant", ConversationID: "room", IdempotencyKey: "voice", ContentType: "audio/webm", Content: []byte("fixture"), Locale: "en-US"}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	call := func(auth bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, ChatVoicePath+"send", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		if auth {
			request.Header.Set("Authorization", bearer)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := call(false); response.Code != 401 || decoder.calls != 0 || writer.calls != 0 {
		t.Fatal("voice side effect before admission", response.Code)
	}
	response := call(true)
	if response.Code != 200 || writer.calls != 1 || store.requests != 1 || !strings.Contains(response.Body.String(), `"DurationMS":1000`) {
		t.Fatal("voice handler not wired", response.Code, response.Body, writer.calls, store.requests)
	}
}
