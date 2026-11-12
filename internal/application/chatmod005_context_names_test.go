package application

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatmodContextFixture serves one open report whose "Show the conversation
// around it" neighbours are written by a second person, and records which
// author identifiers the page asks the directory to name.
type chatmodContextFixture struct {
	chat.ModerationStore
	requested []string
}

func (f *chatmodContextFixture) ModerationNotices(context.Context, chat.Principal, string) ([]chat.ModerationNotice, error) {
	return nil, nil
}

func (f *chatmodContextFixture) SearchModeration(context.Context, chat.Principal, string, string) ([]chat.ModerationItem, error) {
	return []chat.ModerationItem{{
		ID: "report:one", Kind: "report", State: "OPEN", ConversationID: "people-ops", ConversationName: "people-ops",
		PostID: "post-b", AuthorID: "nia", ReporterID: "reporter", Reason: "spam",
		Message: chat.Post{ID: "post-b", AuthorID: "nia", Body: "reported body", CreatedAt: time.Unix(100, 0)},
		Context: []chat.Post{
			{ID: "post-a", AuthorID: "nia", Body: "before", CreatedAt: time.Unix(90, 0)},
			{ID: "post-b", AuthorID: "nia", Body: "reported body", CreatedAt: time.Unix(100, 0)},
			{ID: "post-c", AuthorID: "rosa", Body: "neighbour body", CreatedAt: time.Unix(110, 0)},
		},
		CanRemove: true,
	}}, nil
}

func (f *chatmodContextFixture) CanModerate(context.Context, chat.Principal, string, string, string) error {
	return nil
}

func (f *chatmodContextFixture) CanReportMessage(context.Context, chat.Principal, string, string, string) error {
	return nil
}

func (f *chatmodContextFixture) AssignModerationPermission(context.Context, chat.Principal, string, string, string, string, bool) error {
	return nil
}

// chatmodContextDirectory answers names only for the identifiers it is asked
// for, the way the workforce directory does.
type chatmodContextDirectory struct {
	fixture *chatmodContextFixture
	names   map[string]string
}

func (d chatmodContextDirectory) ModerationNames(_ context.Context, _ chat.Principal, _ string, ids []string) (map[string]string, error) {
	d.fixture.requested = append(d.fixture.requested, ids...)
	out := map[string]string{}
	for _, id := range ids {
		if name := d.names[id]; name != "" {
			out[id] = name
		}
	}
	return out, nil
}

// TestTodo_ISSUE_3694_ContextAuthorsAreNamed pins issue 3694: the neighbours
// shown by "Show the conversation around it" must carry the author's name, not
// the anonymous "Someone" label, when the directory knows that author. The
// page only asked the directory for the queue item's own people, so a
// neighbour written by someone else lost her name.
func TestTodo_ISSUE_3694_ContextAuthorsAreNamed(t *testing.T) {
	f := &chatmodContextFixture{}
	h := ChatModerationHTTP{Service: &chat.ModerationService{Store: f}, Permissions: f}
	dir := chatmodContextDirectory{fixture: f, names: map[string]string{"nia": "Nia Brooks", "rosa": "Rosa Santos"}}
	page := ChatModerationPageHTTP{HTTP: h, Directory: dir}

	r := httptest.NewRequest("GET", "/chat/moderation?locale=en-US", nil)
	r = r.WithContext(trust.WithPrincipal(r.Context(), personaDraftPrincipal(t, "tenant-a", "reviewer", time.Now())))
	w := httptest.NewRecorder()
	page.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("page status=%d", w.Code)
	}
	if !slices.Contains(f.requested, "rosa") {
		t.Fatalf("directory was not asked to name the neighbour's author; asked for %v", f.requested)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Rosa Santos") || !strings.Contains(body, "neighbour body") {
		t.Fatalf("neighbour is not shown with her name: body does not contain Rosa Santos next to her message")
	}
}
