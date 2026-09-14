package journeyclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UIPOLISH_009_StaleCallbackCannotPublishNotice proves the
// generation fence used by late RPC refusals. A callback for the same kind
// of route is stale once navigation advances the generation, and must not
// replace the current page's loading state with its old answer.
func TestTodo_UIPOLISH_009_StaleCallbackCannotPublishNotice(t *testing.T) {
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), newFakeService(), store, nil)
	app.route = Route{Kind: RouteDetail, IntentID: testIntentID}
	app.generation = 2
	app.show(busy("journey.busy_detail", "Loading promotion details."))

	if app.showCurrent(1, &journey.Notice{Tone: toneDanger, Title: "Old refusal"}) {
		t.Fatal("stale callback was reported as published")
	}
	if got := store.Page().Notice; got == nil || got.TitleKey != "journey.busy_title" {
		t.Fatalf("stale callback replaced current notice: %+v", got)
	}
}
