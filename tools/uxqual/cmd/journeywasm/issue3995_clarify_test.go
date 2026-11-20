package main

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestIssue3995_ClientReadsAndAsksOnTheClarificationRoute_Integration(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	client := issue3790Client(200, `{"can_ask":true,"case_open":true,"status":"awaiting_you","revision":"rev-1","worker_name":"Avery","audience":{"kind":"direct","recipient_name":"Riley","private":true},"items":[{"id":"a","role":"asker","state":"current","href":"/workspace/app/chat#channel=dm"}],"incomplete":false}`, func(r *http.Request, body []byte) {
		gotPath, gotMethod, gotBody = r.URL.EscapedPath(), r.Method, string(body)
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s", r.URL)
		}
	})
	view, _, err := journeyClarifyCall(context.Background(), client, issue3790Config(), "intent 1", nil)
	if err != nil || !view.CanAsk || view.Revision != "rev-1" || view.Audience == nil || view.Audience.RecipientName != "Riley" || len(view.Items) != 1 ||
		gotMethod != http.MethodGet || gotPath != "/api/chat/journeys/intent%201/clarification" || gotBody != "" {
		t.Fatalf("read: %+v %v %s %s %q", view, err, gotMethod, gotPath, gotBody)
	}
	created := issue3790Client(201, `{"item":{"id":"a","role":"asker","state":"current","revision":"rev-1","href":"/workspace/app/chat#channel=dm"}}`, func(r *http.Request, body []byte) { gotMethod, gotBody = r.Method, string(body) })
	_, asked, err := journeyClarifyCall(context.Background(), created, issue3790Config(), "j1", &clarifyAskJSON{Revision: "rev-1", Question: "Why?", IdempotencyKey: "clarify-0123456789ab"})
	if err != nil || asked.Item.ID != "a" || gotMethod != http.MethodPost || gotBody != `{"revision":"rev-1","question":"Why?","idempotency_key":"clarify-0123456789ab"}` {
		t.Fatalf("ask: %+v %v %s %q", asked, err, gotMethod, gotBody)
	}
	if _, _, err := journeyClarifyCall(context.Background(), client, issue3790Config(), "a/b", nil); chatworkErrorCode(err) != "invalid_argument" {
		t.Fatalf("a journey id with a slash reached the network: %v", err)
	}
}

func TestIssue3995_ClientReducesRefusalsToTheirCodes_Security(t *testing.T) {
	for status, code := range map[int]string{403: "permission_denied", 404: "not_found", 409: "revision_changed", 422: "no_recipient", 503: "unavailable"} {
		client := issue3790Client(status, `{"code":"`+code+`","detail":"private question text"}`, nil)
		_, _, err := journeyClarifyCall(context.Background(), client, issue3790Config(), "j1", &clarifyAskJSON{Revision: "r", Question: "q", IdempotencyKey: "clarify-0123456789ab"})
		if chatworkErrorCode(err) != code || strings.Contains(err.Error(), "private") {
			t.Errorf("status %d: %v", status, err)
		}
	}
	cfg := issue3790Config()
	cfg.Bearer = ""
	if _, _, err := journeyClarifyCall(context.Background(), issue3790Client(200, `{}`, nil), cfg, "j1", nil); chatworkErrorCode(err) != "unavailable" {
		t.Fatalf("no bearer: %v", err)
	}
}

// Every refusal the server can give has its own sentence; an unknown one reads
// as a failed send that keeps the text.
func TestIssue3995_EveryRefusalHasItsOwnWords(t *testing.T) {
	want := map[string]string{
		"invalid_argument": "err_invalid", "revision_changed": "err_stale", "case_closed": "err_closed",
		"no_recipient": "err_no_recipient", "permission_denied": "err_denied", "not_found": "err_denied", "unavailable": "err_generic", "weird": "err_generic",
	}
	for code, key := range want {
		if got := clarifyErrorKey(code); got != key {
			t.Errorf("%s: %s, want %s", code, got, key)
		}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if clarifyWordsFor(locale)(key) == "" {
				t.Errorf("%s: no %s", locale, key)
			}
		}
	}
}

func TestIssue3995_AttemptKeysAreUniqueAndAcceptedByTheServer(t *testing.T) {
	// The server's key rule.
	rule := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	a, b := clarifyAttemptKey(), clarifyAttemptKey()
	if a == b || !rule.MatchString(a) || !rule.MatchString(b) {
		t.Fatalf("keys %q %q", a, b)
	}
}

func TestIssue3995_RowsSayWhoAskedWhatStateAndLinkOnlyOpenableChatAddresses(t *testing.T) {
	words := clarifyWordsFor("en-US")
	rows := clarifyRows([]clarifyItemJSON{
		{Role: "asker", PeerName: "Riley", State: "current", Href: "/workspace/app/chat#channel=dm"},
		{Role: "recipient", PeerName: "Morgan", State: "outdated", Href: "/workspace/app/chat#channel=dm"},
		{Role: "recipient", State: "closed", Unavailable: true, Href: "/workspace/app/chat#channel=secret"},
		{Role: "asker", PeerName: "Evil", State: "current", Href: "https://evil.example/chat"},
		{Role: "asker", PeerName: "Evil2", State: "current", Href: "//evil.example"},
	}, words)
	if len(rows) != 5 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Title != "You asked Riley" || rows[0].Href != "/workspace/app/chat#channel=dm" || rows[0].Aria != "Open the conversation with Riley" || rows[0].State != words("state_current") {
		t.Fatalf("asker row %+v", rows[0])
	}
	if rows[1].Title != "Morgan asked you" || rows[1].State != words("state_outdated") {
		t.Fatalf("an edited proposal is not flagged: %+v", rows[1])
	}
	if rows[2].Title != "The approver asked you" || rows[2].Href != "" || rows[2].Unavailable == "" || rows[2].State != words("state_closed") {
		t.Fatalf("a conversation the person cannot open was linked: %+v", rows[2])
	}
	if rows[3].Href != "" || rows[4].Href != "" {
		t.Fatalf("an address outside chat was linked: %+v %+v", rows[3], rows[4])
	}
}

func TestIssue3995_RevisionIsShortenedAndFinalRefusalsStopRetries(t *testing.T) {
	if clarifyShortRevision(" rev-1 ") != "rev-1" || clarifyShortRevision("0123456789abcdef") != "0123456789ab…" {
		t.Fatalf("%q %q", clarifyShortRevision(" rev-1 "), clarifyShortRevision("0123456789abcdef"))
	}
	for code, final := range map[string]bool{"revision_changed": true, "case_closed": true, "no_recipient": true, "permission_denied": true, "unavailable": false, "invalid_argument": false} {
		if clarifyFinal(code) != final {
			t.Errorf("%s: final = %v", code, !final)
		}
	}
}
