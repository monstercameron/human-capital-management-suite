package journeyclient

import (
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UIPOLISH_009_SameRouteOutOfOrderDetail(t *testing.T) {
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), newFakeService(), store, nil)
	app.route = Route{Kind: RouteDetail, IntentID: testIntentID}
	app.generation = 4

	newer := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	newer.Journey.InstanceVersion = 9
	newer.Instance.InstanceVersion = 9
	newer.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:02Z")
	newer.DetailDigest = "sha256:newer"
	app.applyDetail(4, newer, &journey.Notice{Title: "Approval recorded", Tone: toneSuccess})

	olderVersion := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	olderVersion.Journey.InstanceVersion = 8
	olderVersion.Instance.InstanceVersion = 8
	olderVersion.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:01Z")
	app.applyDetail(4, olderVersion, &journey.Notice{Title: "Old watch event", Tone: toneWarning})
	if app.detail != newer || store.Page().Notice.Title != "Approval recorded" {
		t.Fatal("an older watch event replaced the newer approval detail or notice")
	}

	olderTimestamp := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	olderTimestamp.Journey.InstanceVersion = 9
	olderTimestamp.Instance.InstanceVersion = 9
	olderTimestamp.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:01Z")
	app.applyDetail(4, olderTimestamp, nil)
	if app.detail != newer || store.Page().Notice.Title != "Approval recorded" {
		t.Fatal("same-version stale data replaced the newer detail or confirmation")
	}

	latest := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE)
	latest.Journey.InstanceVersion = 10
	latest.Instance.InstanceVersion = 10
	latest.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:03Z")
	app.applyDetail(4, latest, nil)
	if app.detail != latest {
		t.Fatal("a genuinely newer watch event was not applied")
	}
}

func TestTodo_UIPOLISH_009_PreExecutionOutOfOrderDetail(t *testing.T) {
	newer := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	older := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	newer.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:02Z")
	older.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:01Z")
	if !olderDetail(newer, older) {
		t.Fatal("pre-execution proposal detail accepted an older timestamp")
	}
	if olderDetail(older, newer) {
		t.Fatal("pre-execution proposal detail rejected a newer timestamp")
	}
}

func TestTodo_UIPOLISH_009_VersionWinsOverTimestamp(t *testing.T) {
	previous := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	next := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	previous.Journey.InstanceId = testInstanceID
	next.Journey.InstanceId = testInstanceID
	previous.Journey.InstanceVersion, previous.Instance.InstanceVersion = 8, 8
	next.Journey.InstanceVersion, next.Instance.InstanceVersion = 9, 9
	previous.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:02Z")
	next.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:01Z")
	if olderDetail(previous, next) {
		t.Fatal("a higher durable instance version was rejected due to clock skew")
	}
	next.Journey.InstanceId = "replacement-instance"
	next.Instance.InstanceId = "replacement-instance"
	next.Journey.InstanceVersion, next.Instance.InstanceVersion = 1, 1
	if !olderDetail(previous, next) || !olderDetail(next, previous) {
		t.Fatal("different instance IDs were treated as orderable within one intent")
	}
}

func TestTodo_UIPOLISH_009_FirstExecutionWinsOverProposalClock(t *testing.T) {
	proposal := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	executed := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	proposal.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:02Z")
	executed.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:01Z")
	executed.Journey.InstanceId = testInstanceID
	executed.Journey.InstanceVersion = 1
	executed.Instance.InstanceVersion = 1
	if olderDetail(proposal, executed) {
		t.Fatal("first durable execution was rejected due to presentation clock skew")
	}
	if !olderDetail(executed, proposal) {
		t.Fatal("proposal-only snapshot was accepted after durable execution")
	}
}

func TestTodo_UIPOLISH_009_DetailAndNoticePublishTogether(t *testing.T) {
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), newFakeService(), store, nil)
	app.route = Route{Kind: RouteDetail, IntentID: testIntentID}
	app.generation = 4
	first := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	second := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)
	first.Journey.InstanceVersion, first.Instance.InstanceVersion = 8, 8
	second.Journey.InstanceVersion, second.Instance.InstanceVersion = 9, 9
	first.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:01Z")
	second.Journey.UpdatedAt = stamp(t, "2026-09-13T14:00:02Z")
	entered, release := make(chan struct{}), make(chan struct{})
	store.Subscribe(func() {
		if store.Page().Notice != nil && store.Page().Notice.Title == "First" {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
		}
	})
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	go func() {
		app.applyDetail(4, first, &journey.Notice{Title: "First", Tone: toneSuccess})
		close(firstDone)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first detail did not reach the publication seam")
	}
	go func() {
		app.applyDetail(4, second, &journey.Notice{Title: "Second", Tone: toneSuccess})
		close(secondDone)
	}()
	select {
	case <-secondDone:
		t.Fatal("newer detail published before the first detail completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first detail remained blocked after release")
	}
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second detail did not publish")
	}
	if app.detail != second || store.Page().Notice.Title != "Second" {
		t.Fatal("newer detail and its notice were not published together")
	}
}
