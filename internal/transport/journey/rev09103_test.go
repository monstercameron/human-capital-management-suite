package journey_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
)

const (
	rev09103Hidden  = "00000000-0000-4000-8000-000000009103"
	rev09103Visible = "00000000-0000-4000-8000-000000009104"
)

// rev09103Engine is the fixture engine with one journey the caller may not
// see: Inspect refuses it exactly as the real engine refuses a journey
// outside the caller's authority.
type rev09103Engine struct {
	*fakeEngine
	hidden string
}

func (e *rev09103Engine) Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error) {
	if intentID == e.hidden {
		return workspace.JourneyDetail{}, workspace.ErrDenied
	}
	detail, err := e.fakeEngine.Inspect(ctx, intentID)
	detail.Summary.IntentID = intentID
	return detail, err
}

type rev09103Stream struct {
	events chan rev09103Event
}

type rev09103Event struct {
	msg *journeyv1.WatchPromotionInvalidationsResponse
	err error
}

func openInvalidations(t *testing.T, ctx context.Context, client journeyv1.JourneyServiceClient, region string, after uint64) *rev09103Stream {
	t.Helper()
	stream, err := client.WatchPromotionInvalidations(ctx, &journeyv1.WatchPromotionInvalidationsRequest{Region: region, AfterSequence: after})
	if err != nil {
		t.Fatalf("open WatchPromotionInvalidations: %v", err)
	}
	s := &rev09103Stream{events: make(chan rev09103Event, 8)}
	go func() {
		defer close(s.events)
		for {
			msg, err := stream.Recv()
			s.events <- rev09103Event{msg, err}
			if err != nil {
				return
			}
		}
	}()
	return s
}

func (s *rev09103Stream) next(t *testing.T, d time.Duration) (*journeyv1.WatchPromotionInvalidationsResponse, error, bool) {
	t.Helper()
	select {
	case e, ok := <-s.events:
		if !ok {
			return nil, errors.New("stream reader ended"), true
		}
		return e.msg, e.err, true
	case <-time.After(d):
		return nil, nil, false
	}
}

func waitSubscribers(t *testing.T, hub *journeyinvalidation.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for hub.Subscribers(fixtureTenant) != want {
		if time.Now().After(deadline) {
			t.Fatalf("subscribers = %d, want %d", hub.Subscribers(fixtureTenant), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func decodeInvalidation(t *testing.T, msg *journeyv1.WatchPromotionInvalidationsResponse) productquery.InvalidationMessage {
	t.Helper()
	var message productquery.InvalidationMessage
	if err := json.Unmarshal(msg.GetInvalidation(), &message); err != nil {
		t.Fatalf("decode invalidation: %v", err)
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("invalid invalidation on the wire: %v", err)
	}
	return message
}

// TestTodo_REV_091_03 drives the served stream: a committed transition the
// caller may see arrives as one canonical, contiguously numbered hint naming
// the region's subject, and resuming continues the caller's own numbering.
func TestTodo_REV_091_03(t *testing.T) {
	hub := journeyinvalidation.NewHub(journeyinvalidation.Options{})
	engine := &rev09103Engine{fakeEngine: newFakeEngine(), hidden: rev09103Hidden}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Invalidations: hub}))

	stream := openInvalidations(t, authorizedContext(t), client, string(promotion.RegionShellCount), 4)
	waitSubscribers(t, hub, 1)
	hub.Publish(journeyinvalidation.Committed{Tenant: fixtureTenant, IntentID: rev09103Visible, Revision: 12})
	msg, err, ok := stream.next(t, 5*time.Second)
	if !ok || err != nil {
		t.Fatalf("first hint: ok=%t err=%v", ok, err)
	}
	message := decodeInvalidation(t, msg)
	if message.SourceSequence != 5 || message.Watermark != 4 {
		t.Fatalf("sequence = %d/%d, want 5/4 continuing after_sequence 4", message.SourceSequence, message.Watermark)
	}
	if len(message.Items) != 1 || message.Items[0].Subject != promotion.ShellCountSubject(fixtureTenant) || message.Items[0].Revision != 12 {
		t.Fatalf("items = %+v", message.Items)
	}

	t.Run("detail region names the journey", func(t *testing.T) {
		detail := openInvalidations(t, authorizedContext(t), client, string(promotion.RegionDetail), 0)
		waitSubscribers(t, hub, 2)
		hub.Publish(journeyinvalidation.Committed{Tenant: fixtureTenant, IntentID: rev09103Visible, Revision: 13})
		msg, err, ok := detail.next(t, 5*time.Second)
		if !ok || err != nil {
			t.Fatalf("detail hint: ok=%t err=%v", ok, err)
		}
		got := decodeInvalidation(t, msg)
		if got.SourceSequence != 1 || got.Items[0].Subject != journeyinvalidation.JourneyRef(fixtureTenant, rev09103Visible) {
			t.Fatalf("detail hint = %+v", got)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			deps journey.Dependencies
			req  *journeyv1.WatchPromotionInvalidationsRequest
			want envelope.Code
		}{
			{"unknown region", journey.Dependencies{Engine: engine, Invalidations: hub}, &journeyv1.WatchPromotionInvalidationsRequest{Region: "EVERYTHING"}, envelope.CodeInvalidArgument},
			{"resume out of range", journey.Dependencies{Engine: engine, Invalidations: hub}, &journeyv1.WatchPromotionInvalidationsRequest{Region: "SHELL_COUNT", AfterSequence: journeyinvalidation.MaxAfterSequence + 1}, envelope.CodeInvalidArgument},
			{"no hub", journey.Dependencies{Engine: engine}, &journeyv1.WatchPromotionInvalidationsRequest{Region: "SHELL_COUNT"}, envelope.CodeUnavailable},
			{"no engine", journey.Dependencies{Invalidations: hub}, &journeyv1.WatchPromotionInvalidationsRequest{Region: "SHELL_COUNT"}, envelope.CodeUnavailable},
		} {
			c := dialJourneyClient(startTestServer(t, tc.deps))
			s, err := c.WatchPromotionInvalidations(authorizedContext(t), tc.req)
			if err == nil {
				_, err = s.Recv()
			}
			assertOwnedCode(t, err, tc.want)
		}
	})

	t.Run("lagging reader is told to reconnect", func(t *testing.T) {
		small := journeyinvalidation.NewHub(journeyinvalidation.Options{Buffer: 1})
		gate := make(chan struct{})
		slow := &rev09103SlowEngine{rev09103Engine: engine, gate: gate}
		c := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: slow, Invalidations: small}))
		lagging := openInvalidations(t, authorizedContext(t), c, "SHELL_COUNT", 0)
		deadline := time.Now().Add(5 * time.Second)
		for small.Subscribers(fixtureTenant) != 1 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		// The first record is taken and blocks in Inspect; the second fills
		// the buffer; the third overflows it.
		for revision := uint64(1); revision <= 3; revision++ {
			small.Publish(journeyinvalidation.Committed{Tenant: fixtureTenant, IntentID: rev09103Visible, Revision: revision})
			time.Sleep(20 * time.Millisecond)
		}
		close(gate)
		for {
			msg, err, ok := lagging.next(t, 5*time.Second)
			if !ok {
				t.Fatal("lagging stream neither delivered nor ended")
			}
			if err != nil {
				assertOwnedCode(t, err, envelope.CodeAborted)
				return
			}
			_ = msg
		}
	})
}

type rev09103SlowEngine struct {
	*rev09103Engine
	gate chan struct{}
}

func (e *rev09103SlowEngine) Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error) {
	select {
	case <-e.gate:
	case <-ctx.Done():
		return workspace.JourneyDetail{}, ctx.Err()
	}
	return e.rev09103Engine.Inspect(ctx, intentID)
}

// TestTodo_REV_091_03_Security proves the served stream leaks nothing: a
// journey the caller may not see never reaches them and never consumes a
// sequence number; a caller whose roles admit no relationship-scoped access
// receives nothing even for a journey the engine would show; and a caller
// whose page access grants neither Journeys nor My Work cannot subscribe.
func TestTodo_REV_091_03_Security(t *testing.T) {
	hub := journeyinvalidation.NewHub(journeyinvalidation.Options{})
	engine := &rev09103Engine{fakeEngine: newFakeEngine(), hidden: rev09103Hidden}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Invalidations: hub}))

	admin := openInvalidations(t, authorizedContext(t), client, "PERSON", 0)
	// The fixture manager holds only intent_author: no role admits
	// relationship-scoped access to another worker's record.
	unscoped := openInvalidations(t, testContext(t), client, "PERSON", 0)
	waitSubscribers(t, hub, 2)

	hub.Publish(journeyinvalidation.Committed{Tenant: fixtureTenant, IntentID: rev09103Hidden, Revision: 1})
	hub.Publish(journeyinvalidation.Committed{Tenant: fixtureTenant, IntentID: rev09103Visible, Revision: 2})
	hub.Publish(journeyinvalidation.Committed{Tenant: values.TenantId("another-tenant"), IntentID: rev09103Visible, Revision: 3})

	msg, err, ok := admin.next(t, 5*time.Second)
	if !ok || err != nil {
		t.Fatalf("admin hint: ok=%t err=%v", ok, err)
	}
	got := decodeInvalidation(t, msg)
	if got.SourceSequence != 1 || got.Watermark != 0 || got.Items[0].Revision != 2 {
		t.Fatalf("admin hint = seq %d/%d revision %d; the hidden journey leaked or left a gap", got.SourceSequence, got.Watermark, got.Items[0].Revision)
	}
	if msg, err, ok := admin.next(t, 300*time.Millisecond); ok {
		t.Fatalf("admin received a second event (%v, %v): the hidden or foreign-tenant transition leaked", msg, err)
	}
	if msg, err, ok := unscoped.next(t, 300*time.Millisecond); ok {
		t.Fatalf("unscoped caller received %v (%v)", msg, err)
	}

	denied := &roleAccessSpy{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{
		{RoleID: "comp_admin", PageID: "home", View: true},
		{RoleID: "intent_author", PageID: "home", View: true},
	}}}
	gated := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Invalidations: hub, RoleAccess: denied}))
	s, err := gated.WatchPromotionInvalidations(authorizedContext(t), &journeyv1.WatchPromotionInvalidationsRequest{Region: "SHELL_COUNT"})
	if err == nil {
		_, err = s.Recv()
	}
	assertOwnedCode(t, err, envelope.CodePermissionDenied)
}
