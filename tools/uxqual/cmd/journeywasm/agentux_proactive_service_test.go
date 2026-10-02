package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestAgentUXProactive_BrowserService(t *testing.T) {
	var path, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, authorization = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(agentcontrols.AnnouncementReply{Snapshot: productui.AgentAnnouncementsSnapshot{Available: true}})
	}))
	defer server.Close()
	input := agentcontrols.AnnouncementDraft{InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Tell employees which company holidays are coming up.", Documents: []agentdocref.Reference{{DocumentID: "holiday-guide", VersionMode: agentdocref.ModeLatestPublished, Label: "2026 holiday guide"}}, Cadence: "WEEKLY", Time: "09:00", Zone: "America/New_York", Weekdays: []int{1}, IdempotencyKey: "request-123"}
	if err := validateAgentAnnouncementInput(input); err != nil {
		t.Fatal(err)
	}
	reply, err := agentAnnouncementRequest(context.Background(), server.Client(), journeyclient.Config{TunnelURL: server.URL, Bearer: "session"}, "create", input)
	if err != nil || !reply.Snapshot.Available || path != agentcontrols.AnnouncementPath || authorization != "Bearer session" {
		t.Fatalf("reply=%+v path=%q auth=%q err=%v", reply, path, authorization, err)
	}
	for _, action := range []string{"preview", "update", "control"} {
		if _, err := agentAnnouncementRequest(context.Background(), server.Client(), journeyclient.Config{TunnelURL: server.URL, Bearer: "session"}, action, input); err != nil || path != agentcontrols.AnnouncementPath+"/"+action {
			t.Fatalf("%s path=%q err=%v", action, path, err)
		}
	}
	if _, err := agentAnnouncementRequest(context.Background(), server.Client(), journeyclient.Config{}, "", nil); !errors.Is(err, errAgentAnnouncement) {
		t.Fatalf("missing authenticated endpoint error=%v", err)
	}
}

func TestAgentUXProactive_BrowserInputCalendar(t *testing.T) {
	base := agentcontrols.AnnouncementDraft{InstallationID: "install", PersonaID: "agent", ConversationID: "general", Instruction: "Post the holidays", Documents: []agentdocref.Reference{{DocumentID: "guide", VersionMode: agentdocref.ModeLatestPublished, Label: "Guide"}}, Cadence: "WEEKLY", Time: "09:00", Zone: "America/New_York", Weekdays: []int{1}, IdempotencyKey: "request-123"}
	for _, change := range []func(*agentcontrols.AnnouncementDraft){func(d *agentcontrols.AnnouncementDraft) { d.Weekdays = nil }, func(d *agentcontrols.AnnouncementDraft) { d.Weekdays = []int{1, 1} }, func(d *agentcontrols.AnnouncementDraft) { d.Weekdays = []int{7} }, func(d *agentcontrols.AnnouncementDraft) { d.Zone = "unknown/zone" }, func(d *agentcontrols.AnnouncementDraft) { d.Time = "25:00" }, func(d *agentcontrols.AnnouncementDraft) { d.Instruction = strings.Repeat("a", 1001) }, func(d *agentcontrols.AnnouncementDraft) { d.Documents = nil }} {
		input := base
		change(&input)
		if !errors.Is(validateAgentAnnouncementInput(input), errAgentAnnouncementInvalid) {
			t.Fatalf("invalid input accepted: %+v", input)
		}
	}
	for _, cadence := range []string{"NOW", "ONCE", "DAILY", "MONTHLY"} {
		input := base
		input.Cadence, input.Weekdays = cadence, nil
		if cadence == "MONTHLY" {
			input.MonthDay = 31
		}
		if err := validateAgentAnnouncementInput(input); err != nil {
			t.Fatalf("%s rejected: %v", cadence, err)
		}
	}
	if got := fmtAnnouncementTime(9, 5); got != "09:05" {
		t.Fatalf("viewer clock was not padded: %s", got)
	}
}
