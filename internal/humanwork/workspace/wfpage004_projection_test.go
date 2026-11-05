package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_WFPAGE_004_ServedProjectionPersistsPerUserState proves a star
// survives the reload round trip on the served path: the saved ids ride the
// island newest-first for recents, and the catalog entries carry the
// per-user marks. Section rendering from that projection, including the
// fail-closed drop of unavailable ids, is proven in productui.
func TestTodo_WFPAGE_004_ServedProjectionPersistsPerUserState(t *testing.T) {
	h, token := newShellHandler(t, false)
	h.workflowStarts = func(context.Context, values.TenantId, bool) ([]WorkflowStartConfig, error) {
		return []WorkflowStartConfig{
			{WorkflowID: "hire", Name: "New hire", Category: "People", Availability: "available"},
			{WorkflowID: "promotion", Name: "Promotion review", Category: "People", Availability: "available"},
			{WorkflowID: "quarantined", Name: "Quarantined flow", Category: "People", Availability: "quarantined"},
		}, nil
	}
	snapshot := preferences.DefaultSnapshot()
	snapshot.User.FavoritePages = []string{"workflow-start:hire", "workflow-start:quarantined", "people"}
	snapshot.User.WorkflowUses = map[string]int64{"hire": 2, "promotion": 5}
	h.preferences = uxblindTPreferenceStore{snapshot: snapshot}

	recorder := uxblindTRequest(t, h, token, productui.Path(productui.PageWorkflowStart))
	if recorder.Code != 200 {
		t.Fatalf("GET workflows page = %d: %s", recorder.Code, recorder.Body.String())
	}
	doc := recorder.Body.String()
	// The SSR document is a loading shell: the live client renders the
	// Favourites and Recent sections from this island, so the island is
	// the assertion surface for the served projection.
	for _, want := range []string{`"workflow_start_favorites":["hire","quarantined"]`, `"workflow_start_recent":["promotion","hire"]`} {
		if !strings.Contains(doc, want) {
			t.Errorf("served island misses %q", want)
		}
	}
	// The catalog entries themselves carry the per-user marks the sections
	// fall back to.
	for _, want := range []string{`"workflow_id":"hire"`, `"favorite":true`, `"recent_rank":5`} {
		if !strings.Contains(doc, want) {
			t.Errorf("served catalog misses %q", want)
		}
	}
}
