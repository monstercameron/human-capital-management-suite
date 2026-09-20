package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RED for REV-069-01: the announcements floorplan slot resolves
// but renders nothing. GovernAnnouncements splits the stream,
// HomePageProps has no announcements field, and homePage never
// reads the slot, so a tenant-admin-authored announcement never
// reaches the rendered Home page.
func TestTodo_REV_069_01(t *testing.T) {
	view := testView(PageHome)
	view.Announcements = []Announcement{
		{ID: "maint", Message: "Maintenance Sunday 02:00 UTC"},
		{ID: "expiry", Message: "Session expiring soon", Assertive: true},
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Maintenance Sunday 02:00 UTC", "Session expiring soon", "home-announcements"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("home omits governed announcements: missing %q", want)
		}
	}
	politeAt := strings.Index(markup, "home-announcements-polite")
	assertiveAt := strings.Index(markup, "home-announcements-assertive")
	if politeAt < 0 || assertiveAt < 0 || assertiveAt < politeAt {
		t.Fatalf("announcement regions misordered: polite=%d assertive=%d", politeAt, assertiveAt)
	}
	if !strings.Contains(markup[assertiveAt:], "Session expiring soon") {
		t.Fatal("assertive claim outside the assertive region")
	}
	if strings.Contains(markup[politeAt:assertiveAt], "Session expiring soon") {
		t.Fatal("assertive claim duplicated in the polite region")
	}
	politeTagStart := strings.LastIndex(markup[:politeAt], "<div")
	politeTagEndRel := strings.Index(markup[politeAt:], ">")
	if politeTagStart < 0 || politeTagEndRel < 0 {
		t.Fatal("polite region has no opening tag")
	}
	politeOpen := markup[politeTagStart : politeAt+politeTagEndRel]
	if !strings.Contains(politeOpen, `aria-live="polite"`) {
		t.Fatal("polite region not marked polite")
	}
	if strings.Contains(politeOpen, `aria-live="assertive"`) {
		t.Fatal("polite region marked assertive")
	}

	// No stream: no region.
	bare, err := ui.RenderToString(homePage(testView(PageHome)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bare, "home-announcements") {
		t.Fatal("empty stream renders an announcements region")
	}

	// The props gate holds: content with ShowAnnouncements false
	// renders nothing.
	off, err := ui.RenderToString(ui.CreateElement(HomePage, HomePageProps{
		Announcements: AnnouncementsProps{Polite: []Announcement{{ID: "a", Message: "Hi"}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off, "home-announcements") {
		t.Fatal("gated-off announcements render")
	}
}

// Golden: announcement region splits and gates over a stream matrix.
func TestTodo_REV_069_01_Golden(t *testing.T) {
	streams := [][]Announcement{
		nil,
		{},
		{{ID: "a", Message: "Hi"}},
		{{ID: "a", Message: "Urgent", Assertive: true}, {ID: "b", Message: "Later"}},
		{{ID: "a", Message: "First", Assertive: true}, {ID: "b", Message: "Second", Assertive: true}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		view := testView(PageHome)
		view.Announcements = stream
		polite, assertive := GovernAnnouncements(stream)
		announcements, show := homeAnnouncementProps(view, polite, assertive)
		for _, item := range announcements.Polite {
			builder.WriteString("p:" + item.ID + "\x00")
		}
		for _, item := range announcements.Assertive {
			builder.WriteString("a:" + item.ID + "\x00")
		}
		if show {
			builder.WriteString("shown")
		} else {
			builder.WriteString("hidden")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "57fc48d578748a70e4453e2d463b1b5f346d072d79753df387f8bef5dd08bbeb"
	if got != want {
		t.Fatalf("announcement matrix digest = %s, want %s", got, want)
	}
}

// RED for REV-069-02: homePage hand-picks completion evidence per
// row, iterating RecentWork with inline title/status calls, while
// CompletedHistory — the derived-only record WEB-107 built
// precisely to prevent that drift — has no caller. The rail must
// derive from CompletedHistory mapped by ID instead.
func TestTodo_REV_069_02(t *testing.T) {
	view := testView(PageHome)
	completed := CompletedHistory(view.Work)
	rows := completedActivityProps(view, completed, view.Work)
	if len(rows) != len(completed) {
		t.Fatalf("rail maps %d rows for %d completed entries", len(rows), len(completed))
	}
	byID := make(map[string]WorkItem, len(view.Work))
	for _, item := range view.Work {
		byID[item.ID] = item
	}
	for i, entry := range completed {
		item := byID[entry.ID]
		if rows[i].Title != homeTaskLabel(view, item) || rows[i].Status != localizedWorkStatus(view.Locale, item) || rows[i].Href != item.Href || rows[i].Tone != item.Tone {
			t.Fatalf("row %d diverges from entry %q: %+v", i, entry.ID, rows[i])
		}
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !strings.Contains(markup, ">"+row.Title+"<") {
			t.Fatalf("completed row %q missing from home", row.Title)
		}
	}

	// The rail stays bounded: seven terminal items still show five rows.
	crowded := testView(PageHome)
	crowded.Work = nil
	for i := 1; i <= 7; i++ {
		id := "done"
		title := "Terminal"
		if i > 1 {
			id = "done-extra"
		}
		crowded.Work = append(crowded.Work, WorkItem{ID: id, Title: title, Status: "Stage", Terminal: true})
	}
	crowdedRows := completedActivityProps(crowded, CompletedHistory(crowded.Work), crowded.Work)
	if len(crowdedRows) != 7 {
		t.Fatalf("helper maps %d rows for 7 entries", len(crowdedRows))
	}
	crowdedMarkup, err := ui.RenderToString(homePage(crowded))
	if err != nil {
		t.Fatal(err)
	}
	shown := strings.Count(crowdedMarkup, ">Terminal<")
	if shown != 5 {
		t.Fatalf("crowded rail shows %d rows, want 5", shown)
	}
}

// Golden: completed-rail rows over a terminal stream matrix.
func TestTodo_REV_069_02_Golden(t *testing.T) {
	view := testView(PageHome)
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a", Title: "Alpha", Status: "Done", Terminal: true}},
		{{ID: "a", Title: "Alpha", Status: "Done"}, {ID: "b", Title: "Beta", Status: "Done", Terminal: true}},
		{{ID: "a", Title: "Alpha", Status: "Done", Terminal: true}, {ID: "b", Title: "Beta", Status: "Done", Terminal: true}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		for _, row := range completedActivityProps(view, CompletedHistory(stream), stream) {
			builder.WriteString(row.Title + "\x00" + row.Status + "\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "e6d662a849130ccc0b84a184fda9bd545a9f401f3a92751d067fa0cdabcd1f36"
	if got != want {
		t.Fatalf("completed-rail digest = %s, want %s", got, want)
	}
}
