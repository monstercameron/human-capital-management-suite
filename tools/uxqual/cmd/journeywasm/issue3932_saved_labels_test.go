package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestIssue3932_ParseLabels(t *testing.T) {
	got, ok := chatsaveParseLabels(" Open enrollment, hiring ,, Hiring ,Shift  handover")
	if !ok || !reflect.DeepEqual(got, []string{"Open enrollment", "hiring", "Shift handover"}) {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := chatsaveParseLabels(strings.Repeat("x", 41)); ok {
		t.Fatal("a label longer than 40 characters was accepted")
	}
	if got, ok := chatsaveParseLabels(" , "); !ok || len(got) != 0 {
		t.Fatalf("empty input: %v %v", got, ok)
	}
}

func issue3932Page() chat.SavedPage {
	return chat.SavedPage{Items: []chat.SavedItem{
		{TenantID: "t", ConversationID: "c", PostID: "p1", State: chat.SavedTodo, Labels: []string{"Hiring", "Benefits"}},
		{TenantID: "t", ConversationID: "c", PostID: "p2", State: chat.SavedDone, Labels: []string{"hiring"}},
		{TenantID: "t", ConversationID: "c", PostID: "p3", State: chat.SavedTodo},
	}}
}

func TestIssue3932_LabelViews(t *testing.T) {
	page := issue3932Page()
	var in []string
	for _, item := range page.Items {
		if chatsaveInLabelView(item, "HIRING", false) {
			in = append(in, item.PostID)
		}
	}
	if !reflect.DeepEqual(in, []string{"p1", "p2"}) {
		t.Fatalf("label view = %v", in)
	}
	in = nil
	for _, item := range page.Items {
		if chatsaveInLabelView(item, "", true) {
			in = append(in, item.PostID)
		}
	}
	if !reflect.DeepEqual(in, []string{"p3"}) {
		t.Fatalf("unlabelled view = %v", in)
	}
	view := chatsaveLabelsView(page, "hiring", false, "", "")
	if len(view.Options) != 2 || view.Options[1].Name != "Hiring" || view.Options[1].Count != 2 || view.UnlabelledCount != 1 {
		t.Fatalf("view = %+v", view)
	}
	// Completing a message keeps its labels (and the chip counts).
	done := chatsaveApplyLocal(page, "t", chatsaveCommand{Action: "done", ConversationID: "c", PostID: "p1"}, nil)
	if !reflect.DeepEqual(done.Items[0].Labels, []string{"Hiring", "Benefits"}) || done.Items[0].State != chat.SavedDone {
		t.Fatalf("done = %+v", done.Items[0])
	}
}

func TestIssue3932_ApplyLocalMatchesService(t *testing.T) {
	page := issue3932Page()
	page = chatsaveApplyLocal(page, "t", chatsaveCommand{Action: "labels", ConversationID: "c", PostID: "p3", AddLabels: []string{"Handover", "handover"}}, nil)
	if !reflect.DeepEqual(page.Items[2].Labels, []string{"Handover"}) {
		t.Fatalf("add = %v", page.Items[2].Labels)
	}
	page = chatsaveApplyLocal(page, "t", chatsaveCommand{Action: "rename_label", Label: "HIRING", NewLabel: "Recruiting"}, nil)
	if !reflect.DeepEqual(page.Items[0].Labels, []string{"Recruiting", "Benefits"}) || !reflect.DeepEqual(page.Items[1].Labels, []string{"Recruiting"}) {
		t.Fatalf("rename = %v %v", page.Items[0].Labels, page.Items[1].Labels)
	}
	page = chatsaveApplyLocal(page, "t", chatsaveCommand{Action: "remove_label", Label: "recruiting"}, nil)
	if len(page.Items) != 3 || !reflect.DeepEqual(page.Items[0].Labels, []string{"Benefits"}) || len(page.Items[1].Labels) != 0 {
		t.Fatalf("remove_label must never remove an item: %+v", page.Items)
	}
}

func TestIssue3932_RestoreBringsLabelsBack(t *testing.T) {
	commands := chatsaveRestore(chat.SavedItem{ConversationID: "c", PostID: "p", Labels: []string{"Hiring"}})
	if len(commands) != 2 || commands[0].Action != "save" || commands[1].Action != "labels" || !reflect.DeepEqual(commands[1].AddLabels, []string{"Hiring"}) {
		t.Fatalf("commands = %+v", commands)
	}
}

func TestIssue3932_RowsCarryLabelsAndRequestShape(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "t", Subject: "u"}
	page := issue3932Page()
	for i := range page.Items {
		page.Items[i].HomeTenantID, page.Items[i].PersonID = "t", "u"
	}
	rows := chatsaveRows(page, cfg, chatui.Model{})
	if len(rows) != 3 || !reflect.DeepEqual(rows[0].Labels, []string{"Hiring", "Benefits"}) {
		t.Fatalf("rows = %+v", rows)
	}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = map[string]any{"method": r.Method}
		_, _ = w.Write([]byte(`{"changed":2}`))
	}))
	defer server.Close()
	cfg.TunnelURL, cfg.Bearer = server.URL, "token"
	if _, err := chatsaveRequest(context.Background(), server.Client(), cfg, "t", "all", "", "", &chatsaveCommand{Action: "rename_label", Label: "a", NewLabel: "b"}); err != nil || got["method"] != http.MethodPost {
		t.Fatalf("a label-wide command needs no message: %v", err)
	}
	for _, bad := range []chatsaveCommand{
		{Action: "rename_label", Label: "a"},
		{Action: "remove_label"},
		{Action: "labels", ConversationID: "c", PostID: "p"},
		{Action: "rename_label", Label: "a", NewLabel: "b", ConversationID: "c", PostID: "p"},
	} {
		if _, err := chatsaveRequest(context.Background(), server.Client(), cfg, "t", "all", "", "", &bad); chatsaveErrorCode(err) != "invalid_argument" {
			t.Errorf("%+v was sent: %v", bad, err)
		}
	}
}
