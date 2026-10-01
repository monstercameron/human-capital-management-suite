package journey_test

import (
	"context"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

func TestTodo_WFPAGE_007_Cursor(t *testing.T) {
	engine := &pagingFakeEngine{fakeEngine: newFakeEngine(), page: workspace.JourneyListPage{
		Journeys: []workspace.JourneySummary{pagedSummary("history-run-1", true)}, NextCursor: "engine-position", TotalCount: 2,
	}}
	key := []byte("history-page-cursor-key-32-bytes-long")
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, CursorKey: key}))
	ctx := testContext(t)

	first, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{Page: &commonv1.PageRequest{PageSize: 1}, Query: "promotion"})
	if err != nil {
		t.Fatalf("first history page: %v", err)
	}
	cursor := first.GetPage().GetNextCursor()
	if cursor == "" || cursor == "engine-position" {
		t.Fatalf("next cursor = %q, want signed transport cursor", cursor)
	}

	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{Page: &commonv1.PageRequest{PageSize: 1, Cursor: cursor}, Query: "promotion"}); err != nil {
		t.Fatalf("resume history page: %v", err)
	}
	if got := engine.recordedReq().Cursor; got != "engine-position" {
		t.Fatalf("engine cursor = %q, want the signed cursor's inner position", got)
	}

	tampered := cursor[:len(cursor)-1] + "0"
	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{Page: &commonv1.PageRequest{Cursor: tampered}, Query: "promotion"}); err == nil {
		t.Fatal("tampered history cursor was accepted")
	} else {
		assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	}
	foreign := withToken(context.Background(), fixtureOtherTenantToken)
	if _, err := client.ListJourneys(foreign, &journeyv1.ListJourneysRequest{Page: &commonv1.PageRequest{Cursor: cursor}, Query: "promotion"}); err == nil {
		t.Fatal("history cursor crossed tenant boundary")
	} else {
		assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	}
}
