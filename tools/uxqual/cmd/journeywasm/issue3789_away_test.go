package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var issue3789Config = journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}

var issue3789Now = time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

func TestIssue3789_ClientReadsOnlyTheSignedInPersonsSetting(t *testing.T) {
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/chat/away" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.RawQuery != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"revision":3,"active":true,"until":"2026-03-12T00:00:00Z","cover_id":"priya","hidden":true,"notify_cover":true}`)), Header: http.Header{}}, nil
	})}
	view, err := chatAwayRead(context.Background(), client, issue3789Config, issue3789Now)
	if err != nil || !view.Loaded || !view.Active || view.Revision != 3 || view.CoverID != "priya" || !view.Hidden || !view.NotifyCover || !view.Until.Equal(time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	// An absence that has already ended reads as not away, but keeps the display choice.
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"revision":4,"active":true,"until":"2026-03-01T00:00:00Z","cover_id":"priya","hidden":true}`))}, nil
	})
	if view, err := chatAwayRead(context.Background(), client, issue3789Config, issue3789Now); err != nil || view.Active || view.CoverID != "" || !view.Hidden {
		t.Fatalf("ended: %+v %v", view, err)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"code":"permission_denied"}`))}, nil
	})
	if _, err := chatAwayRead(context.Background(), client, issue3789Config, issue3789Now); err == nil {
		t.Fatal("a refusal must not read as not away")
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	if _, err := chatAwayRead(context.Background(), client, issue3789Config, issue3789Now); err == nil {
		t.Fatal("an answer without a revision was accepted")
	}
	if _, err := chatAwayRead(context.Background(), client, journeyclient.Config{}, issue3789Now); err == nil {
		t.Fatal("no tunnel or bearer must be unavailable")
	}
}

func TestIssue3789_BodyCarriesTheRevisionAndDropsWhatDoesNotApply(t *testing.T) {
	until := time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC)
	body := chatAwayBody(chatui.AwayEdit{Until: until, CoverID: "priya", NotifyCover: true, Hidden: true}, 7)
	if body.ExpectedRevision != 7 || body.Until == nil || !body.Until.Equal(until) || body.CoverID != "priya" || !body.NotifyCover || !body.Hidden {
		t.Fatalf("body=%+v", body)
	}
	// Notices without a cover are not sent as on.
	if body := chatAwayBody(chatui.AwayEdit{Until: until, NotifyCover: true}, 1); body.NotifyCover {
		t.Fatalf("notices without a cover: %+v", body)
	}
	// Ending an absence sends no date, cover or notices, and keeps the display choice.
	raw, _ := json.Marshal(chatAwayBody(chatui.AwayEdit{CoverID: "priya", NotifyCover: true, Hidden: true}, 2))
	if strings.Contains(string(raw), "until") || strings.Contains(string(raw), "priya") || strings.Contains(string(raw), `"notify_cover":true`) || !strings.Contains(string(raw), `"hidden":true`) {
		t.Fatalf("ended body: %s", raw)
	}
}

func TestIssue3789_SaveOutcomes(t *testing.T) {
	var sent awayBodyJSON
	status := 200
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/away" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"revision":8,"active":true,"until":"2026-03-12T00:00:00Z","cover_id":"priya"}`))}, nil
	})}
	edit := chatui.AwayEdit{Until: time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC), CoverID: "priya"}
	view, outcome := chatAwayWrite(context.Background(), client, issue3789Config, edit, 7, issue3789Now)
	if outcome != "saved" || view.Revision != 8 || !view.Active || sent.ExpectedRevision != 7 || sent.CoverID != "priya" {
		t.Fatalf("saved: %q %+v sent=%+v", outcome, view, sent)
	}
	for code, want := range map[int]string{409: "conflict", 400: "invalid", 500: "failed", 403: "failed"} {
		status = code
		if _, outcome := chatAwayWrite(context.Background(), client, issue3789Config, edit, 7, issue3789Now); outcome != want {
			t.Fatalf("%d: %q, want %q", code, outcome, want)
		}
	}
	if _, outcome := chatAwayWrite(context.Background(), client, journeyclient.Config{}, edit, 7, issue3789Now); outcome != "failed" {
		t.Fatalf("no tunnel: %q", outcome)
	}
}

func TestIssue3789_LookupNamesPeopleInBatchesAndKeepsOnlyRunningAbsences(t *testing.T) {
	var batches [][]string
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/away/lookup" || r.URL.RawQuery != "" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		var body struct {
			Members []string `json:"members"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		batches = append(batches, body.Members)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"people":[{"member_id":"u-1","until":"2026-03-12T00:00:00Z","cover_id":"priya"},{"member_id":"u-old","until":"2026-03-01T00:00:00Z"}]}`))}, nil
	})}
	ids := make([]string, awayLookupMax+3)
	for i := range ids {
		ids[i] = "u-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	people, err := chatAwayLookup(context.Background(), client, issue3789Config, ids, issue3789Now)
	if err != nil || len(batches) != 2 || len(batches[0]) != awayLookupMax || len(batches[1]) != 3 {
		t.Fatalf("batches=%v err=%v", len(batches), err)
	}
	if p, ok := people["u-1"]; !ok || p.CoverID != "priya" || len(people) != 1 {
		t.Fatalf("people=%+v", people)
	}
	client.Transport = chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"code":"unavailable"}`))}, nil
	})
	if _, err := chatAwayLookup(context.Background(), client, issue3789Config, ids[:2], issue3789Now); err == nil {
		t.Fatal("a failed lookup must not read as nobody being away")
	}
}

func TestIssue3789_LookupIdsAreThePeopleInViewOnce(t *testing.T) {
	model := chatui.Model{CurrentUser: "me",
		Members:       []chatui.Member{{ID: "me"}, {ID: "dana"}, {ID: "bot", Agent: true}, {ID: "dana"}, {ID: "priya"}},
		PersonDetails: &chatui.PersonDetails{ID: "lee"}}
	got := chatAwayIDs(model, "extra", "dana")
	want := []string{"extra", "dana", "lee", "priya"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids=%v want %v", got, want)
	}
}
