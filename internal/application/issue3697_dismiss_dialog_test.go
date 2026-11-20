package application

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestIssue3697_DismissDialogIsServedToDeciders_Integration: the page serves
// the Dismiss confirmation for an item to a person who may decide it, names the
// author and carries the item's case; it refuses a request with no case, and a
// person who may decide nothing gets no dialog.
func TestIssue3697_DismissDialogIsServedToDeciders_Integration(t *testing.T) {
	f := chatmodSetup(t)
	post := f.post
	if code := f.report("reporter", post.ID, "spam", ""); code != http.StatusOK {
		t.Fatalf("report: %d", code)
	}
	report := chatmodItem(t, f.queue("owner"), "report")
	query := func(extra url.Values) string {
		q := url.Values{"action": {"dismiss"}, "conversation": {"room"}, "post": {post.ID}, "locale": {"en-US"}}
		for k, v := range extra {
			q[k] = v
		}
		return ChatModerationPagePath + "?" + q.Encode()
	}
	page := f.call("owner", http.MethodGet, query(url.Values{"case": {report.ID}}), nil)
	body := page.Body.String()
	for _, want := range []string{"Dismiss this item?", `data-chatremove="resolve"`, `value="dismiss"`, `name="note"`, "Alex Author", post.Body} {
		if page.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("the dismiss dialog misses %q: %d %s", want, page.Code, body)
		}
	}
	if strings.Contains(body, `name="reason"`) {
		t.Fatalf("a dismissal asks for a reason: %s", body)
	}
	if got := f.call("owner", http.MethodGet, query(nil), nil).Code; got != http.StatusBadRequest {
		t.Fatalf("a dismissal with no item: %d", got)
	}
	if got := f.call("member", http.MethodGet, query(url.Values{"case": {report.ID}}), nil).Code; got != http.StatusForbidden {
		t.Fatalf("a member was served the dismiss dialog: %d", got)
	}
	// The decision itself still takes an optional note and records it.
	if code := f.resolve("owner", report.ID, "dismiss", "", "Not aimed at anyone."); code != http.StatusOK {
		t.Fatalf("dismiss with a note: %d", code)
	}
	if got := f.rows(`SELECT reason FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2`, f.tenant, report.ID); len(got) != 1 || got[0] != "Not aimed at anyone." {
		t.Fatalf("the note is not recorded with the decision: %v", got)
	}
}
