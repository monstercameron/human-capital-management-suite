//go:build !(js && wasm)

package main

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATUX_046 is the primary leg of the client-bundle todo: the largest
// view records the browser module links no longer carry equality functions.
// (The size ceiling and its headroom floors were removed on 2026-11-20.) The full marker
// list is pinned in productui (TestTodo_CHATUX_046_MarkersPinned, which also
// sees unexported types); this test pins the mechanism from the outside.
// The built bundle itself is measured by TestTodo_CHATUX_046_Integration,
// and the history arrival by TestTodo_CHATUX_046_Browser.
func TestTodo_CHATUX_046(t *testing.T) {
	t.Run("LargestViewRecordsAreNotComparable", func(t *testing.T) {
		marked := map[string]any{
			"Person":                   productui.Person{},
			"DocumentSummary":          productui.DocumentSummary{},
			"MissingPunchReviewView":   productui.MissingPunchReviewView{},
			"WorkflowHistoryRun":       productui.WorkflowHistoryRun{},
			"DocsProjectTaskPreview":   productui.DocsProjectTaskPreview{},
			"AgentScheduleDraft":       productui.AgentScheduleDraft{},
			"DocumentComment":          productui.DocumentComment{},
			"DocumentReviewProjection": productui.DocumentReviewProjection{},
			"ClockProjection":          productui.ClockProjection{},
			"DocsJourneyPreview":       productui.DocsJourneyPreview{},
			"WorkflowPaletteItem":      productui.WorkflowPaletteItem{},
			"MissingPunchSessionView":  productui.MissingPunchSessionView{},
			"PolicyRuleProjection":     productui.PolicyRuleProjection{},
			"MissingPunchReceipt":      productui.MissingPunchReceipt{},
		}
		for name, value := range marked {
			if reflect.TypeOf(value).Comparable() {
				t.Errorf("productui.%s is comparable; its equality function is back in the browser module", name)
			}
		}
	})
}
