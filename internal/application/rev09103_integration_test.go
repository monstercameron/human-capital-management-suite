package application

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
)

type rev09103Event struct {
	message productquery.InvalidationMessage
	err     error
}

func rev09103Open(t *testing.T, h *rbacHarness, user string, region promotion.Region) <-chan rev09103Event {
	t.Helper()
	stream, err := h.journey.WatchPromotionInvalidations(h.rpc(user), &journeyv1.WatchPromotionInvalidationsRequest{Region: string(region)})
	if err != nil {
		t.Fatalf("open %s stream for %s: %v", region, user, err)
	}
	events := make(chan rev09103Event, 8)
	go func() {
		defer close(events)
		for {
			msg, err := stream.Recv()
			if err != nil {
				events <- rev09103Event{err: err}
				return
			}
			var message productquery.InvalidationMessage
			if decodeErr := json.Unmarshal(msg.GetInvalidation(), &message); decodeErr != nil {
				events <- rev09103Event{err: decodeErr}
				return
			}
			if validateErr := message.Validate(); validateErr != nil {
				events <- rev09103Event{err: validateErr}
				return
			}
			events <- rev09103Event{message: message}
		}
	}()
	return events
}

func rev09103Receive(t *testing.T, who string, events <-chan rev09103Event) productquery.InvalidationMessage {
	t.Helper()
	select {
	case event := <-events:
		if event.err != nil {
			t.Fatalf("%s stream: %v", who, event.err)
		}
		return event.message
	case <-time.After(30 * time.Second):
		t.Fatalf("%s received no hint for a committed transition it may see", who)
	}
	return productquery.InvalidationMessage{}
}

func rev09103Journey(t *testing.T, h *rbacHarness, user string) *journeyv1.Journey {
	t.Helper()
	listed, err := h.journey.ListJourneys(h.rpc(user), &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys as %s: %v", user, err)
	}
	for _, j := range listed.GetJourneys() {
		if j.GetIntentId() == h.intentID {
			return j
		}
	}
	t.Fatalf("ListJourneys as %s does not show the fixture journey", user)
	return nil
}

// TestTodo_REV_091_03_Integration drives the served path end to end: the
// composed serve cell (ComposeServe, the real gRPC server and interceptor
// chain, the production journey engine over embedded PostgreSQL). A real
// committed transition -- the routed finance partner approving the fixture
// promotion through DecideJourney -- reaches every viewer who may see it as a
// hint for their region, and each viewer's ordinary re-read then shows the
// transition; a manager outside the promotion's reporting line receives
// nothing at all.
func TestTodo_REV_091_03_Integration(t *testing.T) {
	h := rbacCompose(t)
	hub := h.composed.Cell().JourneyInvalidations
	if hub == nil {
		t.Fatal("the served cell composed no invalidation hub")
	}
	tenant := kernelvalues.TenantId(h.cfg.Tenant)
	before := rev09103Journey(t, h, "compAdmin")

	shell := rev09103Open(t, h, "compAdmin", promotion.RegionShellCount)
	person := rev09103Open(t, h, "compAdmin", promotion.RegionPerson)
	detail := rev09103Open(t, h, "compAdmin", promotion.RegionDetail)
	work := rev09103Open(t, h, "financePartner", promotion.RegionMyWork)
	outsider := rev09103Open(t, h, "hana", promotion.RegionShellCount)
	deadline := time.Now().Add(30 * time.Second)
	for hub.Subscribers(tenant) < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("subscriptions open = %d", hub.Subscribers(tenant))
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := h.journey.DecideJourney(h.rpc("financePartner"), &journeyv1.DecideJourneyRequest{
		IntentId: h.intentID, Approve: true, Reason: "rev09103: finance approves",
	}); err != nil {
		t.Fatalf("DecideJourney as the finance partner: %v", err)
	}

	journeyRef := journeyinvalidation.JourneyRef(tenant, h.intentID)
	got := rev09103Receive(t, "comp admin shell", shell)
	if got.SourceSequence != 1 || got.Items[0].Subject != promotion.ShellCountSubject(tenant) {
		t.Fatalf("shell hint = %+v", got)
	}
	if got := rev09103Receive(t, "comp admin detail", detail); got.Items[0].Subject != journeyRef {
		t.Fatalf("detail hint subject = %v, want %v", got.Items[0].Subject, journeyRef)
	}
	if got := rev09103Receive(t, "comp admin person", person); got.Items[0].Subject.Kind != "worker" {
		t.Fatalf("person hint subject = %v, want the promoted worker", got.Items[0].Subject)
	}
	if got := rev09103Receive(t, "finance partner my work", work); got.Items[0].Subject != journeyRef || got.SourceSequence != 1 {
		t.Fatalf("my work hint = %+v", got)
	}

	// The hint carried no data; the re-read it triggers is what converges.
	after := rev09103Journey(t, h, "compAdmin")
	if after.GetStage() == before.GetStage() && after.GetUpdatedAt().AsTime().Equal(before.GetUpdatedAt().AsTime()) {
		t.Fatalf("the re-read after the hint shows no transition (stage %v, updated %v)", after.GetStage(), after.GetUpdatedAt().AsTime())
	}

	select {
	case event, ok := <-outsider:
		if ok && (event.err == nil || status.Code(event.err) != codes.PermissionDenied) {
			t.Fatalf("a manager outside the reporting line received %+v", event)
		}
	case <-time.After(2 * time.Second):
	}
}
