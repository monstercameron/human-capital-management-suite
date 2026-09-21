package journeyclient

import (
	"context"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/metadata"
)

// TestTodo_REV_091_03 proves the Journeys list converges on a live hint
// without flicker: the quiet refresh re-reads only ListJourneys (not the
// workforce), never publishes a busy notice, and replaces the rows in place.
func TestTodo_REV_091_03(t *testing.T) {
	h := newHarness(t)
	first := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.svc.list = []*journeyv1.Journey{first}
	h.app.Start(context.Background(), "")
	h.awaitPage(t, "the list", listLoaded)
	listReads, workerReads := h.svc.called("ListJourneys"), h.svc.called("ListWorkers")

	// Record every page the store publishes from here on: a busy notice at
	// any point would be the flicker this refresh exists to avoid.
	var sawBusy bool
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if p := h.store.Page(); p.Notice != nil || p.List == nil {
				sawBusy = true
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()

	second := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	second.IntentId = "intent-second"
	h.svc.mu.Lock()
	h.svc.list = []*journeyv1.Journey{first, second}
	h.svc.mu.Unlock()
	if !h.app.RefreshListQuietly() {
		t.Fatal("a loaded list refused the quiet refresh")
	}
	p := h.awaitPage(t, "the refreshed list", func(p journey.Page) bool { return p.List != nil && len(p.List.Journeys) == 2 })
	close(stop)
	<-done
	if sawBusy {
		t.Fatal("the quiet refresh published a busy or empty page")
	}
	if p.Notice != nil {
		t.Fatalf("notice after refresh = %+v", p.Notice)
	}
	if got := h.svc.called("ListJourneys") - listReads; got != 1 {
		t.Fatalf("ListJourneys reads = %d, want exactly 1", got)
	}
	if got := h.svc.called("ListWorkers") - workerReads; got != 0 {
		t.Fatalf("the workforce was re-read %d times for a promotion hint", got)
	}
}

func TestRefreshListQuietlyOnlyOnALoadedList(t *testing.T) {
	var nilApp *App
	if nilApp.RefreshListQuietly() {
		t.Fatal("nil app scheduled a refresh")
	}
	h := newHarness(t)
	if h.app.RefreshListQuietly() {
		t.Fatal("an app that never loaded a list scheduled a refresh")
	}
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.app.Start(context.Background(), DetailHref(h.svc.detail.GetJourney().GetIntentId()))
	h.awaitPage(t, "the detail", detailShown)
	if h.app.RefreshListQuietly() {
		t.Fatal("a detail route scheduled a list refresh; WatchJourney owns the detail")
	}
}

// The live-update stream goes through the same bounded adapter as every
// other call: it opens the canonical method and carries the credential.
func TestWatchPromotionInvalidationsUsesTheCanonicalStream(t *testing.T) {
	conn := &recordingConn{}
	svc, ok := NewGRPCService(conn, "tok_rev09103").(InvalidationService)
	if !ok {
		t.Fatal("the production service does not offer live updates")
	}
	if _, err := svc.WatchPromotionInvalidations(context.Background(), &journeyv1.WatchPromotionInvalidationsRequest{Region: "SHELL_COUNT"}); err != nil {
		t.Fatalf("WatchPromotionInvalidations: %v", err)
	}
	if len(conn.streams) != 1 || conn.streams[0] != journeyv1.JourneyService_WatchPromotionInvalidations_FullMethodName {
		t.Fatalf("streams = %v", conn.streams)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get(AuthorizationHeader); len(got) != 1 || got[0] != BearerScheme+"tok_rev09103" {
		t.Fatalf("authorization = %v", got)
	}
}
