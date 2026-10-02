package main

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentUXProactiveLive_RefusalReason_Client(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable","reason":"The corrected reply contains source lines. Preview again."}`))
	}))
	defer server.Close()
	_, err := agentAnnouncementRequest(context.Background(), server.Client(), journeyclient.Config{TunnelURL: server.URL, Bearer: "fixture"}, "preview", map[string]string{})
	var refusal agentAnnouncementReasonError
	if !errors.As(err, &refusal) || !errors.Is(err, errAgentAnnouncement) || refusal.Reason != "The corrected reply contains source lines. Preview again." {
		t.Fatalf("plain reason lost: %v", err)
	}
}

func TestAgentUXProactiveLive_ExistingPreview_Client(t *testing.T) {
	draft := agentcontrols.AnnouncementDraft{ID: "existing-definition", ExpectedRevision: 3, InstallationID: "install", PersonaID: "assistant", ConversationID: "general", Instruction: "Upcoming holidays", Zone: "America/New_York"}
	row := productui.AgentAnnouncementRow{ID: draft.ID, Revision: 3, Editor: productui.AgentAnnouncementEditorValue{InstallationID: draft.InstallationID, PersonaID: draft.PersonaID, ConversationID: draft.ConversationID, Instruction: draft.Instruction, Zone: draft.Zone}}
	if !agentAnnouncementPreviewMatchesSaved(draft, []productui.AgentAnnouncementRow{row}) {
		t.Fatal("saved definition cannot be previewed")
	}
	draft.Instruction = "Changed instruction"
	if agentAnnouncementPreviewMatchesSaved(draft, []productui.AgentAnnouncementRow{row}) {
		t.Fatal("unsaved changes accepted for posting")
	}
	draft.Instruction = row.Editor.Instruction
	draft.ExpectedRevision++
	if agentAnnouncementPreviewMatchesSaved(draft, []productui.AgentAnnouncementRow{row}) {
		t.Fatal("stale revision accepted")
	}
}
