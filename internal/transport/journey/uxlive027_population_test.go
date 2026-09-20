package journey_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/prototext"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// uxlive027Summaries is a nine-journey authorized list: the RED session's
// population size, across open, closed and exception stages.
func uxlive027Summaries(base time.Time) []workspace.JourneySummary {
	summary := func(id string, stage workspace.JourneyStage, responsibility workspace.JourneyViewerResponsibility, closed bool, at time.Duration) workspace.JourneySummary {
		s := fixtureSummary()
		s.IntentID, s.Stage, s.UpdatedAt = id, stage, base.Add(at)
		s.WorkerName = "Worker " + id
		s.Viewer = workspace.JourneyViewerProjection{Responsibility: responsibility, Closed: closed}
		return s
	}
	initiated := func(s workspace.JourneySummary) workspace.JourneySummary {
		s.Viewer.Relationships = []workspace.JourneyViewerRelationship{workspace.JourneyViewerInitiator}
		return s
	}
	return []workspace.JourneySummary{
		summary("j1", workspace.JourneyStageProposed, workspace.JourneyResponsibilityActionRequired, false, 1*time.Hour),
		initiated(summary("j2", workspace.JourneyStageProposed, workspace.JourneyResponsibilityTracking, false, 2*time.Hour)),
		summary("j3", workspace.JourneyStageManagerApproval, workspace.JourneyResponsibilityActionRequired, false, 3*time.Hour),
		summary("j4", workspace.JourneyStageFinanceApproval, workspace.JourneyResponsibilityObserving, false, 4*time.Hour),
		summary("j5", workspace.JourneyStageBlocked, workspace.JourneyResponsibilityActionRequired, false, 5*time.Hour),
		initiated(summary("j6", workspace.JourneyStageWaitingEffectiveDate, workspace.JourneyResponsibilityTracking, false, 6*time.Hour)),
		initiated(summary("j7", workspace.JourneyStageRecorded, workspace.JourneyResponsibilityClosed, true, 7*time.Hour)),
		summary("j8", workspace.JourneyStageRejected, workspace.JourneyResponsibilityClosed, true, 8*time.Hour),
		summary("j9", workspace.JourneyStageFailed, workspace.JourneyResponsibilityClosed, true, 9*time.Hour),
	}
}

// TestTodo_UXLIVE_027_Integration drives ListJourneys through a real gRPC
// server and client: the population summary on the wire is a count over
// exactly the journeys the same response lists, with its freshness set.
func TestTodo_UXLIVE_027_Integration(t *testing.T) {
	base := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	engine := newFakeEngine()
	engine.summaries = uxlive027Summaries(base)
	computed := base.Add(24 * time.Hour)
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Now: func() time.Time { return computed }}))

	resp, err := client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	population := resp.GetPopulation()
	if population == nil {
		t.Fatal("ListJourneys returned no population summary")
	}
	listed := resp.GetJourneys()
	if int(population.GetTotal()) != len(listed) || len(listed) != 9 {
		t.Fatalf("population total %d, listed %d, want 9 and 9", population.GetTotal(), len(listed))
	}
	closed, needs, exceptions := 0, 0, 0
	byStage := map[journeyv1.JourneyStage]int32{}
	for _, j := range listed {
		byStage[j.GetStage()]++
		if j.GetViewer().GetClosed() {
			closed++
		} else if j.GetViewer().GetResponsibility() == journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED {
			needs++
		}
		switch j.GetStage() {
		case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED, journeyv1.JourneyStage_JOURNEY_STAGE_FAILED, journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED:
			exceptions++
		}
	}
	if int(population.GetClosed()) != closed || int(population.GetActive()) != len(listed)-closed ||
		int(population.GetNeedsAction()) != needs || int(population.GetExceptions()) != exceptions ||
		population.GetTracking() != 2 {
		t.Fatalf("population %+v does not reconcile with the listed journeys (closed %d, needs %d, exceptions %d)",
			population, closed, needs, exceptions)
	}
	var bucketTotal int32
	last := journeyv1.JourneyStage(-1)
	for _, bucket := range population.GetStages() {
		if bucket.GetStage() <= last {
			t.Fatalf("stage buckets are not in wire stage order: %v", population.GetStages())
		}
		last = bucket.GetStage()
		if byStage[bucket.GetStage()] != bucket.GetCount() {
			t.Fatalf("bucket %v = %d, listed %d", bucket.GetStage(), bucket.GetCount(), byStage[bucket.GetStage()])
		}
		bucketTotal += bucket.GetCount()
	}
	if bucketTotal != population.GetTotal() {
		t.Fatalf("stage buckets sum to %d, total is %d", bucketTotal, population.GetTotal())
	}
	if !population.GetLatestUpdate().AsTime().Equal(base.Add(9*time.Hour)) || !population.GetComputedAt().AsTime().Equal(computed) {
		t.Fatalf("freshness = latest %v computed %v", population.GetLatestUpdate().AsTime(), population.GetComputedAt().AsTime())
	}

	// An empty authorized set is a real zero, not a missing summary.
	engine.mu.Lock()
	engine.summaries = nil
	engine.mu.Unlock()
	resp, err = client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys(empty): %v", err)
	}
	if resp.GetPopulation() == nil || resp.GetPopulation().GetTotal() != 0 || resp.GetPopulation().GetLatestUpdate() != nil {
		t.Fatalf("an empty set answered population %+v", resp.GetPopulation())
	}
}

// TestTodo_UXLIVE_027_Security proves the summary adds no disclosure: it is
// only ever built after admission over the caller's own list, and it carries
// counts and times, never an identity from the journeys it counts.
func TestTodo_UXLIVE_027_Security(t *testing.T) {
	base := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	engine := newFakeEngine()
	engine.summaries = uxlive027Summaries(base)
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
	assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	if resp.GetPopulation() != nil {
		t.Fatal("an unauthenticated caller received a population summary")
	}

	resp, err = client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	wire := prototext.Format(resp.GetPopulation())
	for _, summary := range engine.summaries {
		for _, identity := range []string{summary.IntentID + "\"", summary.WorkerName, summary.Worker.String(), summary.CorrelationID} {
			if identity != "" && identity != "\"" && strings.Contains(wire, identity) {
				t.Fatalf("the population summary carries identity %q:\n%s", identity, wire)
			}
		}
	}
}
