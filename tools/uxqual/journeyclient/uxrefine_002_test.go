package journeyclient

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTodo_UXBLIND_002(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	h.svc.executeErr = status.Error(codes.Unavailable, "approval route unavailable")
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)
	h.app.Submit(ActionExecute, nil)

	p := h.awaitPage(t, "the routed failure", func(p journey.Page) bool {
		return p.Notice != nil && p.Notice.TitleKey == "journey.error_domain_unavailable_title"
	})
	if got := p.Notice.Detail; !strings.Contains(got, "approval service") || !strings.Contains(got, "try again later") {
		t.Fatalf("failure notice = %q, want cause and recovery", got)
	}
	if len(p.Detail.Timeline) < 1 || p.Detail.Timeline[0].Title != "Approval start failed" {
		t.Fatalf("timeline = %+v, want failed approval start first", p.Detail.Timeline)
	}
	if p.Detail.Timeline[0].Actor == "" || p.Detail.Timeline[0].At == "" {
		t.Fatalf("failed timeline event lacks actor/time: %+v", p.Detail.Timeline[0])
	}
}

func TestTodo_UXBLIND_002_Regression_NoteClearsFailure(t *testing.T) {
	app, store, svc := notesHarness(t)
	svc.fakeService.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	app.recordFailedApprovalStart(testIntentID, status.Error(codes.Unavailable, "approval route unavailable"))
	app.show(approvalStartNotice(status.Error(codes.Unavailable, "approval route unavailable"), app.localeCopy()))
	if store.Page().Notice == nil {
		t.Fatal("failed approval did not publish a notice")
	}
	app.show(nil)
	p := store.Page()
	if p.Notice != nil {
		t.Fatalf("notice after successful note = %+v, want cleared", p.Notice)
	}
}
