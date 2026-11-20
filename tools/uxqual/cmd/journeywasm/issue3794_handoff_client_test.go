package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestIssue3794_ClientSendsTheReviewedOfferAndReadsTheStateBack(t *testing.T) {
	var method, path string
	var sent handoffOffer
	client := issue3790Client(200, `{"handoff":{"id":"wh-1","post_id":"m1","project_id":"ops","task_id":"t-1","mode":"FOLLOW_UP","role":"sender","to_id":"priya","next_action":"Confirm","status":"OFFERED"}}`, func(r *http.Request, body []byte) {
		method, path = r.Method, r.URL.Path
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Error(err)
		}
	})
	key := handoffNewKey()
	got, err := handoffSend(context.Background(), client, issue3790Config(), handoffOffer{ConversationID: "general", ProjectID: "ops", TaskID: "t-1", Mode: "FOLLOW_UP", RecipientID: "priya", NextAction: "Confirm", Note: "Soon", IdempotencyKey: key})
	if err != nil || got.ID != "wh-1" || got.Status != "OFFERED" {
		t.Fatalf("send = %+v, %v", got, err)
	}
	if method != http.MethodPost || path != "/api/chat/work-handoffs" || sent.RecipientID != "priya" || sent.IdempotencyKey != key || sent.Note != "Soon" {
		t.Fatalf("request = %s %s %+v", method, path, sent)
	}
}

func TestIssue3794_ClientListAndAnswerUseTheirOwnRoutes(t *testing.T) {
	var path, query, method string
	client := issue3790Client(200, `{"handoffs":[{"id":"wh-1","task_id":"t-1","status":"OFFERED"}],"handoff":{"id":"wh-1","task_id":"t-1","status":"ACCEPTED"}}`, func(r *http.Request, _ []byte) {
		path, query, method = r.URL.Path, r.URL.RawQuery, r.Method
	})
	list, err := handoffList(context.Background(), client, issue3790Config(), "general")
	if err != nil || len(list) != 1 || method != http.MethodGet || path != "/api/chat/work-handoffs" || query != "conversation_id=general" {
		t.Fatalf("list = %+v %v %s %s %s", list, err, method, path, query)
	}
	done, err := handoffAnswer(context.Background(), client, issue3790Config(), "wh-1", "accept")
	if err != nil || done.Status != "ACCEPTED" || method != http.MethodPost || path != "/api/chat/work-handoffs/wh-1/accept" {
		t.Fatalf("answer = %+v %v %s %s", done, err, method, path)
	}
}

func TestIssue3794_ClientAnswerRefusesBadIdsAndVerbsBeforeAnyRequest(t *testing.T) {
	calls := 0
	client := issue3790Client(200, `{}`, func(*http.Request, []byte) { calls++ })
	for _, c := range []struct{ id, verb string }{{"", "accept"}, {"a/b", "accept"}, {"wh-1", "erase"}, {"wh-1", ""}} {
		if _, err := handoffAnswer(context.Background(), client, issue3790Config(), c.id, c.verb); chatworkErrorCode(err) != "invalid_argument" {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if calls != 0 {
		t.Fatalf("%d requests were made for refused input", calls)
	}
}

func TestIssue3794_ClientKeepsOnlyOffersTheViewerCanOpenAndMergesAnswers(t *testing.T) {
	grouped := handoffByTask([]handoffJSON{
		{ID: "wh-1", TaskID: "t-1", ProjectID: "ops", PostID: "m1", Role: "recipient", Status: "OFFERED", NextAction: "Confirm"},
		{ID: "wh-2", Restricted: true, Role: "recipient", Status: "OFFERED", NextAction: "Secret"},
		{ID: "", TaskID: "t-9"},
	})
	if len(grouped) != 1 || len(grouped["t-1"]) != 1 {
		t.Fatalf("grouped = %+v", grouped)
	}
	merged := handoffMerge(grouped, handoffJSON{ID: "wh-1", TaskID: "t-1", ProjectID: "ops", PostID: "m1", Role: "recipient", Status: "ACCEPTED", NextAction: "Confirm"})
	if merged["t-1"][0].Status != "ACCEPTED" || grouped["t-1"][0].Status != "OFFERED" || len(merged["t-1"]) != 1 {
		t.Fatalf("merged = %+v (original %+v)", merged, grouped)
	}
	if k := handoffNewKey(); !strings.HasPrefix(k, "handoff-") || len(k) < 16 || k == handoffNewKey() {
		t.Fatalf("key %q", k)
	}
}

func TestIssue3794_ClientReducesRefusalsToTheServersCode(t *testing.T) {
	client := issue3790Client(409, `{"code":"conflict","detail":"Fix March payroll"}`, nil)
	_, err := handoffAnswer(context.Background(), client, issue3790Config(), "wh-1", "accept")
	if chatworkErrorCode(err) != "conflict" || strings.Contains(err.Error(), "Fix March") {
		t.Fatalf("err = %v", err)
	}
}
