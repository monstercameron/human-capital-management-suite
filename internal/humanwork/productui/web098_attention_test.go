package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_WEB_098 proves Home and My Work consume the same authorized,
// viewer-relevant open work projection. Passive tracking, terminal work and
// records denied by the adapter do not become attention rows.
func TestTodo_WEB_098(t *testing.T) {
	view := testView(PageHome)
	view.Work = []WorkItem{
		{ID: "assigned", Title: "Assigned approval", PersonRef: "someone-else", AssigneeRef: "worker-jordan", ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"ASSIGNEE"}, NextStep: "approval_decision", Status: "Awaiting approval"},
		{ID: "passive", Title: "Passive status", PersonRef: "worker-jordan", ViewerResponsibility: "TRACKING", ViewerRelationships: []string{"INITIATOR"}, Status: "In progress"},
		{ID: "closed", Title: "Closed record", PersonRef: "worker-jordan", Terminal: true, ViewerResponsibility: "CLOSED", Status: "Completed"},
		{ID: "denied", Title: "Denied approval", PersonRef: "worker-jordan", ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"ASSIGNEE"}, Status: "Awaiting approval"},
	}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"assigned": {ID: "assigned", Disclosable: true},
		"passive":  {ID: "passive", Disclosable: true},
		"closed":   {ID: "closed", Disclosable: true},
		"denied":   {ID: "denied", Disclosable: false},
	}

	for _, page := range []PageID{PageHome, PageWork} {
		view.Page = page
		var node ui.Node
		if page == PageHome {
			node = homePage(view)
		} else {
			node = workPage(view)
		}
		markup, err := ui.RenderToString(node)
		if err != nil {
			t.Fatalf("render %s: %v", page, err)
		}
		doc, err := xhtml.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatalf("parse %s: %v", page, err)
		}
		queue := findHTMLNode(doc, func(n *xhtml.Node) bool {
			if n.Type != xhtml.ElementNode || n.Data != "section" {
				return false
			}
			for _, attr := range n.Attr {
				if attr.Key == "data-work-kind" && attr.Val == "action-queue" {
					return true
				}
			}
			return false
		})
		if queue == nil {
			t.Errorf("%s omitted the named attention queue", page)
			continue
		}
		queueText := htmlText(queue)
		if !strings.Contains(queueText, "Assigned approval") {
			t.Errorf("%s omitted the viewer's assigned approval: %s", page, queueText)
		}
		for _, forbidden := range []string{"Passive status", "Closed record", "Denied approval"} {
			if strings.Contains(queueText, forbidden) {
				t.Errorf("%s exposed non-actionable or denied attention item %q", page, forbidden)
			}
		}
	}
}

// Golden pins the attention population and urgency order produced from
// versioned work projections, including the empty and terminal-only cases.
func TestTodo_WEB_098_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{{ID: "done", Terminal: true}},
		{
			{ID: "future", ViewerResponsibility: "ACTION_REQUIRED", Tone: "neutral"},
			{ID: "waiting", ViewerResponsibility: "ACTION_REQUIRED", Tone: "warning", AwaitsPerson: true},
			{ID: "repair", ViewerResponsibility: "ACTION_REQUIRED", Tone: "danger"},
			{ID: "passive", ViewerResponsibility: "TRACKING", Tone: "danger"},
		},
	}
	var result strings.Builder
	for _, stream := range streams {
		for _, item := range SortWorkByUrgency(ActionableWorkItems(stream)) {
			result.WriteString(item.ID)
			result.WriteByte(0)
		}
		result.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(result.String()))
	if got, want := hex.EncodeToString(digest[:]), "580ab8e83a0f432d4b77d972da51e820e9c637b5f9cb11340d4af14942f796c0"; got != want {
		t.Fatalf("attention population/order digest = %s, want %s", got, want)
	}
}

// TestTodo_WEB_098_Conformance checks that the shared selector keeps row
// fields intact, preserves source order, and partitions active from terminal
// work without aliasing the caller's input.
func TestTodo_WEB_098_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "first", Title: "Manager approval", Person: "A. Worker", Summary: "Promotion", ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"ASSIGNEE"}},
		{ID: "second", Title: "Repair", Person: "B. Worker", Tone: "danger", ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"CANDIDATE"}},
		{ID: "past", Title: "Completed", Terminal: true, ViewerResponsibility: "CLOSED"},
	}
	got := OpenWorkItems(stream)
	want := stream[:2]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("open attention projection = %#v, want %#v", got, want)
	}
	got[0].Title = "changed"
	if stream[0].Title != "Manager approval" {
		t.Fatal("attention projection aliases input records")
	}
	if len(OpenWorkItems(nil)) != 0 || len(OpenWorkItems([]WorkItem{{ID: "only-past", Terminal: true}})) != 0 {
		t.Fatal("empty or terminal-only stream produced attention")
	}
}

// TestTodo_WEB_098_Browser delegates to the Chromium scenario that opens the
// authenticated running product, rather than treating server HTML as browser
// evidence.
func TestTodo_WEB_098_Browser(t *testing.T) {
	t.Setenv("HCMNEXT_DEV_URL", "http://localhost:8888")
	runUXScanLiveBrowser(t, "WEB-098")
}
