package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func issue3785Config() journeyclient.Config {
	return journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
}

func TestIssue3785_ClientAsksForTheBriefAsTheSignedInPerson(t *testing.T) {
	var sent map[string]any
	client := &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat/catch-up/v1" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatal(err)
		}
		body := `{"host_tenant_id":"tenant","conversation_id":"room","thread_root_id":"","cursor":4,"after":4,"through":9,"covered_from":5,"total":5,"covered":5,"omitted":0,"truncated":false,"generated_at":"2026-03-10T08:05:00Z","method":"extractive",
 "sections":[{"kind":"asked_you","more":1,"lines":[{"post_id":"m1","sequence":6,"author_id":"dana","text":"can you look"},{"post_id":"","sequence":7,"author_id":"x","text":"no post"}]},{"kind":"unknown","more":0,"lines":[{"post_id":"m2","sequence":8,"author_id":"x","text":"ignored kind"}]}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	reply, err := chatCatchUpRequest(context.Background(), client, issue3785Config(), "room", "", "m0")
	if err != nil {
		t.Fatal(err)
	}
	if sent["conversation_id"] != "room" || sent["since_post_id"] != "m0" || sent["host"] != "tenant" || len(sent) != 3 {
		t.Fatalf("sent %v: the person is never named in the request", sent)
	}
	brief := chatCatchUpView(reply)
	if brief.Phase != chatui.CatchUpReady || brief.ConversationID != "room" || brief.Through != 9 || brief.Total != 5 || brief.Covered != 5 || brief.CoveredFrom != 5 {
		t.Fatalf("brief = %+v", brief)
	}
	if len(brief.Sections) != 1 || brief.Sections[0].Kind != "asked_you" || brief.Sections[0].More != 1 || len(brief.Sections[0].Lines) != 1 || brief.Sections[0].Lines[0].PostID != "m1" {
		t.Fatalf("a line with no post, and a kind the page does not know, are dropped: %+v", brief.Sections)
	}
}

func TestIssue3785_ClientFailuresNeverLookLikeAnEmptyBrief(t *testing.T) {
	cases := map[int]string{403: "denied", 404: "gone", 503: "unavailable", 500: "unavailable", 401: "unavailable"}
	for status, want := range cases {
		client := &http.Client{Transport: chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"code":"x"}`)), Header: http.Header{}}, nil
		})}
		_, err := chatCatchUpRequest(context.Background(), client, issue3785Config(), "room", "", "")
		if err == nil || chatCatchUpFailure(err) != want {
			t.Fatalf("status %d: err=%v failure=%q, want %q", status, err, chatCatchUpFailure(err), want)
		}
	}
	if _, err := chatCatchUpRequest(context.Background(), http.DefaultClient, journeyclient.Config{}, "room", "", ""); err == nil || chatCatchUpFailure(err) != "unavailable" {
		t.Fatalf("no tunnel or bearer: %v", err)
	}
	if _, err := chatCatchUpRequest(context.Background(), http.DefaultClient, issue3785Config(), "", "", ""); err == nil {
		t.Fatal("a brief needs a conversation")
	}
	bad := &http.Client{Transport: chatsaveRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`not json`)), Header: http.Header{}}, nil
	})}
	if _, err := chatCatchUpRequest(context.Background(), bad, issue3785Config(), "room", "", ""); err == nil {
		t.Fatal("an unreadable answer must fail, not read as nothing missed")
	}
}

func TestIssue3785_FailedBriefDropsEarlierContent(t *testing.T) {
	failed := chatCatchUpFailed("room", "root", "denied")
	if failed.Phase != chatui.CatchUpFailed || failed.Error != "denied" || failed.ConversationID != "room" || failed.ThreadRootID != "root" || len(failed.Sections) != 0 || failed.Total != 0 {
		t.Fatalf("failed = %+v", failed)
	}
}
