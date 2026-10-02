package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_CHATMOD_004_Selected: the form of ticked messages sends exactly the
// ticked ones as the selection, and says so itself when none is ticked.
func TestTodo_CHATMOD_004_Selected(t *testing.T) {
	values := map[string]string{"conversation": "room", "reason": "spam", "removal_action": "remove"}
	input, err := chatremoveFormInput("preview", values, []string{"p1", "p3"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection := input.Removal.Selection
	if selection.ConversationID != "room" || strings.Join(selection.PostIDs, ",") != "p1,p3" || selection.AuthorID != "" || !selection.From.IsZero() || input.Removal.ReasonCode != "spam" || input.Removal.Action != "remove" {
		t.Fatalf("selection=%+v removal=%+v", selection, input.Removal)
	}
	// The server accepts this selection as it is.
	request := input.Removal
	request.Principal, request.TenantID = chat.Principal{TenantID: "t", SubjectID: "admin"}, "t"
	if err = chat.ValidateRemoval(request); err != nil {
		t.Fatalf("the server would refuse the selection: %v", err)
	}
	if _, err = chatremoveFormInput("preview", values, nil, nil); !errors.Is(err, errChatmod004NothingSelected) {
		t.Fatalf("nothing ticked: %v", err)
	}
	if _, err = chatremoveFormInput("apply", values, nil, nil); !errors.Is(err, errChatmod004NothingSelected) {
		t.Fatalf("nothing ticked at confirmation: %v", err)
	}
	if key := chatremoveErrorKey(errChatmod004NothingSelected); key != "none_selected" {
		t.Fatalf("the refusal is said as %q", key)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		if chatui.ModerationText(locale, "none_selected") == "" {
			t.Errorf("%s: nothing ticked has no words", locale)
		}
	}
	if got := chatui.ModerationText("en-US", "none_selected"); got != "Tick at least one message first." {
		t.Fatalf("English sentence is %q", got)
	}
	// The one-message form and the time-range form are unchanged by the check.
	if input, err = chatremoveFormInput("quick", map[string]string{"conversation": "room", "post": "p9", "reason": "spam"}, []string{"p9"}, nil); err != nil || len(input.Removal.Selection.PostIDs) != 1 {
		t.Fatalf("one message: %+v %v", input, err)
	}
	if _, err = chatremoveFormInput("preview", map[string]string{"conversation": "room", "author": "a", "author_home": "t", "from": "2026-10-01T08:00", "until": "2026-10-01T09:00"}, nil, time.UTC); err != nil {
		t.Fatalf("a time range: %v", err)
	}
	// A report or a decision carries no selection and is not refused.
	if _, err = chatremoveFormInput("report", map[string]string{"conversation": "room", "post": "p1"}, nil, nil); err != nil {
		t.Fatalf("a report: %v", err)
	}
}

// TestTodo_CHATMOD_004_Restored: an edit that brings back a message the page
// holds as removed marks it restored; an ordinary edit does not; a second
// removal forgets it.
func TestTodo_CHATMOD_004_Restored(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	model := chatui.Model{
		Messages:       []chatui.Message{{ID: "p1", Body: chat.RemovedByAdministrator, Revision: 4}, {ID: "p2", Body: "ordinary", Revision: 1}},
		ThreadMessages: []chatui.Message{{ID: "p5", Body: chat.RemovedByAdministrator, Revision: 2}},
		ThreadParent:   &chatui.Message{ID: "p6", Body: chat.RemovedByAdministrator, Revision: 2},
	}
	applyChatPostEdited(&model, &chatv1.Post{Id: "p2", Body: "ordinary, edited", Revision: 2}, "en-US", nil, now)
	if len(model.Moderation.Restored) != 0 {
		t.Fatalf("an ordinary edit was taken for a restore: %v", model.Moderation.Restored)
	}
	applyChatPostEdited(&model, &chatv1.Post{Id: "p1", Body: "the text again", Revision: 5}, "en-US", nil, now)
	if !model.Moderation.Restored["p1"] || model.Messages[0].Body != "the text again" {
		t.Fatalf("a restore was not noted: %v %+v", model.Moderation.Restored, model.Messages[0])
	}
	applyChatPostEdited(&model, &chatv1.Post{Id: "p5", Body: "a reply again", Revision: 3}, "en-US", nil, now)
	applyChatPostEdited(&model, &chatv1.Post{Id: "p6", Body: "a parent again", Revision: 3}, "en-US", nil, now)
	if !model.Moderation.Restored["p5"] || !model.Moderation.Restored["p6"] {
		t.Fatalf("a restore in the thread was not noted: %v", model.Moderation.Restored)
	}
	// An event that still says "removed", a message the page does not hold, and
	// a nil post change nothing.
	before := len(model.Moderation.Restored)
	chatmod004NoteRestore(&model, &chatv1.Post{Id: "p1", Deleted: true, Body: chat.RemovedByAdministrator})
	chatmod004NoteRestore(&model, &chatv1.Post{Id: "unknown", Body: "text"})
	chatmod004NoteRestore(&model, nil)
	if len(model.Moderation.Restored) != before {
		t.Fatalf("restored=%v", model.Moderation.Restored)
	}
	// Removed again: the line would contradict the tombstone.
	applyChatPostDeleted(&model, &chatv1.Post{Id: "p1", Deleted: true, Body: chat.RemovedByAdministrator, Revision: 6})
	if model.Moderation.Restored["p1"] || model.Messages[0].Body != chat.RemovedByAdministrator || !model.Moderation.Restored["p5"] {
		t.Fatalf("after a second removal: %v %+v", model.Moderation.Restored, model.Messages[0])
	}
}

// TestTodo_CHATMOD_005_Permissions: a switch of the Permissions tab becomes one
// assignment on the wire; other requests do not carry its fields.
func TestTodo_CHATMOD_005_Permissions(t *testing.T) {
	input, ok := chatmod005PermissionInput(" hr_partner ", chat.PermissionReviewRemovedMessages, "true", "room")
	if !ok || input.Role != "hr_partner" || input.Permission != chat.PermissionReviewRemovedMessages || !input.Allowed || input.ConversationID != "room" {
		t.Fatalf("input=%+v ok=%v", input, ok)
	}
	for _, bad := range [][4]string{{"", chat.PermissionReport, "true", ""}, {"MANAGER", "Delete everything", "true", ""}, {"MANAGER", chat.PermissionReport, "yes", ""}, {"MANAGER", chat.PermissionReport, "", ""}} {
		if _, ok := chatmod005PermissionInput(bad[0], bad[1], bad[2], bad[3]); ok {
			t.Errorf("accepted %v", bad)
		}
	}
	var sent map[string]any
	var path string
	client := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		sent = map[string]any{}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Errorf("body %s: %v", body, err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"assigned":true}`))}, nil
	})}
	cfg := journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "fixture"}
	if _, err := chatremoveRequest(context.Background(), client, cfg, "permissions", "", input); err != nil {
		t.Fatal(err)
	}
	if path != "/api/chat/moderation/permissions" || sent["Role"] != "hr_partner" || sent["Permission"] != chat.PermissionReviewRemovedMessages || sent["Allowed"] != true || sent["ConversationID"] != "room" {
		t.Fatalf("path=%s sent=%v", path, sent)
	}
	// Taking a permission away is the same request with Allowed left out, which
	// the server reads as false.
	off, _ := chatmod005PermissionInput("MANAGER", chat.PermissionManageFilters, "false", "")
	if _, err := chatremoveRequest(context.Background(), client, cfg, "permissions", "", off); err != nil {
		t.Fatal(err)
	}
	if allowed, present := sent["Allowed"]; present && allowed != false {
		t.Fatalf("sent=%v", sent)
	}
	if sent["Role"] != "MANAGER" {
		t.Fatalf("sent=%v", sent)
	}
	// A decision on a queue item carries none of the three fields: the server
	// refuses a field it does not expect on that route only by name, so they
	// must not appear as empty values either.
	if _, err := chatremoveRequest(context.Background(), client, cfg, "resolve", "", chatremoveClientInput{CaseID: "report:1", Action: "dismiss"}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"Role", "Permission", "Allowed"} {
		if _, present := sent[field]; present {
			t.Errorf("a decision carries %s: %v", field, sent)
		}
	}
	href := chatmod005RoleHref(chatui.ModerationPageHref+"?locale=de-DE&tab=open&conversation=room&tz=120", " payroll admin ")
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != chatui.ModerationPageHref {
		t.Fatalf("href=%s err=%v", href, err)
	}
	if q := parsed.Query(); q.Get("tab") != "permissions" || q.Get("role") != "payroll admin" || q.Get("locale") != "de-DE" || q.Get("conversation") != "room" || q.Get("tz") != "120" {
		t.Fatalf("query=%v", parsed.Query())
	}
	if again, _ := url.Parse(chatmod005RoleHref(href, "")); again.Query().Has("role") || again.Query().Get("tab") != "permissions" {
		t.Fatalf("an empty role keeps a row: %s", again)
	}
	// The page request accepts the tab's address.
	page := &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("tab") != "permissions" || r.URL.Query().Get("role") != "payroll admin" {
			t.Errorf("page query=%v", r.URL.Query())
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<main></main>`))}, nil
	})}
	if _, err = chatremovePageRequest(context.Background(), page, cfg, href); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CHATMOD_003_Hits: reading the matches again replaces what is shown;
// an older page is added to it without listing a match twice.
func TestTodo_CHATMOD_003_Hits(t *testing.T) {
	record := func(id int64) chatfilter.Record { return chatfilter.Record{ID: id} }
	held := []chatfilter.Record{record(9), record(8), record(7)}
	if got := chatmod003OlderCursor(held); got != 7 {
		t.Fatalf("cursor=%d, want the oldest match held", got)
	}
	if got := chatmod003OlderCursor(nil); got != 0 {
		t.Fatalf("cursor of nothing=%d", got)
	}
	older := chatmod003MergeHits(held, []chatfilter.Record{record(7), record(6), record(5)}, 7)
	var ids []int64
	for _, r := range older {
		ids = append(ids, r.ID)
	}
	if len(ids) != 5 || ids[0] != 9 || ids[3] != 6 || ids[4] != 5 {
		t.Fatalf("after an older page: %v", ids)
	}
	if fresh := chatmod003MergeHits(older, []chatfilter.Record{record(10), record(9)}, 0); len(fresh) != 2 || fresh[0].ID != 10 {
		t.Fatalf("after reading again: %+v", fresh)
	}
	// The client asks the route the server serves, with the cursor.
	var asked *url.URL
	api := FilterAPIClient{BaseURL: "https://fixture.invalid", Client: &http.Client{Transport: chatremoveHTTPTransport(func(r *http.Request) (*http.Response, error) {
		asked = r.URL
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"ID":6,"Channel":"room","Hit":{"RuleName":"Client names","Action":"block","DryRun":true}}]`))}, nil
	})}}
	records, err := api.SearchHits(context.Background(), "", "room", 7)
	if err != nil || len(records) != 1 || records[0].ID != 6 || !records[0].Hit.DryRun || records[0].Hit.RuleName != "Client names" {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if asked.Path != "/api/chat/filters/v1/hits" || asked.Query().Get("channel") != "room" || asked.Query().Get("before") != "7" {
		t.Fatalf("asked %s", asked)
	}
	if chatmod003HitsPage != 200 {
		t.Fatalf("the page size is %d; the server answers 200 at once", chatmod003HitsPage)
	}
}
