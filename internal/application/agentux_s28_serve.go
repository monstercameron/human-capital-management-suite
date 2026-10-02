package application

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// composeAgentUXAmbientSurface puts the ambient agents' read and controls on
// the same chat store the conversation service writes (AGENTUX-066, -067,
// -068). It composes what a person can do on the page: see who reads here,
// switch their own "Don't act on my messages", press Add or Set on a card, and,
// as the conversation's administrator, turn "Reads every message here" on or
// off. Nothing here reads a message or calls a model: the outbox consumer that
// does is left out until its run planner exists, so no card appears that a
// person did not cause a model to write.
//
// A person's own time zone is not stored anywhere on the server yet, so the
// cards state their times in UTC and say so in the zone abbreviation.
func composeAgentUXAmbientSurface(store *chatstore.Store, todoAuthority func(context.Context, string, string, string) error, now func() time.Time) ambientagents.Surface {
	if store == nil || now == nil {
		return nil
	}
	service := &AgentUXAmbientService{DB: store, Now: now}
	service.Effects = AgentUXAmbientEffectsAdapter{Chat: store, TodoAuthority: todoAuthority}
	return AgentUXAmbientSurface{
		Service:    service,
		ViewerZone: func(context.Context, string, string) (string, error) { return "UTC", nil },
		SourceLink: func(_ context.Context, _, conversation, post string) (string, error) {
			return "/workspace/app/chat?" + url.Values{"conversation": {conversation}, "message": {post}}.Encode(), nil
		},
	}
}

// agentUXServed is what the served assembly mounts for the ambient agents and
// the demo agents' controls. A surface that is not composed stays nil and its
// path is answered as before.
type agentUXServed struct {
	Ambient ambientagents.Surface
	// Birthday is the person's own "share my birthday" preference.
	Birthday agentdemo.BirthdayPreferenceSurface
	// SupportInbox is the owner's "Simulate a customer email". It has no
	// production composition yet: the executor's ports (objects, queue, owner
	// check, planner, alert delivery) are libraries only.
	SupportInbox agentdemo.Surface
}

func (u agentUXServed) overlay(next http.Handler, admission transport.Config) http.Handler {
	next = OverlayAgentUXDemo(next, u.Birthday, u.SupportInbox, admission)
	return OverlayAgentUXAmbient(next, u.Ambient, admission)
}

// agentUXServedPath is the part of the served assembly's path set that belongs
// to the ambient agents and the demo agents. Without it the browser-policy
// wrapper never hands these requests to their overlays.
func agentUXServedPath(path string) bool {
	return path == ambientagents.Path || strings.HasPrefix(path, ambientagents.Path+"/")
}

// BindAgentUX composes the ambient agents' surface and the profile's birthday
// preference onto the served assembly. It runs once, after the chat and agent
// databases exist; a cell without either leaves the field empty and the paths
// answer as they did before.
func (s *agentServedAssembly) BindAgentUX(chatStore *chatstore.Store, extensions *ChatExtensions, agents *agentstore.Store, now func() time.Time) error {
	if s == nil {
		return nil
	}
	// Adding a task to the channel's list is the person's own change to that
	// list: it must pass the same check as editing it by hand (the channel is
	// open for writing, the person is a member who may edit), never a looser one.
	var todoAuthority func(context.Context, string, string, string) error
	if extensions != nil {
		todoAuthority = func(ctx context.Context, tenant, conversation, actor string) error {
			return extensions.channelWriteActor(ctx, chat.Principal{TenantID: tenant, SubjectID: actor}, tenant, conversation, channelTodoStatusAction("ADD"))
		}
	}
	if surface := composeAgentUXAmbientSurface(chatStore, todoAuthority, now); surface != nil {
		s.agentUX.Ambient = surface
	}
	if agents != nil && now != nil {
		preferences, err := agentstore.NewSupportInboxStore(agents)
		if err != nil {
			return err
		}
		s.agentUX.Birthday = AgentUXDemoBirthdayProfile{Store: preferences, TenantUUID: tenantKeyMapper[values.TenantId](pgstore.TenantID), Now: now}
	}
	return nil
}

// OverlayAgentUXDemo mounts the two demo-agent controls under the agent
// controls path: a person's own "share my birthday" preference, and the owner's
// "Simulate a customer email". A control whose surface is not composed is not
// mounted, so its path falls through to the agent controls' own answer.
func OverlayAgentUXDemo(next http.Handler, birthday agentdemo.BirthdayPreferenceSurface, inbox agentdemo.Surface, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	preference := agentdemo.BirthdayPreferenceHandler{Surface: birthday}
	simulate := agentdemo.Handler{Surface: inbox}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var handler http.Handler
		switch {
		case r.URL.Path == agentdemo.BirthdayPreferencePath && birthday != nil:
			handler = preference
		case r.URL.Path == agentdemo.Path && inbox != nil:
			handler = simulate
		default:
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
