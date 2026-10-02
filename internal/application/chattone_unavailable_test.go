package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATTONE_004_NotQualifiedSaysWhy: a deployment whose writing-style
// model has not passed the qualification run does not silently have no controls.
// The features answer says why, in a code the page turns into a sentence, and
// the service itself offers nothing.
func TestTodo_CHATTONE_004_NotQualifiedSaysWhy(t *testing.T) {
	admission, bearer := integrate1Admission(t, "tenant-a", "alice", time.Now)
	assembly := &agentServedAssembly{WritingStyles: ChattoneNotProvisioned("no model is qualified for chat writing styles (set HCMNEXT_CHAT_WRITING_STYLE_MODEL_CONFIG_FILE)")}
	handler := assembly.Overlay(nil, admission)
	request := httptest.NewRequest(http.MethodGet, integrate2FeaturesPath, nil)
	request.Header.Set("Authorization", bearer)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var features chatui.ChatFeatures
	if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &features) != nil {
		t.Fatalf("features: %d %s", recorder.Code, recorder.Body)
	}
	if features.WritingStyles || features.WritingStylesNote != ChattoneNoteNotQualified {
		t.Fatalf("features = %+v, want writing styles off with the note %q", features, ChattoneNoteNotQualified)
	}
	// The member is told the reason, never the deployment's detail.
	if strings.Contains(recorder.Body.String(), "HCMNEXT_") || strings.Contains(recorder.Body.String(), "config") {
		t.Fatalf("the deployment's detail reached a member: %s", recorder.Body)
	}

	service := ChattoneNotProvisioned("why")
	if _, err := service.ReadSuggestion(context.Background(), "room"); err == nil {
		t.Fatal("an unprovisioned service answered a suggestion read")
	}
	fixture, ctx, _, _, _, _ := chattoneFixture(t)
	service.Now = fixture.Now
	if service.WritingStylesEnabled(ctx) {
		t.Fatal("an unprovisioned service reports itself enabled")
	}
	if _, err := service.RewriteDraft(ctx, ChattoneDraft{ConversationID: "room", Draft: "Please send the report today", StyleID: "professional"}); !errors.Is(err, chatrewrite.ErrUnavailable) {
		t.Fatalf("an unprovisioned service rewrote a draft: %v", err)
	}
}

// TestTodo_CHATTONE_004_NoteReasons: the reason is "" when the controls are on
// and otherwise names what the writer can understand: an administrator turned
// them off, or no qualified model is bound.
func TestTodo_CHATTONE_004_NoteReasons(t *testing.T) {
	service, ctx, _, _, _, _ := chattoneFixture(t)
	if note := service.WritingStylesNote(ctx); note != "" || !service.WritingStylesEnabled(ctx) {
		t.Fatalf("a workspace with the controls on: note %q", note)
	}
	if err := service.Rewrite.Registry.Configure("tenant", false, chatrewrite.DefaultStyles()); err != nil {
		t.Fatal(err)
	}
	if note := service.WritingStylesNote(ctx); note != ChattoneNoteWorkspaceOff {
		t.Fatalf("an administrator's off: note %q", note)
	}
	// A model that is not bound reads as "not qualified", whatever the setting.
	if err := service.Rewrite.Registry.Configure("tenant", true, chatrewrite.DefaultStyles()); err != nil {
		t.Fatal(err)
	}
	service.Rewrite.Model = ChattoneGatewayModel{}
	if note := service.WritingStylesNote(ctx); note != ChattoneNoteNotQualified {
		t.Fatalf("an unbound model: note %q", note)
	}
	// Without a signed-in person there is nothing to say about a workspace.
	if note := service.WritingStylesNote(context.Background()); note == "" {
		t.Fatal("an anonymous caller was told the controls are on")
	}
	var absent *ChattoneService
	if note := absent.WritingStylesNote(ctx); note != ChattoneNoteNotQualified {
		t.Fatalf("no service: note %q", note)
	}
	if chattoneFeatureNote(ctx, nil) != "" || chattoneFeatureNote(ctx, absent) != "" {
		t.Fatal("the features note for a missing surface changed")
	}
}
