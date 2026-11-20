package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3792: Record decision sends the typed words to the decisions route
// with the attempt's key, and never a person: the server knows who is asking.
func TestIssue3792_ClientSendsTheRecordWithItsKey(t *testing.T) {
	var path, method string
	var sent map[string]any
	client := issue3790Client(200, `{"decision":{"id":"d1","text":"Freeze","informal":true}}`, func(r *http.Request, body []byte) {
		path, method = r.URL.Path, r.Method
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Error(err)
		}
	})
	key := decisionsNewKey()
	reply, err := decisionsCall(context.Background(), client, issue3790Config(), "record", decisionsCommand{ConversationID: "room", Text: "Freeze", Rationale: "calibration", SupersedesID: "d0", IdempotencyKey: key, Label: "Decision (informal)"})
	if err != nil || reply.Decision == nil || reply.Decision.ID != "d1" || path != "/api/chat/decisions/v1/record" || method != http.MethodPost {
		t.Fatalf("reply=%+v err=%v path=%s method=%s", reply, err, path, method)
	}
	if sent["idempotency_key"] != key || sent["supersedes_id"] != "d0" || sent["text"] != "Freeze" || sent["rationale"] != "calibration" {
		t.Fatalf("sent=%v", sent)
	}
	for _, forbidden := range []string{"owner_id", "subject", "tenant_id", "person"} {
		if _, ok := sent[forbidden]; ok {
			t.Errorf("the request names %q", forbidden)
		}
	}
	if !strings.HasPrefix(key, "decision-") || len(key) < 20 || decisionsNewKey() == key {
		t.Fatalf("keys: %q", key)
	}
	if _, err := decisionsCall(context.Background(), client, issue3790Config(), "delete", decisionsCommand{}); decisionsErrorCode(err) != "unavailable" {
		t.Fatalf("an unknown action reached the network: %v", err)
	}
}

func TestIssue3792_ClientReducesRefusalsToTheirCodes(t *testing.T) {
	cases := map[int]string{403: "permission_denied", 404: "not_found", 400: "invalid_argument", 409: "conflict", 503: "unavailable", 500: "unavailable"}
	bodies := map[int]string{403: `{"code":"permission_denied"}`, 404: `{"code":"not_found"}`, 400: `{"code":"invalid_argument"}`, 409: `{"code":"conflict"}`, 503: `{"code":"unavailable"}`, 500: `<html>secret stack</html>`}
	for status, want := range cases {
		_, err := decisionsCall(context.Background(), issue3790Client(status, bodies[status], nil), issue3790Config(), "list", decisionsCommand{ConversationID: "room"})
		if got := decisionsErrorCode(err); got != want {
			t.Errorf("status %d: code %q, want %q", status, got, want)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Errorf("status %d: the refusal body leaked: %v", status, err)
		}
	}
	if _, err := decisionsCall(context.Background(), issue3790Client(200, `not json`, nil), issue3790Config(), "list", decisionsCommand{}); decisionsErrorCode(err) != "unavailable" {
		t.Errorf("a garbled answer: %v", err)
	}
	bad := issue3790Config()
	bad.Bearer = ""
	if _, err := decisionsCall(context.Background(), issue3790Client(200, `{}`, nil), bad, "list", decisionsCommand{}); decisionsErrorCode(err) != "unavailable" {
		t.Errorf("no bearer reached the network: %v", err)
	}
}

func TestIssue3792_ItemsNameWhoRecordedThem(t *testing.T) {
	model := chatui.Model{CurrentUser: "me", CurrentTenantID: "t", Members: []chatui.Member{{ID: "dana", HomeTenantID: "t", Name: "Dana Reyes"}, {ID: "dana", HomeTenantID: "other", Name: "Dana Elsewhere"}}}
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	list := []decisionsJSON{
		{ID: "a", Text: "mine", OwnerID: "me", OwnerHomeTenantID: "t", DecidedAt: at},
		{ID: "b", Text: "danas", OwnerID: "dana", OwnerHomeTenantID: "other", DecidedAt: at, SupersededByID: "c", CanReplace: true},
		{ID: "c", Text: "gone", OwnerID: "left", OwnerHomeTenantID: "t", DecidedAt: at, SupersedesID: "b", SourcePostID: "p", SourceSequence: 9},
	}
	items := decisionsItems(model, list, "you", "a former member")
	if items[0].Owner != "you" || items[1].Owner != "Dana Elsewhere" || items[2].Owner != "a former member" || !items[1].CanReplace || items[2].SourceSequence != 9 || items[2].SupersedesID != "b" {
		t.Fatalf("items=%+v", items)
	}
	// The same subject id in another company is not the viewer.
	if got := decisionsOwnerName(model, "other", "me", "you", "?"); got == "you" {
		t.Fatalf("a person of another company was shown as the viewer")
	}
	if n := decisionsInForce(list); n != 2 {
		t.Fatalf("in force: %d", n)
	}
}
