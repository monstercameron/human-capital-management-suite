package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3804: Remember sends the message to propose and never a person or a
// state: the server knows who is asking and decides everything else.
func TestIssue3804_ClientProposesAMessageAndNothingElse(t *testing.T) {
	var path, method string
	var sent map[string]any
	client := issue3790Client(200, `{"memory":{"id":"m1","text":"Enrollment ends Nov 30","status":"proposed","source":"live"}}`, func(r *http.Request, body []byte) {
		path, method = r.URL.Path, r.Method
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Error(err)
		}
	})
	reply, err := memoryCall(context.Background(), client, issue3790Config(), "propose", memoryCommand{ConversationID: "room", SourcePostID: "p1"})
	if err != nil || reply.Memory == nil || reply.Memory.ID != "m1" || path != "/api/chat/memory/v1/propose" || method != http.MethodPost {
		t.Fatalf("reply=%+v err=%v path=%s method=%s", reply, err, path, method)
	}
	if sent["source_post_id"] != "p1" || sent["conversation_id"] != "room" {
		t.Fatalf("sent=%v", sent)
	}
	for _, forbidden := range []string{"proposer_id", "status", "text", "audience", "ttl_days", "tenant_id", "person"} {
		if _, ok := sent[forbidden]; ok {
			t.Errorf("the request names %q", forbidden)
		}
	}
	if _, err := memoryCall(context.Background(), client, issue3790Config(), "delete", memoryCommand{}); memoryErrorCode(err) != "unavailable" {
		t.Fatalf("an unknown action reached the network: %v", err)
	}
}

func TestIssue3804_ClientReducesRefusalsToTheirCodes(t *testing.T) {
	cases := map[int]string{403: "permission_denied", 404: "not_found", 400: "invalid_argument", 409: "conflict", 503: "unavailable", 500: "unavailable"}
	bodies := map[int]string{403: `{"code":"permission_denied"}`, 404: `{"code":"not_found"}`, 400: `{"code":"invalid_argument"}`, 409: `{"code":"conflict"}`, 503: `{"code":"unavailable"}`, 500: `<html>secret stack</html>`}
	for status, want := range cases {
		_, err := memoryCall(context.Background(), issue3790Client(status, bodies[status], nil), issue3790Config(), "forget", memoryCommand{ConversationID: "room", ID: "m1"})
		if got := memoryErrorCode(err); got != want {
			t.Errorf("status %d: code %q, want %q", status, got, want)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Errorf("status %d: the refusal body leaked: %v", status, err)
		}
	}
}

func TestIssue3804_ItemsNameWhoAddedThemAndKeepRetiredNotesWordless(t *testing.T) {
	model := chatui.Model{CurrentUser: "me", CurrentTenantID: "t", Members: []chatui.Member{{ID: "ana", Name: "Ana"}}}
	list := []memoryJSON{
		{ID: "m1", Text: "Enrollment ends Nov 30", ProposerID: "ana", ProposerHomeTenantID: "t", Status: "APPROVED", Source: "LIVE", CanForget: true},
		{ID: "m2", ProposerID: "me", ProposerHomeTenantID: "t", Status: "proposed", Source: "removed", CanForget: true},
		{ID: "m3", ProposerID: "gone", ProposerHomeTenantID: "t", Status: "proposed"},
	}
	items := memoryItems(model, list, "you", "a former member")
	if items[0].Proposer != "Ana" || items[0].Status != "approved" || items[0].Source != "live" || !items[0].CanForget {
		t.Errorf("first %+v", items[0])
	}
	if items[1].Proposer != "you" || items[1].Source != "removed" || items[1].Text != "" {
		t.Errorf("second %+v", items[1])
	}
	if items[2].Proposer != "a former member" || items[2].Source != "live" || items[2].CanForget {
		t.Errorf("third %+v", items[2])
	}
}
