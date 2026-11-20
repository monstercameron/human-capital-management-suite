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

func issue3790Config() journeyclient.Config {
	return journeyclient.Config{TunnelURL: "wss://fixture.invalid/tunnel", Bearer: "deterministic-fixture", Tenant: "tenant", Subject: "person", Locale: "en-US"}
}

func issue3790Client(status int, body string, seen func(*http.Request, []byte)) *http.Client {
	return &http.Client{Transport: chatsaveRoundTrip(func(r *http.Request) (*http.Response, error) {
		var payload []byte
		if r.Body != nil {
			payload, _ = io.ReadAll(r.Body)
		}
		if seen != nil {
			seen(r, payload)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
}

// Issue 3790: the form sends the reviewed values to the message's own route,
// with the attempt's key, and never puts the message text on the wire.
func TestIssue3790_ClientSendsTheReviewedRequestOnce_Integration(t *testing.T) {
	var path, method string
	var sent chatworkConvert
	client := issue3790Client(200, `{"link":{"post_id":"m1","project_id":"ops","task_id":"t1","title":"Swap laptop","status":"To do"}}`, func(r *http.Request, body []byte) {
		path, method = r.URL.Path, r.Method
		if r.Header.Get("Authorization") != "Bearer deterministic-fixture" || r.URL.Scheme != "https" {
			t.Errorf("endpoint or authorization: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Error(err)
		}
	})
	key := chatworkNewKey()
	link, err := chatworkConvertMessage(context.Background(), client, issue3790Config(), "m1", chatworkConvert{ConversationID: "general", Target: "TASK", ProjectID: "ops", Title: "Swap laptop", OwnerID: "person", DueDate: "2026-12-04", IdempotencyKey: key})
	if err != nil || link.TaskID != "t1" || path != "/api/chat/messages/m1/convert" || method != http.MethodPost {
		t.Fatalf("link=%+v err=%v path=%s method=%s", link, err, path, method)
	}
	if sent.IdempotencyKey != key || sent.Target != "TASK" || sent.ProjectID != "ops" || sent.DueDate != "2026-12-04" || !strings.HasPrefix(key, "chatwork-") || len(key) < 20 {
		t.Fatalf("sent=%+v", sent)
	}
	if chatworkNewKey() == key {
		t.Fatal("two attempts share a key")
	}
	if _, err := chatworkConvertMessage(context.Background(), client, issue3790Config(), "a/b", chatworkConvert{}); chatworkErrorCode(err) != "invalid_argument" {
		t.Fatalf("a message id with a slash reached the network: %v", err)
	}
}

func TestIssue3790_ClientReducesRefusalsToTheirCodes_Security(t *testing.T) {
	for status, code := range map[int]string{403: "permission_denied", 404: "not_found", 400: "invalid_argument", 503: "unavailable"} {
		client := issue3790Client(status, `{"code":"`+code+`","detail":"secret project name"}`, nil)
		_, err := chatworkConvertMessage(context.Background(), client, issue3790Config(), "m1", chatworkConvert{ConversationID: "c"})
		if chatworkErrorCode(err) != code || strings.Contains(err.Error(), "secret") {
			t.Errorf("status %d: err=%v", status, err)
		}
	}
	cfg := issue3790Config()
	cfg.Bearer = ""
	if _, err := chatworkProjects(context.Background(), http.DefaultClient, cfg); chatworkErrorCode(err) != "unavailable" {
		t.Fatalf("no credential err=%v", err)
	}
	if _, err := chatworkProjects(context.Background(), issue3790Client(200, `not json`, nil), issue3790Config()); chatworkErrorCode(err) != "unavailable" {
		t.Fatalf("garbled answer err=%v", err)
	}
}

func TestIssue3790_ClientReadsProjectsAndLinkedWork(t *testing.T) {
	projects, err := chatworkProjects(context.Background(), issue3790Client(200, `{"projects":[{"id":"ops","name":"IT operations"},{"id":"","name":"x"},{"id":"hr","name":"  "}]}`, nil), issue3790Config())
	if err != nil || len(projects) != 1 || projects[0].ID != "ops" {
		t.Fatalf("projects=%+v err=%v", projects, err)
	}
	var query string
	links, err := chatworkLinked(context.Background(), issue3790Client(200, `{"links":[{"post_id":"m1","task_id":"t1","project_id":"ops","title":"A","status":"To do","owner_id":"person"},{"post_id":"m1","task_id":"t2","project_id":"ops","title":"B"},{"post_id":"","task_id":"t3"}]}`, func(r *http.Request, _ []byte) { query = r.URL.RawQuery }), issue3790Config(), "general")
	if err != nil || query != "conversation_id=general" {
		t.Fatalf("links=%+v err=%v query=%q", links, err, query)
	}
	grouped := chatworkByPost(links)
	if len(grouped) != 1 || len(grouped["m1"]) != 2 || grouped["m1"][0].Status != "To do" {
		t.Fatalf("grouped=%+v", grouped)
	}
}

func TestIssue3790_TitleAndOwnersAreOnlyWhatThePersonMayChoose(t *testing.T) {
	if got := chatworkTitle("\n  can we get Dana's laptop swapped before Friday  \nsecond line"); got != "can we get Dana's laptop swapped before Friday" {
		t.Fatalf("title=%q", got)
	}
	if got := chatworkTitle(strings.Repeat("ä", 300)); len([]rune(got)) != 200 {
		t.Fatalf("title not cut to 200 characters: %d", len([]rune(got)))
	}
	model := chatui.Model{CurrentUser: "me"}
	owners := chatworkOwners(model, chatui.Message{AuthorID: "dana", Author: "Dana"}, "Me", "wrote the message")
	if len(owners) != 2 || owners[0].ID != "me" || owners[1].ID != "dana" || owners[1].Label != "Dana (wrote the message)" {
		t.Fatalf("owners=%+v", owners)
	}
	if own := chatworkOwners(model, chatui.Message{AuthorID: "me", Author: "Me"}, "Me", "wrote the message"); len(own) != 1 {
		t.Fatalf("own message offers %d owners", len(own))
	}
}
