package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaSurfaceChatFixture struct {
	room    chat.Conversation
	members []chat.Membership
	posts   []chat.Post
	sends   []chat.SendPostRequest
	err     error
}

func (s *personaSurfaceChatFixture) GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error) {
	return s.room, s.err
}
func (s *personaSurfaceChatFixture) ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return chat.ListMembershipsResponse{Memberships: s.members}, s.err
}
func (s *personaSurfaceChatFixture) ListPosts(context.Context, chat.ListPostsRequest) (chat.ListPostsResponse, error) {
	return chat.ListPostsResponse{Posts: s.posts}, s.err
}
func (s *personaSurfaceChatFixture) SendPost(_ context.Context, req chat.SendPostRequest) (chat.Post, error) {
	s.sends = append(s.sends, req)
	return chat.Post{ID: "fresh-post", ConversationID: req.ConversationID}, s.err
}

type personaSurfaceInvocationsFixture struct {
	rows                []agentinvoke.Invocation
	owner, tenant, room string
	err                 error
}

func (s *personaSurfaceInvocationsFixture) ListPersonaInvocations(_ context.Context, tenant, owner, room string) ([]agentinvoke.Invocation, error) {
	s.tenant, s.owner, s.room = tenant, owner, room
	return s.rows, s.err
}
func (s *personaSurfaceInvocationsFixture) Lookup(_ context.Context, tenant, owner, id string) (agentinvoke.Invocation, error) {
	s.tenant, s.owner = tenant, owner
	for _, row := range s.rows {
		if row.ID == id && row.TenantID == tenant && row.InvokerID == owner {
			return row, s.err
		}
	}
	return agentinvoke.Invocation{}, personachat.ErrDenied
}

type personaSurfaceExecutionFixture struct {
	run runstate.Run
	err error
}

func (s *personaSurfaceExecutionFixture) Get(context.Context, string) (runstate.Run, error) {
	return s.run, s.err
}
func (s *personaSurfaceExecutionFixture) Create(context.Context, runstate.Run) error { return nil }
func (s *personaSurfaceExecutionFixture) Save(context.Context, runstate.Run, uint64) error {
	return nil
}

type personaSurfaceReferencesFixture struct {
	personaReferenceLookupFake
	candidates []chat.ReferenceCandidate
}

func (s *personaSurfaceReferencesFixture) ListPersonaReferenceCandidates(context.Context, chat.Principal, string, string, string) ([]chat.ReferenceCandidate, error) {
	return s.candidates, nil
}

func personaSurfaceFixture(t *testing.T) (*PersonaChatSurface, context.Context, *personaSurfaceChatFixture, *personaSurfaceInvocationsFixture, *personaSurfaceExecutionFixture) {
	t.Helper()
	p := agentUserCatalogPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), p)
	profile := candidateProfile(t)
	hidden := agentUserCatalogPersona(t, "secret-comp", "Secret Comp Analyst", "agent.self_service", agentpersonaSkillPin())
	facts := currentPersonaReferenceFacts("agent:coach")
	facts.PersonaID, facts.PersonaVersion, facts.CurrentVersion = profile.Profile.PersonaID, uint64(profile.Profile.Version), uint64(profile.Profile.Version)
	ref := chat.Reference{Kind: chat.AgentMention, TenantID: "tenant-a", ID: "agent:coach", Display: profile.Profile.DisplayName, ConversationID: "channel-a"}
	refs := &personaSurfaceReferencesFixture{personaReferenceLookupFake: personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{ref.ID: facts}}, candidates: []chat.ReferenceCandidate{{Reference: ref, Eligible: true}}}
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	chatFixture := &personaSurfaceChatFixture{room: chat.Conversation{TenantID: "tenant-a", ID: "channel-a", Kind: chat.PublicChannel, Revision: 1}, members: []chat.Membership{{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: "channel-a", SubjectID: "user-a", JoinedAt: &at}}}
	invocation := agentinvoke.Invocation{ID: "invocation-a", TenantID: "tenant-a", InvokerID: "user-a", ConversationID: "channel-a", ThreadID: "post-a", PostID: "post-a", PersonaID: profile.Profile.PersonaID, State: agentinvoke.InvocationStarted}
	chatFixture.posts = []chat.Post{{ID: "post-a", TenantID: "tenant-a", AuthorID: "user-a", ConversationID: "channel-a", Body: "Help with policy", References: []chat.Reference{ref, {Kind: chat.PersonMention, ID: "coworker", TenantID: "tenant-a"}}}}
	invocations := &personaSurfaceInvocationsFixture{rows: []agentinvoke.Invocation{invocation}}
	runID, err := personaSurfaceRunID(invocation)
	if err != nil {
		t.Fatal(err)
	}
	execution := &personaSurfaceExecutionFixture{run: runstate.Run{ID: runID, AdmissionID: runID, TenantID: "tenant-a", State: runstate.StateRunning, Checkpoints: []runstate.Checkpoint{{Phase: runstate.PhaseModelCall}}}}
	discovery := &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{personaChatReplyPurpose: {{Definition: agentskills.SkillDefinition{ID: agentpersonaSkillPin().ID, Version: 2, Description: "Read policy", SideEffectTier: agentskills.TierRead}, Digest: agentpersonaSkillPin().Digest}}}}
	s := &PersonaChatSurface{Chat: chatFixture, References: refs, Personas: agentUserCatalogPersonas{profile, hidden}, Skills: discovery, Invocations: invocations, Executions: func(context.Context, string) (runstate.Store, error) { return execution, nil }, Now: func() time.Time { return at }}
	return s, ctx, chatFixture, invocations, execution
}

func TestTodo_AGENTP_019_SurfaceHidesUninvocableProfileFromPayload(t *testing.T) {
	s, ctx, room, _, _ := personaSurfaceFixture(t)
	directory, err := s.Directory(ctx, "channel-a")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(directory)
	if len(directory.Personas) != 1 || directory.Personas[0].Reference.ID != "agent:coach" || len(directory.Personas[0].Skills) != 1 || directory.Personas[0].Skills[0].Tier != "T0" || strings.Contains(string(encoded), "Secret Comp Analyst") || strings.Contains(string(encoded), "instructions") {
		t.Fatalf("payload=%s", encoded)
	}
	if directory.Personas[0].ReplyPlacement != "private_audience" {
		t.Fatalf("placement=%s", directory.Personas[0].ReplyPlacement)
	}
	discovery := s.Skills.(*agentUserCatalogDiscovery)
	if len(discovery.called) != 1 || discovery.called[0] != personaChatReplyPurpose || directory.Personas[0].Purpose == personaChatReplyPurpose {
		t.Fatalf("governance purpose=%v human purpose=%q", discovery.called, directory.Personas[0].Purpose)
	}
	s.ChannelAlwaysPrivate = func(context.Context, string, personaReferenceFacts) (bool, error) { return true, nil }
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || directory.Personas[0].ReplyPlacement != "private_always" {
		t.Fatalf("current channel policy placement=%+v %v", directory, err)
	}
	s.ChannelAlwaysPrivate = nil
	room.room.Kind = chat.PrivateChannel
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || directory.Personas[0].ReplyPlacement != "private_always" {
		t.Fatalf("private directory=%+v %v", directory, err)
	}
	s.Skills = &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{}}
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 0 {
		t.Fatalf("revoked skill directory=%+v %v", directory, err)
	}
	if _, err = s.Directory(context.Background(), "channel-a"); !errors.Is(err, personachat.ErrUnauthenticated) {
		t.Fatalf("no auth=%v", err)
	}
	if _, err = s.Directory(ctx, " "); !errors.Is(err, personachat.ErrInvalid) {
		t.Fatalf("invalid=%v", err)
	}
	room.members = nil
	if _, err = s.Directory(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("left room=%v", err)
	}
}

func TestTodo_AGENTP_019_SurfaceGovernancePurposeUsesCurrentGrantGate(t *testing.T) {
	s, ctx, _, _, _ := personaSurfaceFixture(t)
	p := foregroundPrincipal(t, s.Now().Add(-time.Minute), s.Now().Add(time.Hour), personaChatReplyPurpose, "user-a", "tenant-a")
	ctx = trust.WithPrincipal(ctx, p)
	pin := agentpersonaSkillPin()
	record := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read policy", RequiredPurposes: []string{personaChatReplyPurpose}}, Digest: pin.Digest, Status: agentskills.StatusActive}
	grants := agentgate.StaticGrants{{ID: "grant", Tenant: p.Tenant(), Skill: record.Definition.Key(), Roles: []string{"employee"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{personaChatReplyPurpose}}}
	current := sourceCurrent{user: agentgate.UserContext{Principal: p, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}}}
	source, err := NewAgentSkillSource(sourceSkillCatalog{records: []agentskills.SkillRecord{record}}, grants, current)
	if err != nil {
		t.Fatal(err)
	}
	s.Skills = source
	directory, err := s.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 1 || directory.Personas[0].Purpose != "agent.self_service" {
		t.Fatalf("exact purpose grant directory=%+v %v", directory, err)
	}
	current.user.Roles = []string{"revoked"}
	source, err = NewAgentSkillSource(sourceSkillCatalog{records: []agentskills.SkillRecord{record}}, grants, current)
	if err != nil {
		t.Fatal(err)
	}
	s.Skills = source
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 0 {
		t.Fatalf("revoked current role directory=%+v %v", directory, err)
	}
}

func TestTodo_AGENTP_020_SurfaceProgressOwnershipFailureAndFreshRetry(t *testing.T) {
	s, ctx, room, invocations, execution := personaSurfaceFixture(t)
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].Activity != "Validating answer" || progress.Invocations[0].AgentName != "People Coach" || invocations.owner != "user-a" || invocations.tenant != "tenant-a" {
		t.Fatalf("progress=%+v %v scopes=%+v", progress, err, invocations)
	}
	invocations.rows[0].InvokerID = "other-user"
	if _, err = s.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("foreign owner=%v", err)
	}
	invocations.rows[0].InvokerID = "user-a"
	execution.run.State, execution.run.TerminalCode = runstate.StateFailed, "MODEL_UNAVAILABLE"
	progress, err = s.Progress(ctx, "channel-a")
	if err != nil || !progress.Invocations[0].Retryable || progress.Invocations[0].FailureCode != "MODEL_UNAVAILABLE" {
		t.Fatalf("failure=%+v %v", progress, err)
	}
	result, err := s.Retry(ctx, "invocation-a", "unique-click")
	if err != nil || result.PostID != "fresh-post" || len(room.sends) != 1 || room.sends[0].Principal.SubjectID != "user-a" || room.sends[0].ParentID != "post-a" || len(room.sends[0].References) != 1 || room.sends[0].References[0].Kind != chat.AgentMention {
		t.Fatalf("retry=%+v %v sends=%+v", result, err, room.sends)
	}
	execution.run.State = runstate.StateCompleted
	if _, err = s.Retry(ctx, "invocation-a", "unique-click"); !errors.Is(err, personachat.ErrConflict) {
		t.Fatalf("completed retry=%v", err)
	}
	if _, err = s.Retry(ctx, "invocation-a", ""); !errors.Is(err, personachat.ErrInvalid) {
		t.Fatalf("keyless=%v", err)
	}
	if _, err = s.Retry(context.Background(), "invocation-a", "unique-click"); !errors.Is(err, personachat.ErrUnauthenticated) {
		t.Fatalf("untrusted retry=%v", err)
	}
	execution.err = agentrunstate.ErrNotFound
	progress, err = s.Progress(ctx, "channel-a")
	if err != nil || progress.Invocations[0].Status != "STARTED" {
		t.Fatalf("pending=%+v %v", progress, err)
	}
}

func TestTodo_AGENTP_019_SurfaceHTTPAdmissionIgnoresForgedScope(t *testing.T) {
	s, ctx, room, invocations, execution := personaSurfaceFixture(t)
	p, _ := trust.FromContext(ctx)
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte("persona-chat-http-test-key-32bytes"), Issuer: "persona-test", Audience: "persona-http", Now: s.Now})
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	validToken, err := verifier.Issue(trust.Claims{Issuer: "persona-test", Audience: "persona-http", Tenant: p.Tenant().String(), Subject: p.Subject(), SubjectKind: "human", AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "test-session", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	handler := OverlayPersonaChatSurface(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(418) }), s, transport.Config{Verifier: verifier})
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, token := range []string{"", "Bearer forged", "Bearer " + validToken} {
		req, err := http.NewRequest("GET", server.URL+personachat.Path+"/invocations?conversation_id=channel-a&tenant_id=other&invoker_id=other", nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		want := 401
		if token == "Bearer "+validToken {
			want = 200
		}
		if response.StatusCode != want {
			t.Fatalf("status=%d want=%d body=%s", response.StatusCode, want, body)
		}
	}
	if invocations.owner != "user-a" || invocations.tenant != "tenant-a" {
		t.Fatalf("forged scope reached store: %+v", invocations)
	}
	execution.run.State, execution.run.TerminalCode = runstate.StateFailed, "MODEL_UNAVAILABLE"
	browserServer := httptest.NewServer(OverlayPersonaChatSurface(http.NotFoundHandler(), s, transport.Config{Verifier: verifier}, PersonaChatBrowserOptions{PublicOrigin: "http://persona.test", BrowserLogin: true}))
	defer browserServer.Close()
	// Cookie substitution remains opt-in and never overrides an explicit credential.
	for _, test := range []struct {
		name, method, credential, origin, site string
		proof                                  bool
		want                                   int
	}{
		{name: "cookie read", method: "GET", want: 200},
		{name: "explicit bad credential", method: "GET", credential: "Bearer forged", want: 401},
		{name: "cookie write without proof", method: "POST", want: 403},
		{name: "foreign origin", method: "POST", origin: "http://evil.test", proof: true, want: 403},
		{name: "cross site missing origin", method: "POST", site: "cross-site", proof: true, want: 403},
		{name: "cookie write with current proof", method: "POST", origin: "http://persona.test", proof: true, want: 200},
		{name: "native bearer write", method: "POST", credential: "Bearer " + validToken, want: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := personachat.Path + "?conversation_id=channel-a"
			if test.method == "POST" {
				path = personachat.Path + "/invocations/invocation-a/retry"
			}
			req, err := http.NewRequest(test.method, browserServer.URL+path, strings.NewReader(`{"idempotency_key":"browser-retry-key"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = "persona.test"
			req.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: validToken})
			if test.proof {
				req.AddCookie(&http.Cookie{Name: edge.BrowserCSRFCookieName, Value: "edge-issued-proof"})
			}
			if test.credential != "" {
				req.Header.Set("Authorization", test.credential)
			}
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.site != "" {
				req.Header.Set("Sec-Fetch-Site", test.site)
			}
			response, err := browserServer.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, test.want, body)
			}
		})
	}
	if len(room.sends) != 2 || room.sends[0].ParentID != "post-a" || room.sends[0].Principal.SubjectID != "user-a" {
		t.Fatalf("browser guard effect calls=%+v", room.sends)
	}
	request := httptest.NewRequest("GET", personachat.Path+"?conversation_id=channel-a", nil)
	request.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: validToken})
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("cookie substituted while browser login disabled=%d", denied.Code)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/fallback", nil))
	if recorder.Code != 418 {
		t.Fatalf("fallback=%d", recorder.Code)
	}
	if _, err := (*personaServeWiring)(nil).chatSurface(nil, nil); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("nil composition=%v", err)
	}
}
