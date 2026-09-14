package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UIPOLISH_009_SameVersionStageRewindWithoutTimestampIsRejected(t *testing.T) {
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), newFakeService(), store, nil)
	app.route = Route{Kind: RouteDetail, IntentID: testIntentID}
	app.generation = 1

	current := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	incoming := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	current.Journey.InstanceVersion, current.Instance.InstanceVersion = 4, 4
	incoming.Journey.InstanceVersion, incoming.Instance.InstanceVersion = 4, 4
	current.Journey.UpdatedAt = nil
	incoming.Journey.UpdatedAt = nil
	app.detail = current
	app.show(&journey.Notice{Title: "Approval recorded", Tone: toneSuccess})

	app.applyDetail(1, incoming, &journey.Notice{Title: "Stale watch event", Tone: toneWarning})
	page := store.Page()
	if app.detail != current {
		t.Fatal("same-version stage rewind without timestamps replaced current detail")
	}
	if page.Notice == nil || page.Notice.Title != "Approval recorded" {
		t.Fatalf("notice after stale stage rewind = %+v, want existing success notice", page.Notice)
	}
}

func TestTodo_UIPOLISH_009_SameVersionStageRewindWithEqualTimestampIsRejected(t *testing.T) {
	current := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	incoming := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	current.Journey.InstanceVersion, current.Instance.InstanceVersion = 4, 4
	incoming.Journey.InstanceVersion, incoming.Instance.InstanceVersion = 4, 4
	equal := stamp(t, "2026-09-13T14:00:00Z")
	current.Journey.UpdatedAt = equal
	incoming.Journey.UpdatedAt = equal
	if !olderDetail(current, incoming) {
		t.Fatal("same-version stage rewind with equal timestamps was accepted")
	}
}

func TestTodo_UIPOLISH_009_SameVersionSameStageRefreshRemainsAccepted(t *testing.T) {
	current := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	refresh := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	current.Journey.InstanceVersion, current.Instance.InstanceVersion = 4, 4
	refresh.Journey.InstanceVersion, refresh.Instance.InstanceVersion = 4, 4
	current.Journey.UpdatedAt = nil
	refresh.Journey.UpdatedAt = nil
	refresh.DetailDigest = "sha256:authorized-capability-refresh"
	if olderDetail(current, refresh) {
		t.Fatal("same-version same-stage authorized detail refresh was rejected")
	}
}
