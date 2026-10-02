package application

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// chatbug040Cards is the reader of private cards: what one person was
// delivered in one conversation.
type chatbug040Cards struct {
	cards []chat.EphemeralPost
	reads []chat.ListEphemeralPostsRequest
	err   error
}

func (c *chatbug040Cards) ListEphemeralPosts(_ context.Context, r chat.ListEphemeralPostsRequest) ([]chat.EphemeralPost, uint64, error) {
	c.reads = append(c.reads, r)
	if c.err != nil {
		return nil, r.AfterSequence, c.err
	}
	var out []chat.EphemeralPost
	next := r.AfterSequence
	for _, card := range c.cards {
		if card.Sequence > r.AfterSequence {
			out = append(out, card)
			next = max(next, card.Sequence)
		}
	}
	return out, next, nil
}

func chatbug040Surface(t *testing.T) (*PersonaChatSurface, context.Context, *chatbug040Cards) {
	t.Helper()
	surface, ctx, _, _, _ := personaSurfaceFixture(t)
	at := surface.Now()
	card := func(id, thread, recipient string, sequence uint64) chat.EphemeralPost {
		return chat.EphemeralPost{ID: id, TenantID: "tenant-a", ConversationID: "channel-a", ThreadID: thread, RecipientHomeTenantID: "tenant-a", RecipientSubjectID: recipient, Body: "Answer " + id, OnlyVisibleToYou: true, CreatedAt: at.Add(time.Duration(sequence) * time.Minute), ExpiresAt: at.Add(24 * time.Hour), ThreadLink: "/chat/share/" + id, DurableCopyConversationID: "private-dm", DurableCopyPostID: "copy-" + id, Sequence: sequence}
	}
	cards := &chatbug040Cards{cards: []chat.EphemeralPost{
		card("card-late", "post-b", "user-a", 4),
		card("card-mine", "post-a", "user-a", 2),
		// Delivered to somebody else: the reader refuses these, and the surface
		// would not send one if it were handed it.
		card("card-theirs", "post-a", "user-b", 3),
		// The caller's, but named by no receipt of the caller's runs.
		card("card-unreceipted", "post-a", "user-a", 5),
	}}
	surface.Cards = cards
	surface.Receipts = &personaSurfaceReceiptFixture{receipts: []agentinvocationstore.ReplyReceipt{
		{TenantID: "tenant-a", InvocationID: "invocation-a", InvokerID: "user-a", ConversationID: "channel-a", EphemeralPostID: "card-mine"},
		{TenantID: "tenant-a", InvocationID: "invocation-b", InvokerID: "user-a", ConversationID: "channel-a", EphemeralPostID: "card-late"},
		{TenantID: "tenant-a", InvocationID: "invocation-c", InvokerID: "user-b", ConversationID: "channel-a", EphemeralPostID: "card-theirs"},
		{TenantID: "tenant-a", InvocationID: "invocation-d", InvokerID: "user-a", ConversationID: "another-room", EphemeralPostID: "card-unreceipted"},
	}}
	return surface, ctx, cards
}

// The first read of a conversation's activity carries the caller's stored
// private answers in one batch, so a finished question is drawn answered at
// once. The reads that follow do not carry them, and nobody else's answer is
// ever sent.
func TestTodo_CHATBUG_040(t *testing.T) {
	surface, ctx, cards := chatbug040Surface(t)

	opening, err := surface.OpeningProgress(ctx, "channel-a")
	if err != nil || len(opening.Invocations) != 1 {
		t.Fatalf("opening read = %+v %v", opening, err)
	}
	if len(opening.Answers) != 2 || opening.Answers[0].ID != "card-mine" || opening.Answers[1].ID != "card-late" {
		t.Fatalf("answers = %+v, want the caller's two receipted answers, oldest first", opening.Answers)
	}
	first := opening.Answers[0]
	if first.ThreadID != "post-a" || first.Body != "Answer card-mine" || first.ThreadLink != "/chat/share/card-mine" || first.CreatedAt.IsZero() || !first.ExpiresAt.After(first.CreatedAt) {
		t.Fatalf("answer = %+v", first)
	}
	encoded, _ := json.Marshal(opening)
	for _, hidden := range []string{"card-theirs", "card-unreceipted", "user-b", "private-dm", "copy-card-mine", "recipient"} {
		if strings.Contains(string(encoded), hidden) {
			t.Fatalf("the opening read discloses %q: %s", hidden, encoded)
		}
	}
	if len(cards.reads) == 0 || cards.reads[0].Principal.SubjectID != "user-a" || cards.reads[0].ConversationID != "channel-a" {
		t.Fatalf("the cards were read as %+v, want the caller in this conversation", cards.reads)
	}

	// The read the watch repeats every second holds no answers.
	repeated, err := surface.Progress(ctx, "channel-a")
	if err != nil || len(repeated.Invocations) != 1 || len(repeated.Answers) != 0 {
		t.Fatalf("the repeated read = %+v %v", repeated, err)
	}
	if encoded, _ = json.Marshal(repeated); strings.Contains(string(encoded), `"answers"`) {
		t.Fatalf("the repeated read names answers: %s", encoded)
	}

	// A failed read of the cards still answers with the activity: the page keeps
	// its placeholder and the answers arrive on the stream.
	cards.err = chat.ErrUnavailable
	if opening, err = surface.OpeningProgress(ctx, "channel-a"); err != nil || len(opening.Invocations) != 1 || len(opening.Answers) != 0 {
		t.Fatalf("with the card reader down: %+v %v", opening, err)
	}
	cards.err = nil

	// Somebody who is not a member is refused before anything is read.
	surface.Chat.(*personaSurfaceChatFixture).members = nil
	cards.reads = nil
	if _, err = surface.OpeningProgress(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) || len(cards.reads) != 0 {
		t.Fatalf("a non-member's opening read = %v (%d card reads)", err, len(cards.reads))
	}
}

// What the page receives: the plain read and the first event of the watch hold
// the answers; the watch's next event does not repeat them.
func TestTodo_CHATBUG_040_Browser(t *testing.T) {
	surface, ctx, _ := chatbug040Surface(t)
	handler := personachat.Handler{Surface: surface, WatchInterval: 10 * time.Millisecond}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, personachat.Path+"/invocations?conversation_id=channel-a", nil).WithContext(ctx))
	var read personachat.Progress
	if err := json.Unmarshal(response.Body.Bytes(), &read); err != nil || response.Code != http.StatusOK || len(read.Invocations) != 1 || len(read.Answers) != 2 {
		t.Fatalf("read = %d %s %v", response.Code, response.Body.String(), err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r.WithContext(ctx)) }))
	defer server.Close()
	watchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(watchCtx, http.MethodGet, server.URL+personachat.Path+"/invocations?conversation_id=channel-a&watch=1", nil)
	stream, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	lines := bufio.NewScanner(stream.Body)
	lines.Buffer(make([]byte, 4096), 1<<20)
	var events []personachat.Progress
	keepalives := 0
	for lines.Scan() && keepalives < 3 {
		line := lines.Text()
		if strings.HasPrefix(line, ": keepalive") {
			keepalives++
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event personachat.Progress
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &event); err != nil {
			t.Fatalf("event %q: %v", line, err)
		}
		events = append(events, event)
	}
	// Three idle ticks after the first event: nothing changed, so nothing but
	// keepalives was sent, and the answers were sent once.
	if len(events) != 1 || len(events[0].Invocations) != 1 || len(events[0].Answers) != 2 || keepalives != 3 {
		t.Fatalf("the watch sent %d events (%+v) and %d keepalives, want the opening event once", len(events), events, keepalives)
	}
}
