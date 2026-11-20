package main

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestIssue3811_ClientReadsTheOwnersChecklistForEachCard(t *testing.T) {
	var path, query, auth string
	body := `{"views":{
		"card-1":{"post_id":"card-1","state":"active","start_date":"2026-11-17","items":[{"kind":"ACCESS","owner":"IT desk","status":"pending","required":true},{"kind":"IDENTITY","status":"confirmed","confirmed_on":"2026-11-10"}]},
		"card-9":{"post_id":"card-9","state":"active"}
	}}`
	views, err := onboardingRead(context.Background(), issue3790Client(200, body, func(r *http.Request, _ []byte) {
		path, query, auth = r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Authorization")
	}), issue3790Config(), "gen eral", []string{"card-1", "card-2"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/chat/conversations/gen%20eral/onboarding" || auth == "" {
		t.Fatalf("path %q auth %q", path, auth)
	}
	if q, _ := url.ParseQuery(query); len(q["post"]) != 2 {
		t.Fatalf("query %q", query)
	}
	one := views["card-1"]
	if !one.Loaded || one.State != "active" || one.StartDate != "2026-11-17" || len(one.Items) != 2 || one.Items[0].Owner != "IT desk" || !one.Items[0].Required || one.Items[1].ConfirmedOn != "2026-11-10" {
		t.Fatalf("card-1 %+v", one)
	}
	// A card the server did not describe is loaded with nothing offered; a card
	// nobody asked about is dropped.
	if two := views["card-2"]; !two.Loaded || two.State != "" || len(two.Items) != 0 {
		t.Fatalf("card-2 %+v", two)
	}
	if _, extra := views["card-9"]; extra || len(views) != 2 {
		t.Fatalf("views %+v", views)
	}
}

func TestIssue3811_ClientTreatsEveryFailureAsUnreadable_Security(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"denied":  {403, `{"code":"permission_denied"}`},
		"down":    {503, `{"code":"unavailable"}`},
		"garbled": {200, `<html>`},
		"html":    {500, `<html>boom</html>`},
	} {
		views, err := onboardingRead(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "general", []string{"card-1"})
		if err == nil || views != nil {
			t.Errorf("%s: views %+v err %v, want an error and no views", name, views, err)
		}
	}
}
