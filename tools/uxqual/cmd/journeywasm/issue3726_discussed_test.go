package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestIssue3726_ClientReadsAndRecordsOnTheJourneyRoute_Integration(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	client := issue3790Client(200, `{"items":[{"conversation_id":"general","name":"general","href":"/workspace/app/chat#channel=general"}],"incomplete":true}`, func(r *http.Request, body []byte) {
		gotPath, gotMethod, gotBody = r.URL.EscapedPath(), r.Method, string(body)
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s", r.URL)
		}
	})
	reply, err := journeyDiscussionsCall(context.Background(), client, issue3790Config(), "intent 1", "")
	if err != nil || len(reply.Items) != 1 || !reply.Incomplete || gotMethod != http.MethodGet || gotPath != "/api/chat/journeys/intent%201/discussions" || gotBody != "" {
		t.Fatalf("read: %+v %v %s %s %q", reply, err, gotMethod, gotPath, gotBody)
	}
	noContent := issue3790Client(204, "", func(r *http.Request, body []byte) { gotMethod, gotBody = r.Method, string(body) })
	if _, err := journeyDiscussionsCall(context.Background(), noContent, issue3790Config(), "j1", "general"); err != nil || gotMethod != http.MethodPost || gotBody != `{"conversation_id":"general"}` {
		t.Fatalf("record: %v %s %q", err, gotMethod, gotBody)
	}
	if _, err := journeyDiscussionsCall(context.Background(), client, issue3790Config(), "a/b", ""); chatworkErrorCode(err) != "invalid_argument" {
		t.Fatalf("a journey id with a slash reached the network: %v", err)
	}
}

func TestIssue3726_ClientReducesRefusalsToTheirCodes_Security(t *testing.T) {
	for status, code := range map[int]string{403: "permission_denied", 404: "not_found", 503: "unavailable"} {
		client := issue3790Client(status, `{"code":"`+code+`","detail":"board-only conversation"}`, nil)
		_, err := journeyDiscussionsCall(context.Background(), client, issue3790Config(), "j1", "")
		if chatworkErrorCode(err) != code || strings.Contains(err.Error(), "board-only") {
			t.Errorf("status %d: %v", status, err)
		}
	}
	cfg := issue3790Config()
	cfg.Bearer = ""
	if _, err := journeyDiscussionsCall(context.Background(), issue3790Client(200, `{}`, nil), cfg, "j1", ""); chatworkErrorCode(err) != "unavailable" {
		t.Fatalf("no bearer: %v", err)
	}
}

func TestIssue3726_RowsLinkOnlyChatAddressesAndNameDirectMessagesByTheirOwnWord(t *testing.T) {
	rows := journeyDiscussedRows([]journeyDiscussionJSON{
		{Name: "general", Href: "/workspace/app/chat#channel=general"},
		{Name: "dana, lee", Direct: true, Href: "/workspace/app/chat#channel=dm"},
		{Name: "evil", Href: "https://evil.example/chat"},
		{Name: "evil2", Href: "//evil.example"},
		{Name: "x", Href: "javascript:alert(1)"},
	}, "Direct message", "Open conversation {name}")
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Label != "general" || rows[0].Aria != "Open conversation general" {
		t.Fatalf("channel %+v", rows[0])
	}
	if rows[1].Label != "Direct message" || strings.Contains(rows[1].Aria, "dana") {
		t.Fatalf("a direct message was named by a stored title: %+v", rows[1])
	}
}

func TestIssue3726_JourneyIDComesOnlyFromTheJourneyAddress(t *testing.T) {
	cases := map[string]string{
		"/workspace/app/journeys?journey=abc-1":       "abc-1",
		"/workspace/app/journeys?journey=a%20b&nav=1": "a b",
		"/workspace/app/projects?journey=abc":         "",
		"/workspace/app/journeys":                     "",
	}
	for href, want := range cases {
		if got := journeyIDFromHref(href); got != want {
			t.Errorf("%s: %q, want %q", href, got, want)
		}
	}
}
