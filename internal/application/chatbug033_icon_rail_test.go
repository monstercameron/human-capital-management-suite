package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// railChatFixture is the persona surface's chat port plus the conversation
// listing the agent list reads.
type railChatFixture struct {
	*personaSurfaceChatFixture
	rooms []chat.Conversation
}

func (f railChatFixture) ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error) {
	return chat.ListConversationsResponse{Conversations: f.rooms}, f.err
}

// TestTodo_CHATBUG_033 reads the agent list that travels with the conversation
// list: the viewer's direct conversation with an agent comes back with the
// agent's name, its own description and its stored icon, so the sidebar draws it
// on first paint; a channel and a direct conversation with no agent do not
// appear; and nobody who is not signed in as a person is answered.
func TestTodo_CHATBUG_033(t *testing.T) {
	store, scoped, _, now, _ := agentIconApplicationFixture(t)
	base, ctx, room, _, _ := personaSurfaceFixture(t)
	principal, _ := trust.FromContext(ctx)
	profiles, err := base.Personas.ListAvailable(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	profile := profiles[0].Profile
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent-v1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: profileRaw, ContentDigest: profiles[0].Digest, CreatedAt: now}
	owner := agentpersonastore.PersonaOwner{TenantID: "tenant-a", PersonaID: profile.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: profile.Owner}
	steward := agentpersonastore.PersonaOwner{TenantID: "tenant-a", PersonaID: profile.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: profile.Steward}
	if err := scoped.CreateDraftWithIcon(ctx, row, owner, steward, "user-a", now); err != nil {
		t.Fatal(err)
	}
	stored, err := scoped.GetIcon(ctx, profile.PersonaID)
	if err != nil || !stored.Value.Valid() {
		t.Fatal("the agent has no stored icon to deliver", stored, err)
	}
	// The conversation the fixture's agent reference belongs to is the viewer's
	// direct conversation; a public channel and a direct conversation the agent
	// reference does not belong to sit beside it.
	base.Chat = railChatFixture{personaSurfaceChatFixture: room, rooms: []chat.Conversation{
		{ID: "channel-a", TenantID: "tenant-a", Kind: chat.Direct},
		{ID: "public-room", TenantID: "tenant-a", Kind: chat.PublicChannel},
		{ID: "person-dm", TenantID: "tenant-a", Kind: chat.Direct},
	}}
	source := AgentIconChatDirectory{Base: base, Store: store}
	rail, err := source.RailAgentIcons(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rail.Agents) != 1 {
		t.Fatalf("want the one direct conversation with an agent, got %+v", rail.Agents)
	}
	entry := rail.Agents[0]
	if entry.ConversationID != "channel-a" || entry.AgentID != "agent:coach" || entry.Name != profile.DisplayName || entry.Purpose != profile.Purpose || entry.Icon != stored.Value || entry.IconRevision != stored.Revision {
		t.Fatalf("the entry does not carry the agent's own name, description and stored icon: %+v", entry)
	}
	if entry.Purpose == "" {
		t.Fatal("the agent's own description is empty; the header line has nothing to show")
	}

	// Served: the route answers the viewer's own list, and only a signed-in person.
	w := httptest.NewRecorder()
	transport.AgentIconDirectoryHandler{Source: source}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, personachat.Path+"?rail=direct", nil).WithContext(ctx))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"agents":[{"conversation_id":"channel-a"`) || !strings.Contains(w.Body.String(), `"icon_revision":`) || strings.Contains(w.Body.String(), "instructions") {
		t.Fatalf("rail payload %d %s", w.Code, w.Body.String())
	}
	admission, bearer := integrate1Admission(t, "tenant-a", "user-a", base.Now)
	handler := OverlayPersonaChatSurface(http.NotFoundHandler(), base, admission, PersonaChatBrowserOptions{Icons: store})
	w = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, personachat.Path+"?rail=direct", nil)
	request.Header.Set("Authorization", bearer)
	handler.ServeHTTP(w, request)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"`+profile.DisplayName+`"`) {
		t.Fatalf("served rail %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, personachat.Path+"?rail=direct", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unadmitted rail answered %d", w.Code)
	}
	if _, err := source.RailAgentIcons(context.Background()); err == nil {
		t.Fatal("a request with no verified person was answered")
	}
	// The per-conversation read the rail replaces on first paint is unchanged.
	w = httptest.NewRecorder()
	handler2 := httptest.NewRequest(http.MethodGet, personachat.Path+"?conversation_id=channel-a", nil)
	handler2.Header.Set("Authorization", bearer)
	handler.ServeHTTP(w, handler2)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"personas"`) {
		t.Fatalf("the conversation directory changed: %d %s", w.Code, w.Body.String())
	}
}
