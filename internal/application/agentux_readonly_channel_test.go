package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentReadOnlyChannel is a real channel, with the member the surface fixture
// signs in as, served by the fixture's surface over the real chat service.
func agentReadOnlyChannel(t *testing.T, member bool) (*PersonaChatSurface, context.Context, *agentUX070Room) {
	t.Helper()
	room := newAgentUX070Room(t, agentUX070Options{question: "Good morning."})
	surface, ctx, _, invocations, _ := personaSurfaceFixture(t)
	principal, _ := trust.FromContext(ctx)
	if member {
		if _, err := room.service.AddMembership(room.ctx, chat.AddMembershipRequest{Principal: room.asker, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: room.channel, SubjectID: principal.Subject(), HistoryVisibility: chat.FullHistory}}); err != nil {
			t.Fatal(err)
		}
	}
	invocations.rows = nil
	surface.Chat = room.service
	return surface, ctx, room
}

func (r *agentUX070Room) setStatus(status chatpolicy.ChannelStatus) {
	r.t.Helper()
	current, err := r.store.ReadChannelStatus(r.ctx, "tenant-a", r.channel)
	if err != nil {
		r.t.Fatal(err)
	}
	request := chat.ChangeChannelStatusRequest{Principal: r.asker, TenantID: "tenant-a", ConversationID: r.channel, Status: status, ExpectedRevision: current.Revision, Reason: "test"}
	if _, err = r.store.CommitChannelStatus(r.ctx, request, r.now, func(context.Context, chat.ChannelStatus) error { return nil }); err != nil {
		r.t.Fatalf("set status %s: %v", status, err)
	}
}

// A member of a channel that is locked, announcements only or archived may
// still read which agents are there and what became of their own questions:
// those are reads, and the channel's status forbids writing. They were answered
// 403, three times on every load of such a channel. Somebody who is not a
// member is refused as before, and nothing can be started from the reads.
func TestAgentUX_ReadOnlyChannelReads(t *testing.T) {
	for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusOpen, chatpolicy.StatusAnnouncements, chatpolicy.StatusLocked, chatpolicy.StatusArchived} {
		t.Run(string(status), func(t *testing.T) {
			surface, ctx, room := agentReadOnlyChannel(t, true)
			if status != chatpolicy.StatusOpen {
				room.setStatus(status)
			}
			if _, err := surface.Directory(ctx, room.channel); err != nil {
				t.Fatalf("the agents of a %s channel: %v", status, err)
			}
			progress, err := surface.Progress(ctx, room.channel)
			if err != nil || len(progress.Invocations) != 0 {
				t.Fatalf("the member's agent activity in a %s channel: %+v %v", status, progress, err)
			}
			if _, err = surface.OpeningProgress(ctx, room.channel); err != nil {
				t.Fatalf("the opening read in a %s channel: %v", status, err)
			}
			// What the page receives: 200 for the three reads it makes on load.
			handler := personachat.Handler{Surface: surface}
			for _, path := range []string{personachat.Path + "?conversation_id=" + room.channel, personachat.Path + "/invocations?conversation_id=" + room.channel} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
				if response.Code != http.StatusOK {
					t.Fatalf("GET %s in a %s channel = %d %s", path, status, response.Code, response.Body.String())
				}
			}
		})
	}
}

// An archived channel names the agents that answered in it and offers none to
// ask; the member's own activity is read, and asking again there is refused.
func TestAgentUX_ReadOnlyChannelReads_Archived(t *testing.T) {
	surface, ctx, room, invocations, _ := personaSurfaceFixture(t)
	room.posts = append(room.posts, chat.Post{ID: "actual-reply", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "persona.coach", ParentID: "post-a", Revision: 1})
	surface.Receipts = &personaSurfaceReceiptFixture{receipts: []agentinvocationstore.ReplyReceipt{{TenantID: "tenant-a", InvocationID: "invocation-a", InvokerID: "user-a", ConversationID: "channel-a", ThreadID: "post-a", PersonaID: "persona.coach", AgentID: "agent:coach", Display: "People Coach", InvokerHandle: "user-a", PublicPostID: "actual-reply"}}}
	open, err := surface.Directory(ctx, "channel-a")
	if err != nil || len(open.Personas) != 1 || len(open.PostActors) != 1 {
		t.Fatalf("the open channel = %+v %v", open, err)
	}

	room.room.Archived = true
	archived, err := surface.Directory(ctx, "channel-a")
	if err != nil || len(archived.Personas) != 0 || len(archived.PostActors) != 1 || archived.PostActors[0].PostID != "actual-reply" || archived.PostActors[0].Display != "People Coach" {
		t.Fatalf("the archived channel = %+v %v, want no agent to ask and the agent that answered named", archived, err)
	}
	if encoded, _ := json.Marshal(archived); !strings.Contains(string(encoded), `"personas":[]`) {
		t.Fatalf("the archived channel's agent list is not an empty list: %s", encoded)
	}
	progress, err := surface.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].PublicPostID != "actual-reply" {
		t.Fatalf("the member's activity in the archived channel = %+v %v", progress, err)
	}
	// Nothing can be started there: asking again and sharing are refused, and no
	// message is sent.
	if _, err = surface.Retry(ctx, invocations.rows[0].ID, "retry-key-0001"); !errors.Is(err, personachat.ErrDenied) || len(room.sends) != 0 {
		t.Fatalf("asking again in an archived channel = %v (%d messages sent)", err, len(room.sends))
	}
	if _, _, err = surface.member(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("an archived channel is open to actions: %v", err)
	}
	// A member who left reads nothing.
	room.members = nil
	if _, err = surface.Directory(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("a former member read the archived channel's agents: %v", err)
	}
	if _, err = surface.Progress(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("a former member read the archived channel's activity: %v", err)
	}
}

// The other side: the reads stay closed to someone who is not a member, in
// every status, and an archived channel starts nothing.
func TestAgentUX_ReadOnlyChannelReads_Security(t *testing.T) {
	for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusOpen, chatpolicy.StatusAnnouncements, chatpolicy.StatusLocked, chatpolicy.StatusArchived} {
		surface, ctx, room := agentReadOnlyChannel(t, false)
		if status != chatpolicy.StatusOpen {
			room.setStatus(status)
		}
		if _, err := surface.Directory(ctx, room.channel); !errors.Is(err, personachat.ErrDenied) {
			t.Fatalf("%s: a non-member read the agents: %v", status, err)
		}
		if _, err := surface.Progress(ctx, room.channel); !errors.Is(err, personachat.ErrDenied) {
			t.Fatalf("%s: a non-member read the activity: %v", status, err)
		}
		if _, err := surface.OpeningProgress(ctx, room.channel); !errors.Is(err, personachat.ErrDenied) {
			t.Fatalf("%s: a non-member read the opening activity: %v", status, err)
		}
	}
}
