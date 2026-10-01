package journey

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// uxblindSNodeStatusLabel keeps engine enum values out of the support table.
// The renderer receives the already-authorized diagnostics rows; it only
// supplies the product vocabulary for their status column.
func uxblindSNodeStatusLabel(locale, status string) string {
	copy := productui.ResolveProductLocale(locale)
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "RUNNING":
		return copy.Text("workflow_viewer.status.running")
	case "WAITING", "PENDING":
		return copy.Text("workflow_viewer.status.waiting")
	case "SUCCEEDED", "COMPLETED", "DONE":
		return copy.Text("workflow_viewer.status.completed")
	case "FAILED", "ERROR":
		return copy.Text("workflow_viewer.status.failed")
	case "CANCELLED", "CANCELED":
		return copy.Text("workflow_viewer.status.cancelled")
	default:
		return copy.Text("workflow_viewer.status.unknown")
	}
}
