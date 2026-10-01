package journeyclient

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	journey "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UXBLIND_090_JourneyColdLoad(t *testing.T) {
	h := newHarness(t)
	h.svc.workers = []*journeyv1.Worker{{WorkerRef: "ana-flores", PreferredName: "Ana", LegalName: "Ana Flores"}}
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	h.svc.detail.Journey.WorkerRef = "ana-flores"
	h.svc.detail.Journey.WorkerName = "Ana"
	h.app.Start(context.Background(), DetailHref(testIntentID))
	p := h.awaitPage(t, "the cold-loaded named journey", detailShown)
	if got := p.Detail.Journey.WorkerName; got != "Ana Flores" {
		t.Fatalf("cold-loaded journey name = %q, want Ana Flores", got)
	}
	markup, err := journey.RenderToString(p)
	if err != nil {
		t.Fatalf("render cold-loaded journey: %v", err)
	}
	if !strings.Contains(markup, "Ana Flores") || strings.Contains(markup, ">Ana<") {
		t.Fatalf("rendered cold-loaded journey does not contain the full worker name: %s", markup)
	}
}

func TestTodo_UXBLIND_090_Browser(t *testing.T) {
	h := newHarness(t)
	h.svc.workers = []*journeyv1.Worker{{WorkerRef: "ana-flores", PreferredName: "Ana", LegalName: "Ana Flores"}}
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	h.svc.detail.Journey.WorkerRef = "ana-flores"
	h.svc.detail.Journey.WorkerName = "Ana"
	h.app.Start(context.Background(), DetailHref(testIntentID))
	p := h.awaitPage(t, "the browser detail projection", detailShown)
	if p.Detail.Journey.WorkerName != "Ana Flores" || p.Title == "" {
		t.Fatalf("browser detail projection = name %q title %q", p.Detail.Journey.WorkerName, p.Title)
	}
}

func TestTodo_UXBLIND_090_Regression(t *testing.T) {
	journey := &journeyv1.Journey{WorkerRef: "ana-flores", WorkerName: "Ana"}
	worker := &journeyv1.Worker{WorkerRef: "ana-flores", PreferredName: "Ana", LegalName: "Ana Flores"}
	projectJourneyWorkerNames([]*journeyv1.Journey{journey}, []*journeyv1.Worker{worker})
	if journey.WorkerName != "Ana Flores" {
		t.Fatalf("projected history name = %q, want Ana Flores", journey.WorkerName)
	}
}
