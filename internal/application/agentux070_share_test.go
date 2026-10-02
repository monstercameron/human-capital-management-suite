package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// agentUX070SourceAccess lets a reader open the document a Sources line links,
// as the served product's document hub does for a reader with a grant.
type agentUX070SourceAccess struct{}

func (agentUX070SourceAccess) ResolveAgentDocumentSource(_ context.Context, _ chat.Principal, _, _ string, source chat.AgentDocumentSource) (chat.AgentDocumentSource, error) {
	source.Readable = source.Href != ""
	return source, nil
}

type agentUX070Invocations struct{ invocation agentinvoke.Invocation }

func (f agentUX070Invocations) ListPersonaInvocations(context.Context, string, string, string) ([]agentinvoke.Invocation, error) {
	return []agentinvoke.Invocation{f.invocation}, nil
}

func (f agentUX070Invocations) Lookup(_ context.Context, tenant, owner, id string) (agentinvoke.Invocation, error) {
	if tenant != f.invocation.TenantID || owner != f.invocation.InvokerID || id != f.invocation.ID {
		return agentinvoke.Invocation{}, chat.ErrNotFound
	}
	return f.invocation, nil
}

type agentUX070ShareOptions struct {
	alwaysPrivate bool
	unreadableBy  map[string]bool
	invoker       string
}

// shareSurface is the served surface with the check it is composed with: the
// agent's own setting by the function every public answer uses, the audience
// of the channel and the document decision for every member.
func (r *agentUX070Room) shareSurface(options agentUX070ShareOptions) *PersonaChatSurface {
	r.t.Helper()
	revision, err := r.store.AudienceRevision(r.ctx, "tenant-a", r.channel)
	if err != nil {
		r.t.Fatal(err)
	}
	members := []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "owner"}, {TenantID: "tenant-a", SubjectID: "employee"}}
	snapshot := chatrecipient.AudienceSnapshot{TenantID: "tenant-a", ConversationID: r.channel, Revision: revision, CurrentMembers: members, EligibilityPopulation: members, Complete: true, EligibilityComplete: true, GuestAndExternalComplete: true}
	profile := agentUX070Profile(r.t, options.alwaysPrivate)
	gate := &PersonaAnswerShareGate{
		Profile: func(_ context.Context, invocation agentinvoke.Invocation, _ chat.Conversation) error {
			return profile(personaShareIdentity(invocation))
		},
		Audience:    proactiveAudience{snapshot: snapshot},
		Documents:   proactiveDocumentAuthority{denied: options.unreadableBy},
		BodyClasses: &runtimeReplyClassFake{class: dlp.ClassInternal},
		Cited: func(_ context.Context, _, _ string, source personaShareSource) (AgentAnnouncementResolvedDocument, error) {
			return AgentAnnouncementResolvedDocument{DocumentID: source.DocumentID, Version: source.VersionID, Digest: personaRunT0ToolOutputDigest([]byte("Employees carry over 40 hours."))}, nil
		},
	}
	invoker := options.invoker
	if invoker == "" {
		invoker = "owner"
	}
	invocation := agentinvoke.Invocation{ID: r.question.ID, TenantID: "tenant-a", ConversationID: r.channel, ThreadID: r.question.ID, PostID: r.question.ID, InvokerID: invoker, PersonaID: "policy-helper", PersonaVersion: "1", InstallationID: "install"}
	return &PersonaChatSurface{Chat: r.service, Invocations: agentUX070Invocations{invocation: invocation}, Receipts: r.receipts, Share: gate}
}

// TestAgentUXPublicAnswer_Default_Integration (share afterwards): the asker of a private answer posts it
// to the channel from the card. The channel then holds one message under the
// question, the same to everyone; asking again changes nothing.
func TestAgentUXPublicAnswer_Default_Integration(t *testing.T) {
	room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over? Just for me."})
	receipt, err := room.deliver(agentUX070Delivery{})
	room.assertPrivate(receipt, err, chat.PrivateReasonAsked)
	surface := room.shareSurface(agentUX070ShareOptions{})
	shared, err := surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0001")
	if err != nil || shared.PostID == "" || shared.InvocationID != room.question.ID {
		t.Fatalf("share = %+v, %v", shared, err)
	}
	for _, subject := range []string{"owner", "employee"} {
		posts := room.channelPosts(subject)
		if len(posts) != 2 || posts[1].ID != shared.PostID || posts[1].ParentID != room.question.ID || posts[1].AuthorID != "owner" {
			t.Fatalf("%s did not see the shared answer under the question: %+v", subject, posts)
		}
		body := posts[1].Body
		if !strings.Contains(body, "40 hours") || !strings.Contains(body, "\n\nSources\n- [Paid time off policy") || !strings.Contains(body, "document=pto-policy&version=1") || strings.Contains(body, "chat.agent.private") || strings.Contains(body, "source.readable") {
			t.Fatalf("%s saw a shared answer without its sources, or with the card's markers: %s", subject, body)
		}
	}
	again, err := surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0002")
	if err != nil || again.PostID != shared.PostID {
		t.Fatalf("sharing twice posted twice: %+v %v", again, err)
	}
	if posts := room.channelPosts("employee"); len(posts) != 2 {
		t.Fatalf("channel holds %d messages after sharing twice", len(posts))
	}
}

// TestAgentUXPublicAnswer_Default_Security covers each reason the
// card cannot be shared: the agent is strict, a source is not open to every
// member, and the caller is not the person who asked.
func TestAgentUXPublicAnswer_Default_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		options agentUX070ShareOptions
		state   string
		denied  error
	}{
		"strict agent":         {options: agentUX070ShareOptions{alwaysPrivate: true}, state: chat.PrivateReasonAgent},
		"unreadable source":    {options: agentUX070ShareOptions{unreadableBy: map[string]bool{"employee": true}}, state: chat.PrivateReasonAudience},
		"another person asked": {options: agentUX070ShareOptions{invoker: "employee"}, denied: personachat.ErrDenied},
	} {
		t.Run(name, func(t *testing.T) {
			room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over? Keep this private."})
			receipt, err := room.deliver(agentUX070Delivery{})
			room.assertPrivate(receipt, err, chat.PrivateReasonAsked)
			_, err = room.shareSurface(tc.options).ShareAnswer(room.ctx, room.question.ID, "share-key-0001")
			var conflict *personachat.FinalStateConflict
			switch {
			case tc.denied != nil && !errors.Is(err, tc.denied):
				t.Fatalf("error = %v, want %v", err, tc.denied)
			case tc.denied == nil && (!errors.As(err, &conflict) || conflict.State != tc.state):
				t.Fatalf("error = %v, want a refusal naming %q", err, tc.state)
			}
			if posts := room.channelPosts("employee"); len(posts) != 1 {
				t.Fatalf("a refused share reached the channel: %+v", posts)
			}
		})
	}
}

// TestTodo_AGENTUX_070_ShareSources reads a card's Sources the way sharing does.
func TestTodo_AGENTUX_070_ShareSources(t *testing.T) {
	card := "40 hours carry over.\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=d1&version=v1#carryover) <!--chat.agent.source.readable:true-->\n- [Handbook](/workspace/app/docs?document=d2&version=v9)"
	answer, sources, ok := personaShareSources(card)
	if !ok || answer != "40 hours carry over." || len(sources) != 2 || sources[0] != (personaShareSource{DocumentID: "d1", VersionID: "v1", Anchor: "carryover"}) || sources[1] != (personaShareSource{DocumentID: "d2", VersionID: "v9"}) {
		t.Fatalf("sources = %+v %q %v", sources, answer, ok)
	}
	for name, body := range map[string]string{
		"no sources":             "40 hours carry over.",
		"a source shown as text": "40 hours.\n\nSources\n- Executive succession plan <!--chat.agent.source.readable:false-->",
		"not a document":         "40 hours.\n\nSources\n- [Elsewhere](https://example.com/page)",
		"no version":             "40 hours.\n\nSources\n- [Handbook](/workspace/app/docs?document=d2)",
	} {
		if _, _, ok := personaShareSources(body); ok {
			t.Errorf("%s: a card that cannot be shown to be readable was accepted", name)
		}
	}
}

// TestAgentUXPublicAnswer_Default_ProgressNamesThePublicAnswer: the person who
// asked is told which message in the channel answered their question, so the
// rating controls belong to that message; nobody else's receipt is read.
func TestAgentUXPublicAnswer_Default_ProgressNamesThePublicAnswer(t *testing.T) {
	s, ctx, _, _, _ := personaSurfaceFixture(t)
	s.Receipts = &personaSurfaceReceiptFixture{receipts: []agentinvocationstore.ReplyReceipt{
		{TenantID: "tenant-a", InvocationID: "invocation-a", InvokerID: "someone-else", ConversationID: "channel-a", PublicPostID: "not-mine"},
		{TenantID: "tenant-a", InvocationID: "invocation-a", InvokerID: "user-a", ConversationID: "channel-a", PublicPostID: "public-answer"},
	}}
	progress, err := s.Progress(ctx, "channel-a")
	if err != nil || len(progress.Invocations) != 1 || progress.Invocations[0].PublicPostID != "public-answer" || progress.Invocations[0].PrivatePostID != "" {
		t.Fatalf("progress = %+v %v", progress, err)
	}
}
