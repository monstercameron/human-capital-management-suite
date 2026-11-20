package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestIssue3815_ClientReadsWhatTheReaderMayDoWithEachPost(t *testing.T) {
	var path, query string
	body := `{"can_require":true,"views":{
		"p-1":{"post_id":"p-1","required":true,"can_acknowledge":true},
		"p-2":{"post_id":"p-2","required":true,"acknowledged":true,"acknowledged_at":"2026-11-19T10:00:00Z"},
		"p-3":{"post_id":"p-3","required":true,"can_manage":true,"total":4,"ack_count":1,"pending":["u-2","u-3"],"pending_more":1}
	}}`
	views, canRequire, err := ackRead(context.Background(), issue3790Client(200, body, func(r *http.Request, _ []byte) { path, query = r.URL.EscapedPath(), r.URL.RawQuery }), issue3790Config(), "gen eral", []string{"p-1", "p-2", "p-3", "p-4"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/chat/conversations/gen%20eral/acks" {
		t.Fatalf("path %q", path)
	}
	if q, _ := url.ParseQuery(query); len(q["post"]) != 4 {
		t.Fatalf("query %q", query)
	}
	if !canRequire {
		t.Fatal("can_require was dropped")
	}
	if one := views["p-1"]; !one.Loaded || !one.CanAcknowledge || one.Acknowledged {
		t.Fatalf("p-1 %+v", one)
	}
	if two := views["p-2"]; !two.Acknowledged || two.CanAcknowledge || !two.AcknowledgedAt.Equal(time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("p-2 %+v", two)
	}
	if three := views["p-3"]; !three.CanManage || three.Total != 4 || three.Count != 1 || len(three.Pending) != 2 || three.PendingMore != 1 {
		t.Fatalf("p-3 %+v", three)
	}
	// A post the server did not describe requires nothing.
	if _, has := views["p-4"]; has || len(views) != 3 {
		t.Fatalf("views %+v", views)
	}
}

func TestIssue3815_ClientSendsOnePostAndReducesRefusalsToCodes_Security(t *testing.T) {
	var method, path, sent string
	view, err := ackAcknowledge(context.Background(), issue3790Client(200, `{"view":{"post_id":"p-1","required":true,"acknowledged":true,"acknowledged_at":"2026-11-19T10:00:00Z"}}`, func(r *http.Request, b []byte) {
		method, path, sent = r.Method, r.URL.EscapedPath(), string(b)
	}), issue3790Config(), "general", "p-1")
	if err != nil || !view.Acknowledged {
		t.Fatalf("view %+v err %v", view, err)
	}
	if method != http.MethodPost || path != "/api/chat/conversations/general/acks/p-1/acknowledge" || sent != "" {
		t.Fatalf("sent %s %s %q", method, path, sent)
	}
	if _, err = ackRequire(context.Background(), issue3790Client(200, `{"view":{"post_id":"p-1","required":true,"can_manage":true}}`, func(r *http.Request, _ []byte) { path = r.URL.EscapedPath() }), issue3790Config(), "general", "p-1"); err != nil || path != "/api/chat/conversations/general/acks/p-1/require" {
		t.Fatalf("require path %q err %v", path, err)
	}
	cases := map[string]struct {
		status int
		body   string
		code   string
	}{
		"denied":    {403, `{"code":"permission_denied"}`, "permission_denied"},
		"no body":   {404, ``, "not_found"},
		"down":      {503, `{"code":"unavailable"}`, "unavailable"},
		"garbled":   {200, `<html>`, "unavailable"},
		"no view":   {200, `{}`, "unavailable"},
		"html fail": {500, `<html>boom</html>`, "unavailable"},
	}
	for name, tc := range cases {
		_, err := ackAcknowledge(context.Background(), issue3790Client(tc.status, tc.body, nil), issue3790Config(), "general", "p-1")
		var refused *ackError
		if !errors.As(err, &refused) || refused.Code != tc.code {
			t.Errorf("%s: err %v, want code %s", name, err, tc.code)
		}
	}
}
