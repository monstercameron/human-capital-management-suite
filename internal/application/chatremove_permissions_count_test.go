package application

import (
	"net/http"
	"strings"
	"testing"
)

// TestTodo_3695_PermissionsTabKeepsResolvedCount: the moderation tab strip on
// the Permissions tab shows the Resolved count as the queue's tabs do, so the
// segment does not lose its number when the administrator opens Permissions.
func TestTodo_3695_PermissionsTabKeepsResolvedCount(t *testing.T) {
	f := chatmodSetup(t)
	page := func(query string) string {
		response := f.call("admin", http.MethodGet, ChatModerationPagePath+"?locale=en-US"+query, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("moderation page %q: %d %s", query, response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	queue := page("")
	permissions := page("&tab=permissions")
	if !strings.Contains(permissions, ">Permissions<") || strings.Count(permissions, `role="switch"`) == 0 {
		t.Fatalf("the Permissions tab is not drawn: %s", permissions)
	}
	// Open and Resolved each carry a count on the queue; the Permissions tab must
	// carry the same two counts.
	want := strings.Count(queue, `chatsave-seg-count`)
	if want != 2 {
		t.Fatalf("the queue tabs carry %d counts, want 2: %s", want, queue)
	}
	if got := strings.Count(permissions, `chatsave-seg-count`); got != want {
		t.Fatalf("the Permissions tab carries %d counts, want %d: %s", got, want, permissions)
	}
}
