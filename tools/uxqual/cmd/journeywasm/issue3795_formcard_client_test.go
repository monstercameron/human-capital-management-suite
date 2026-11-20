package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestIssue3795_ClientReadsWhatTheReaderMayDoWithEachCard(t *testing.T) {
	var path, query string
	body := `{"views":{
		"card-1":{"post_id":"card-1","can_submit":true},
		"card-2":{"post_id":"card-2","submitted":true,"submitted_at":"2026-11-19T10:00:00Z","mine":{"reason":"Promotion"}},
		"card-9":{"post_id":"card-9","can_submit":true}
	}}`
	views, err := formcardRead(context.Background(), issue3790Client(200, body, func(r *http.Request, _ []byte) { path, query = r.URL.EscapedPath(), r.URL.RawQuery }), issue3790Config(), "gen eral", []string{"card-1", "card-2", "card-3"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/chat/conversations/gen%20eral/forms" {
		t.Fatalf("path %q", path)
	}
	if q, _ := url.ParseQuery(query); len(q["post"]) != 3 {
		t.Fatalf("query %q", query)
	}
	if !views["card-1"].Loaded || !views["card-1"].CanSubmit || views["card-1"].Submitted {
		t.Fatalf("card-1 %+v", views["card-1"])
	}
	two := views["card-2"]
	if !two.Submitted || two.CanSubmit || two.Mine["reason"] != "Promotion" || !two.SubmittedAt.Equal(time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("card-2 %+v", two)
	}
	// A card the server did not describe is loaded with nothing offered, and a
	// card nobody asked about is dropped.
	if three := views["card-3"]; !three.Loaded || three.CanSubmit || three.Submitted {
		t.Fatalf("card-3 %+v", three)
	}
	if _, extra := views["card-9"]; extra || len(views) != 3 {
		t.Fatalf("views %+v", views)
	}
}

func TestIssue3795_ClientSendsTheValuesAndReducesRefusalsToCodes_Security(t *testing.T) {
	var sent string
	var method, path string
	view, err := formcardSubmit(context.Background(), issue3790Client(200, `{"view":{"post_id":"card-1","submitted":true,"submitted_at":"2026-11-19T10:00:00Z","mine":{"code":"merit"}}}`, func(r *http.Request, b []byte) {
		sent, method, path = string(b), r.Method, r.URL.EscapedPath()
	}), issue3790Config(), "general", "card-1", map[string]string{"code": "merit"})
	if err != nil || !view.Submitted || view.Mine["code"] != "merit" {
		t.Fatalf("view %+v err %v", view, err)
	}
	if method != http.MethodPost || path != "/api/chat/conversations/general/forms/card-1/answers" || sent != `{"values":{"code":"merit"}}` {
		t.Fatalf("sent %s %s %s", method, path, sent)
	}
	cases := map[string]struct {
		status int
		body   string
		code   string
	}{
		"invalid":   {400, `{"code":"invalid_form","fields":{"effective":"bad_date"}}`, "invalid_form"},
		"answered":  {409, `{"code":"already_answered"}`, "already_answered"},
		"denied":    {403, `{"code":"permission_denied"}`, "permission_denied"},
		"no body":   {404, ``, "not_found"},
		"down":      {503, `{"code":"unavailable"}`, "unavailable"},
		"garbled":   {200, `<html>`, "unavailable"},
		"no view":   {200, `{}`, "unavailable"},
		"html fail": {500, `<html>boom</html>`, "unavailable"},
	}
	for name, tc := range cases {
		_, err := formcardSubmit(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "general", "card-1", map[string]string{})
		var refused *formcardError
		if !errors.As(err, &refused) || refused.Code != tc.code {
			t.Errorf("%s: err %v, want code %s", name, err, tc.code)
		}
	}
	_, err = formcardSubmit(context.Background(), issue3790Client(400, cases["invalid"].body, nil), issue3790Config(), "general", "card-1", map[string]string{})
	var refused *formcardError
	if !errors.As(err, &refused) || refused.Fields["effective"] != "bad_date" {
		t.Errorf("fields lost: %v", err)
	}
}
