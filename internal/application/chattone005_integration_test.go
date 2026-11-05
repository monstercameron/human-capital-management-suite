package application

import (
	"strings"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// TestTodo_CHATTONE_005_Integration: the served reword paths a member reads
// while the rewording is still produced, and the per-message read once it is
// ready. Through the served composition over the test PostgreSQL with
// originals hidden: before the rewording is ready a saved message and a link
// preview carry a stand-in (never the original, never nothing), and the
// member's search matches nothing of the original; once ready the per-message
// read carries the reworded text as ready, still hiding the original. The
// writer reads what they wrote either way.
func TestTodo_CHATTONE_005_Integration(t *testing.T) {
	rig, setting := chattoneY2Rig(t, chattoneRewordReplayFixture(t))
	setting(chatstore.RewordModeOffered, false)
	heated := rig.send("chattone005-integration-heated", w10Heated)

	saved := func(subject string) string {
		t.Helper()
		ref := chatcore.SavedRequest{Principal: rig.person(subject), TenantID: "host", ConversationID: rig.room, PostID: heated.ID}
		if _, err := rig.chat.core.SaveForLater(rig.as(subject), ref); err != nil {
			t.Fatal(err)
		}
		page, err := rig.chat.core.ListSaved(rig.as(subject), chatcore.SavedListRequest{Principal: rig.person(subject), TenantID: "host", Tab: chatcore.SavedAll, Page: chatcore.Page{PageSize: 10}})
		if err != nil || len(page.Items) != 1 || page.Items[0].Post == nil {
			t.Fatalf("saved list: %+v %v", page, err)
		}
		return page.Items[0].Post.Body
	}
	linked := func(subject string) string {
		t.Helper()
		reader, ok := rig.chat.service.(chatcore.AuthorizedReferenceReader)
		if !ok {
			t.Fatal("the served chat service has no reference reader")
		}
		_, post, err := reader.ReadAuthorizedReference(rig.as(subject), rig.person(subject), "host", rig.room, heated.ID)
		if err != nil || post == nil {
			t.Fatalf("link read: %v", err)
		}
		return post.Body
	}
	search := func(subject, query string) []chatcore.SearchResult {
		t.Helper()
		found, err := rig.chat.service.Search(rig.as(subject), chatcore.SearchRequest{Principal: rig.person(subject), TenantID: "host", Query: query, Page: chatcore.Page{PageSize: 20}})
		if err != nil {
			t.Fatal(err)
		}
		return found.Results
	}

	// While the rewording is produced: stand-ins, never the original and
	// never nothing, and no search match against text the member may not read.
	if body := saved("bruno"); body == "" || strings.Contains(body, "idiot") {
		t.Fatalf("the member's saved message before ready: %q", body)
	}
	if body := linked("bruno"); body == "" || strings.Contains(body, "idiot") {
		t.Fatalf("the member's link preview before ready: %q", body)
	}
	if hits := search("bruno", "idiot"); len(hits) != 0 {
		t.Fatalf("a member's search matched the original before the rewording was ready: %+v", hits)
	}

	// Once ready the per-message read is the reworded text, marked ready, with
	// the original still hidden from the member.
	rig.drain()
	got, mark := rig.read("bruno", heated)
	if got.Text != w10Reword {
		t.Fatalf("the member's per-message read when ready: %q", got.Text)
	}
	if mark.State != "ready" || mark.CanShowOriginal {
		t.Fatalf("the member's ready mark: %+v", mark)
	}
	reworded := false
	for _, kind := range mark.Kinds {
		reworded = reworded || kind == chatrender.Reword
	}
	if !reworded {
		t.Fatalf("the ready mark names no rewording: %+v", mark)
	}

	// The writer reads what they wrote, before and after.
	if body := saved("alice"); body != w10Heated {
		t.Fatalf("the writer's saved message: %q", body)
	}
	if body := linked("alice"); body != w10Heated {
		t.Fatalf("the writer's link preview: %q", body)
	}
	if got, _ := rig.read("alice", heated); got.Text != w10Heated {
		t.Fatalf("the writer's per-message read: %q", got.Text)
	}
}
