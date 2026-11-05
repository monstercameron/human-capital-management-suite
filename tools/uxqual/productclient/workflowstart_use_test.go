package productclient

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_WFPAGE_004_RecordUseOnStartOpen proves opening a workflow start
// deep link records that workflow as used, while any other route records
// nothing.
func TestTodo_WFPAGE_004_RecordUseOnStartOpen(t *testing.T) {
	id, ok := WorkflowStartOpenedUse(productui.PageWorkflowStart, "hire")
	if !ok || id != "hire" {
		t.Fatalf("start deep link records %q,%v, want hire,true", id, ok)
	}
	for _, tc := range []struct {
		name string
		page productui.PageID
		id   string
	}{
		{"other page", productui.PageJourneys, "hire"},
		{"blank id", productui.PageWorkflowStart, "  "},
		{"empty id", productui.PageWorkflowStart, ""},
	} {
		if id, ok := WorkflowStartOpenedUse(tc.page, tc.id); ok || id != "" {
			t.Fatalf("%s records %q,%v, want nothing", tc.name, id, ok)
		}
	}
}
