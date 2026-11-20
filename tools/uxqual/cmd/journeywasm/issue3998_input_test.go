package main

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestIssue3998_ClientReadsPreviewsAndSendsOnTheInputRoute(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	client := issue3790Client(200, `{"can_ask":true,"case_open":true,"status":"awaiting_you","revision":"rev-1","case_name":"Avery","items":[{"id":"a","kind":"informal","state":"current","href":"/workspace/app/chat#channel=fin"}],"incomplete":false}`, func(r *http.Request, body []byte) {
		gotPath, gotMethod, gotBody = r.URL.EscapedPath(), r.Method, string(body)
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s", r.URL)
		}
	})
	view, _, err := journeyInputCall(context.Background(), client, issue3790Config(), "intent 1", nil)
	if err != nil || !view.CanAsk || view.Revision != "rev-1" || view.CaseName != "Avery" || len(view.Items) != 1 ||
		gotMethod != http.MethodGet || gotPath != "/api/chat/journeys/intent%201/input-request" || gotBody != "" {
		t.Fatalf("read: %+v %v %s %s %q", view, err, gotMethod, gotPath, gotBody)
	}
	preview := issue3790Client(200, `{"preview":true,"message":"Informal\n\nWhy?","audience":{"kind":"group","size":4},"revision":"rev-1"}`, func(r *http.Request, body []byte) { gotMethod, gotBody = r.Method, string(body) })
	_, asked, err := journeyInputCall(context.Background(), preview, issue3790Config(), "j1", &inputAskJSON{Revision: "rev-1", Question: "Why?", ConversationID: "fin", Label: "Informal", RevisionLabel: "Rev rev-1", Preview: true})
	if err != nil || !asked.Preview || asked.Audience.Size != 4 || asked.Item != nil || gotMethod != http.MethodPost ||
		gotBody != `{"revision":"rev-1","question":"Why?","conversation_id":"fin","label":"Informal","revision_label":"Rev rev-1","include_case_name":false,"preview":true}` {
		t.Fatalf("preview: %+v %v %s %q", asked, err, gotMethod, gotBody)
	}
	created := issue3790Client(201, `{"preview":false,"message":"m","audience":{"kind":"group"},"item":{"id":"a","kind":"informal","state":"current","href":"/workspace/app/chat#channel=fin"}}`, nil)
	_, asked, err = journeyInputCall(context.Background(), created, issue3790Config(), "j1", &inputAskJSON{Revision: "rev-1", Question: "Why?", ConversationID: "fin", Label: "L", RevisionLabel: "rev-1", IdempotencyKey: "input-0123456789ab"})
	if err != nil || asked.Item == nil || asked.Item.ID != "a" {
		t.Fatalf("send: %+v %v", asked, err)
	}
	if _, _, err := journeyInputCall(context.Background(), client, issue3790Config(), "a/b", nil); chatworkErrorCode(err) != "invalid_argument" {
		t.Fatalf("a journey id with a slash reached the network: %v", err)
	}
}

func TestIssue3998_ClientReducesRefusalsToTheirCodes_Security(t *testing.T) {
	for status, code := range map[int]string{403: "permission_denied", 404: "not_found", 409: "revision_changed", 422: "no_conversation", 503: "unavailable"} {
		client := issue3790Client(status, `{"code":"`+code+`","detail":"private question text"}`, nil)
		_, _, err := journeyInputCall(context.Background(), client, issue3790Config(), "j1", &inputAskJSON{Revision: "r", Question: "q"})
		if chatworkErrorCode(err) != code || strings.Contains(err.Error(), "private") {
			t.Errorf("status %d: %v", status, err)
		}
	}
	cfg := issue3790Config()
	cfg.Bearer = ""
	if _, _, err := journeyInputCall(context.Background(), issue3790Client(200, `{}`, nil), cfg, "j1", nil); chatworkErrorCode(err) != "unavailable" {
		t.Fatalf("no bearer: %v", err)
	}
}

func TestIssue3998_EveryRefusalHasItsOwnWordsInEveryLocale(t *testing.T) {
	want := map[string]string{
		"invalid_argument": "err_invalid", "revision_changed": "err_stale", "case_closed": "err_closed", "no_conversation": "err_no_conversation",
		"name_in_public": "err_name_public", "permission_denied": "err_denied", "not_found": "err_denied", "unavailable": "err_generic", "weird": "err_generic",
	}
	for code, key := range want {
		if got := inputErrorKey(code); got != key {
			t.Errorf("%s: %s, want %s", code, got, key)
		}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			if inputWordsFor(locale)(key) == "" {
				t.Errorf("%s: no %s", locale, key)
			}
		}
	}
	for code, final := range map[string]bool{"revision_changed": true, "case_closed": true, "permission_denied": true, "no_conversation": false, "name_in_public": false, "unavailable": false} {
		if inputFinal(code) != final {
			t.Errorf("%s: final = %v", code, !final)
		}
	}
}

func TestIssue3998_AttemptKeysAreUniqueAndAcceptedByTheServer(t *testing.T) {
	rule := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
	a, b := inputAttemptKey(), inputAttemptKey()
	if a == b || !rule.MatchString(a) || !rule.MatchString(b) {
		t.Fatalf("keys %q %q", a, b)
	}
}

func TestIssue3998_AudienceSentenceSaysWhoCanReadItAndThatTheCaseStaysClosed(t *testing.T) {
	words := inputWordsFor("en-US")
	cases := map[string]inputAudienceJSON{
		"only you and the other person": {Kind: "direct"},
		"everyone in this group":        {Kind: "group", Size: 6},
		"private channel finance":       {Kind: "private_channel", Name: "finance", Size: 5},
		"public channel hr":             {Kind: "public_channel", Name: "hr", Size: 120},
	}
	for want, audience := range cases {
		got := inputAudienceSentence(audience, words)
		if !strings.Contains(got, want) || !strings.Contains(got, "cannot open this case") {
			t.Errorf("%+v: %q", audience, got)
		}
		if audience.Size > 0 && !strings.Contains(got, " people.") {
			t.Errorf("the size is missing: %q", got)
		}
	}
	if got := inputAudienceSentence(inputAudienceJSON{Kind: "group"}, words); strings.Contains(got, "people.") {
		t.Errorf("an unknown size was invented: %q", got)
	}
	// Arabic and German read the same facts.
	if got := inputAudienceSentence(inputAudienceJSON{Kind: "public_channel", Name: "hr", Size: 3}, inputWordsFor("de-DE")); !strings.Contains(got, "öffentlichen Kanal hr") || !strings.Contains(got, "3 Personen") {
		t.Errorf("German: %q", got)
	}
}

func TestIssue3998_RowsSayWhereWhatStateAndNeverLinkAConversationThatIsNotCurrentlyOpenable(t *testing.T) {
	words := inputWordsFor("en-US")
	rows := inputRows([]inputItemJSON{
		{ConversationName: "finance", State: "current", Revision: "rev-1", Href: "/workspace/app/chat#channel=fin"},
		{AudienceKind: "direct", State: "outdated", Revision: "rev-1", Href: "/workspace/app/chat#channel=dm"},
		{State: "unavailable", Revision: "rev-1", Href: "/workspace/app/chat#channel=secret"},
		{ConversationName: "evil", State: "current", Href: "https://evil.example/chat"},
		{ConversationName: "evil2", State: "current", Href: "//evil.example"},
		{State: "closed", Revision: "rev-1"},
	}, words)
	if len(rows) != 6 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].Title != "You asked in finance" || rows[0].Href != "/workspace/app/chat#channel=fin" || rows[0].Aria != "Open the conversation finance" || rows[0].State != words("state_current") || rows[0].Revision != "About proposal revision rev-1" {
		t.Fatalf("current row %+v", rows[0])
	}
	if rows[1].Title != "You asked in a direct message" || rows[1].State != words("state_outdated") {
		t.Fatalf("a changed proposal is not flagged: %+v", rows[1])
	}
	if rows[2].Href != "" || rows[2].State != words("state_unavailable") || rows[2].Title != words("item_unknown") {
		t.Fatalf("a conversation the person cannot open was linked or called current: %+v", rows[2])
	}
	if rows[3].Href != "" || rows[4].Href != "" {
		t.Fatalf("an address outside chat was linked")
	}
	if rows[5].State != words("state_closed") {
		t.Fatalf("closed row %+v", rows[5])
	}
}
