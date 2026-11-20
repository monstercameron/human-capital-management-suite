package application

import (
	"context"
	"errors"
	"testing"
	"time"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

type issue3808Sessions struct {
	row   clockservice.MissingPunchSession
	err   error
	calls int
}

func (s *issue3808Sessions) GetMissingPunchSession(_ context.Context, tenant, id string) (clockservice.MissingPunchSession, error) {
	s.calls++
	if s.err != nil || s.row.TenantID != tenant || s.row.ID != id {
		return clockservice.MissingPunchSession{}, errors.Join(chat.ErrNotFound, s.err)
	}
	return s.row, nil
}

type issue3808People struct {
	chatSubject string
	reviewers   []string
	noIdentity  bool
}

func (p issue3808People) ChatSubject(context.Context, string, string) (string, bool, error) {
	return p.chatSubject, !p.noIdentity, nil
}
func (p issue3808People) Reviewers(context.Context, string, string) ([]string, error) {
	return p.reviewers, nil
}

// issue3808Poster keeps what the store keeps: a post per idempotency key.
type issue3808Poster struct {
	posts map[string]chat.ClockCardPost
	order []string
	calls int
	err   error
}

func (p *issue3808Poster) PostClockCard(_ context.Context, r chat.ClockCardPost) (chat.Post, error) {
	p.calls++
	if p.err != nil {
		return chat.Post{}, p.err
	}
	if p.posts == nil {
		p.posts = map[string]chat.ClockCardPost{}
	}
	key := r.Recipient + "\x00" + r.IdempotencyKey
	if _, seen := p.posts[key]; !seen {
		p.posts[key] = r
		p.order = append(p.order, key)
	}
	return chat.Post{ID: key}, nil
}

func issue3808Fixture() (*ClockChatCards, *issue3808Sessions, *issue3808Poster) {
	sessions := &issue3808Sessions{row: clockservice.MissingPunchSession{TenantID: "t1", ID: "sess-1", WorkerRef: "w-maria", Status: "OPEN", MissingOut: true}}
	poster := &issue3808Poster{}
	cards := &ClockChatCards{
		Sessions: sessions,
		People:   issue3808People{chatSubject: "maria", reviewers: []string{"sam"}},
		Poster:   poster,
		Sender:   func(tenant string) chat.Principal { return chat.Principal{TenantID: tenant, SubjectID: "system:time"} },
		Now:      func() time.Time { return time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC) },
	}
	return cards, sessions, poster
}

func issue3808Attempt(seq int64, kind string) clockservice.ClockWebhookAttempt {
	at := time.Date(2026, 11, 17, 22, 30, 0, 0, time.UTC)
	return clockservice.ClockWebhookAttempt{
		Delivery: subscription.DeliveryAttempt{OperationKey: "op-" + kind, Request: subscription.DeliveryRequest{Destination: "chat"}},
		Event: clockservice.ClockEvent{Tenant: "t1", Sequence: seq, EventType: clockservice.ClockExceptionRaised,
			Payload:  map[string]any{"subject_id": "sess-1", "subject_kind": "clock_session", "kind": kind, "detail_ref": "d"},
			Envelope: subscription.CanonicalEnvelope{Tenant: "t1", EffectiveAt: at, KnownAt: at}},
	}
}

func TestIssue3808_MissingPunchEventPostsCardsToWorkerAndSupervisor(t *testing.T) {
	cards, _, poster := issue3808Fixture()
	receipt, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(7, "MISSING_OUT"))
	if err != nil || !receipt.Accepted {
		t.Fatalf("deliver = %+v, %v", receipt, err)
	}
	if len(poster.order) != 2 {
		t.Fatalf("%d cards posted, want one for the worker and one for the supervisor", len(poster.order))
	}
	byRecipient := map[string]chat.ClockCard{}
	for _, post := range poster.posts {
		if post.Sender.SubjectID != "system:time" || post.TenantID != "t1" {
			t.Errorf("post = %+v, want the system sender in the tenant", post)
		}
		byRecipient[post.Recipient] = post.Card
	}
	want := map[string]chat.ClockCard{
		"maria": {Kind: chat.ClockCardMissingPunch, Audience: chat.ClockCardSelf, Subject: "maria", Day: "2026-11-17", Ref: "sess-1"},
		"sam":   {Kind: chat.ClockCardMissingPunch, Audience: chat.ClockCardReview, Subject: "maria", Day: "2026-11-17", Ref: "sess-1"},
	}
	for who, card := range want {
		if byRecipient[who] != card {
			t.Errorf("card for %s = %+v, want %+v", who, byRecipient[who], card)
		}
	}
}

// Replaying the same outbox page must not post a second card to anyone.
func TestIssue3808_ReplayedEventPostsEachCardOnce(t *testing.T) {
	cards, _, poster := issue3808Fixture()
	for i := 0; i < 3; i++ {
		if _, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(7, "MISSING_OUT")); err != nil {
			t.Fatal(err)
		}
	}
	if len(poster.posts) != 2 || poster.calls != 6 {
		t.Fatalf("%d cards from %d calls, want 2 cards however often the event is replayed", len(poster.posts), poster.calls)
	}
	if _, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(8, "MISSING_OUT")); err != nil {
		t.Fatal(err)
	}
	if len(poster.posts) != 4 {
		t.Fatalf("%d cards after a second event, want 4", len(poster.posts))
	}
}

func TestIssue3808_OtherEventsAreAcceptedWithoutACard(t *testing.T) {
	cards, sessions, poster := issue3808Fixture()
	other := issue3808Attempt(9, "SEQUENCE_GAP")
	notException := issue3808Attempt(10, "MISSING_OUT")
	notException.Event.EventType = clockservice.ClockPunchAccepted
	device := issue3808Attempt(11, "MISSING_OUT")
	device.Event.Payload["subject_kind"] = "clock_device"
	for _, a := range []clockservice.ClockWebhookAttempt{other, notException, device} {
		receipt, err := cards.DeliverClockEvent(context.Background(), a)
		if err != nil || !receipt.Accepted {
			t.Fatalf("deliver %s = %+v, %v", a.Event.EventType, receipt, err)
		}
	}
	if poster.calls != 0 || sessions.calls != 0 {
		t.Fatalf("posted %d, read %d sessions; an unrelated event needs neither", poster.calls, sessions.calls)
	}
}

// The payload is a claim; the session is the fact. A resolved session, another
// tenant's session or a worker without a chat identity gets no card.
func TestIssue3808_NoCardWhenTheSessionIsNotMissingAPunch_Security(t *testing.T) {
	for name, mutate := range map[string]func(*ClockChatCards, *issue3808Sessions){
		"already fixed":  func(_ *ClockChatCards, s *issue3808Sessions) { s.row.MissingOut = false },
		"closed session": func(_ *ClockChatCards, s *issue3808Sessions) { s.row.Status = "CLOSED" },
		"other tenant":   func(_ *ClockChatCards, s *issue3808Sessions) { s.row.TenantID = "t2" },
		"no chat identity": func(c *ClockChatCards, _ *issue3808Sessions) {
			c.People = issue3808People{noIdentity: true, reviewers: []string{"sam"}}
		},
		"worker has no row": func(_ *ClockChatCards, s *issue3808Sessions) { s.row.WorkerRef = "" },
	} {
		cards, sessions, poster := issue3808Fixture()
		mutate(cards, sessions)
		if _, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(7, "MISSING_OUT")); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if poster.calls != 0 {
			t.Errorf("%s: %d cards posted, want none", name, poster.calls)
		}
	}
}

// A card goes to the worker or to someone in the worker's reporting line, and
// to nobody else, whatever asks.
func TestIssue3808_RecipientOutsideTheReportingLineIsRefused_Security(t *testing.T) {
	cards, _, poster := issue3808Fixture()
	err := cards.PostMissingPunch(context.Background(), MissingPunchNotice{Tenant: "t1", SessionID: "sess-1", WorkerRef: "w-maria", Day: "2026-11-17", Recipient: "stranger", EventKey: "k"})
	if !errors.Is(err, ErrClockCardRecipientDenied) || poster.calls != 0 {
		t.Fatalf("err = %v, posts %d; want a refusal before any post", err, poster.calls)
	}
	if err := cards.PostMissingPunch(context.Background(), MissingPunchNotice{Tenant: "t1", SessionID: "sess-1", WorkerRef: "w-maria", Day: "2026-11-17", Recipient: "sam", EventKey: "k"}); err != nil || poster.calls != 1 {
		t.Fatalf("a reviewer in the line: err %v, posts %d", err, poster.calls)
	}
}

func TestIssue3808_TenantBudgetStopsAFloodAndTheEventCanBeRetried(t *testing.T) {
	cards, _, poster := issue3808Fixture()
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	cards.Now = func() time.Time { return now }
	cards.Budget = NewClockCardBudget(2)
	for seq := int64(1); seq <= 2; seq++ {
		if _, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(seq, "MISSING_OUT")); err != nil {
			t.Fatal(err)
		}
	}
	before := poster.calls
	_, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(3, "MISSING_OUT"))
	if !errors.Is(err, ErrClockCardBudget) || poster.calls != before {
		t.Fatalf("third event: err %v, posts %d -> %d; want the budget refusal and no post", err, before, poster.calls)
	}
	now = now.Add(61 * time.Second)
	if _, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(3, "MISSING_OUT")); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestIssue3808_PosterFailureIsRetriedAndARefusedRecipientIsSkipped(t *testing.T) {
	cards, _, poster := issue3808Fixture()
	poster.err = chat.ErrUnavailable
	if _, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(7, "MISSING_OUT")); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("unavailable chat: err %v, want it returned so the delivery is retried", err)
	}
	poster.err = chat.ErrPermissionDenied
	if receipt, err := cards.DeliverClockEvent(context.Background(), issue3808Attempt(7, "MISSING_OUT")); err != nil || !receipt.Accepted {
		t.Fatalf("a recipient chat refuses: %+v, %v; want it skipped so the cursor can advance", receipt, err)
	}
}
