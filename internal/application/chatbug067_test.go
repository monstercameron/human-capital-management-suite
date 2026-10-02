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
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// chatbug067ServedChat is the chat service the served composition hands the
// surface: a decorator that passes on the conversation's own methods and
// nothing else, so the read of a person's private cards is not among them.
type chatbug067ServedChat struct{ personaSurfaceChat }

// chatbug067NoCard is a receipt store that holds no private card for anybody.
type chatbug067NoCard struct{ personaReplyReceiptStore }

func (chatbug067NoCard) ListReplyReceipts(context.Context, string, string, string) ([]agentinvocationstore.ReplyReceipt, error) {
	return nil, nil
}

func chatbug067Room(t *testing.T) *agentUX070Room {
	t.Helper()
	room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over? Just for me."})
	receipt, err := room.deliver(agentUX070Delivery{})
	room.assertPrivate(receipt, err, chat.PrivateReasonAsked)
	return room
}

// Sharing was refused for every answer on the served product because the
// surface could not read the private card through the served chat service. It
// reads it through the port it is composed with, and a refusal that remains
// says why.
func TestTodo_CHATBUG_067(t *testing.T) {
	// The served shape without the port: unavailable, not "denied". The asker is
	// not refused for something that is the composition's to provide.
	room := chatbug067Room(t)
	surface := room.shareSurface(agentUX070ShareOptions{})
	surface.Chat = chatbug067ServedChat{room.service}
	if _, err := surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0001"); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("without a way to read the card, share = %v, want unavailable", err)
	}
	// The served shape with the port: the answer is posted under the question.
	surface.Cards = room.service
	shared, err := surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0002")
	if err != nil || shared.PostID == "" {
		t.Fatalf("the served surface cannot share a private answer: %+v %v", shared, err)
	}
	if posts := room.channelPosts("employee"); len(posts) != 2 || posts[1].ID != shared.PostID || posts[1].ParentID != room.question.ID {
		t.Fatalf("the shared answer is not under the question for the channel: %+v", posts)
	}

	// A source one member may not open: the refusal names the source.
	room = chatbug067Room(t)
	surface = room.shareSurface(agentUX070ShareOptions{unreadableBy: map[string]bool{"employee": true}})
	cited := surface.Share.Cited
	surface.Share.Cited = func(ctx context.Context, tenant, subject string, source personaShareSource) (AgentAnnouncementResolvedDocument, error) {
		document, err := cited(ctx, tenant, subject, source)
		document.Title = "Paid time off policy"
		return document, err
	}
	_, err = surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0003")
	var conflict *personachat.FinalStateConflict
	if !errors.As(err, &conflict) || conflict.State != chat.PrivateReasonAudience || conflict.Detail != "Paid time off policy" {
		t.Fatalf("a source not open to everyone: %v (%+v), want the audience refusal naming the source", err, conflict)
	}
	if posts := room.channelPosts("employee"); len(posts) != 1 {
		t.Fatalf("a refused share reached the channel: %+v", posts)
	}

	// An agent set to answer privately: that reason, and no source.
	room = chatbug067Room(t)
	surface = room.shareSurface(agentUX070ShareOptions{alwaysPrivate: true})
	if _, err = surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0004"); !errors.As(err, &conflict) || conflict.State != chat.PrivateReasonAgent || conflict.Detail != "" {
		t.Fatalf("a private agent: %v (%+v)", err, conflict)
	}

	// An answer whose private card is no longer held: too old to share.
	room = chatbug067Room(t)
	surface = room.shareSurface(agentUX070ShareOptions{})
	surface.Receipts = chatbug067NoCard{surface.Receipts}
	if _, err = surface.ShareAnswer(room.ctx, room.question.ID, "share-key-0005"); !errors.As(err, &conflict) || conflict.State != personaShareExpired {
		t.Fatalf("an answer with no card left: %v, want the expired refusal", err)
	}
}

// The reason reaches the page: the HTTP boundary answers 409 with the state and
// the named source, and 503 when sharing is not available at all.
func TestTodo_CHATBUG_067_Browser(t *testing.T) {
	room := chatbug067Room(t)
	surface := room.shareSurface(agentUX070ShareOptions{unreadableBy: map[string]bool{"employee": true}})
	cited := surface.Share.Cited
	surface.Share.Cited = func(ctx context.Context, tenant, subject string, source personaShareSource) (AgentAnnouncementResolvedDocument, error) {
		document, err := cited(ctx, tenant, subject, source)
		document.Title = "Paid time off policy"
		return document, err
	}
	post := func() (*httptest.ResponseRecorder, map[string]string) {
		request := httptest.NewRequest(http.MethodPost, personachat.Path+"/invocations/"+room.question.ID+"/share", strings.NewReader(`{"idempotency_key":"share-key-http"}`)).WithContext(room.ctx)
		response := httptest.NewRecorder()
		personachat.Handler{Surface: surface}.ServeHTTP(response, request)
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("response %d %q: %v", response.Code, response.Body.String(), err)
		}
		return response, body
	}
	response, body := post()
	if response.Code != http.StatusConflict || body["error"] != "conflict" || body["state"] != "audience" || body["detail"] != "Paid time off policy" {
		t.Fatalf("refusal = %d %v", response.Code, body)
	}
	surface.Share = nil
	if response, body = post(); response.Code != http.StatusServiceUnavailable || body["error"] != "unavailable" {
		t.Fatalf("no share gate = %d %v", response.Code, body)
	}
}
