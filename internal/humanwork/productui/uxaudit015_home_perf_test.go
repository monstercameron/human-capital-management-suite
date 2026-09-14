//go:build !race && !covergate

package productui

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

// TestTodo_UXAUDIT_015_Performance bounds Home's client-side projection and
// render cost with a mixed, authorized work stream. Network and paint budgets
// are measured separately; this test cannot claim to measure either one.
func TestTodo_UXAUDIT_015_Performance(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view.Viewer = ViewerProfile{PersonID: "viewer"}
	for index := 0; index < 100; index++ {
		id := fmt.Sprintf("worker-%03d", index)
		status := []string{"Awaiting approval", "Draft", "Waiting on employee", "Completed"}[index%4]
		view.People = append(view.People, Person{ID: id, Name: fmt.Sprintf("Worker %03d", index)})
		view.Work = append(view.Work, WorkItem{
			ID: fmt.Sprintf("work-%03d", index), PersonRef: id, AssigneeRef: "viewer",
			Person: fmt.Sprintf("Worker %03d", index), Title: "Promotion", Status: status,
			Due: "2026-09-14", Terminal: status == "Completed",
		})
	}
	assertInteractionLatency(t, latencygate.Budget{
		Name: "Home mixed work render (100 records)", P95: 50 * time.Millisecond,
		Warmups: 3, Samples: interactionLatencySamples,
	}, func() error {
		_, err := ui.RenderToString(homePage(view))
		return err
	})
}
