package main

import (
	"context"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"strings"
	"testing"
)

func TestAgentUXR7_K29_AccessCheckResultSurvivesSelection(t *testing.T) {
	original := productui.PersonaAdminSnapshot{Available: true, Personas: []productui.PersonaAdminPersona{{ID: "policy", Name: "Policy Helper", Lifecycle: productui.PersonaPublished}}, SubjectOptions: []productui.PersonaAdminTarget{{ID: "walt", Label: "Walt Brennan"}}, Conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "general", Kind: "CHANNEL"}}, Preview: productui.PersonaAdminPreview{Subject: "walt", Conversation: "general", OfficialDocumentTitles: []string{"Leave policy"}, EffectiveSkills: []productui.PersonaAdminSkill{{ID: "knowledge_search_with_citations"}}}}
	got := personaAdminApplyPreviewSelection(original, "policy", "walt", "general")
	if got.PreviewPersonaID != "policy" || got.PreviewSubjectID != "walt" || got.PreviewConversationID != "general" || len(got.PreviewValidationFields) != 0 || len(got.Preview.OfficialDocumentTitles) != 1 {
		t.Fatalf("selected result lost: %+v", got)
	}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		view := productui.ApplyLocale(productui.NewView(productui.PagePersonaAdmin, "tenant", "walt", ""), productui.ResolveProductLocale(language))
		view.EffectivePermissions = []productui.RolePagePermission{{Page: productui.PagePersonaAdmin, View: true}}
		view.PersonaAdminClient = personaAdminBrowserFixture{got}
		markup, err := ui.RenderToString(productui.BuildPersonaAdminPage(view, view.PersonaAdminClient))
		if err != nil || !strings.Contains(markup, `data-persona-preview-result="true"`) || !strings.Contains(markup, "Leave policy") {
			t.Fatalf("%s result not rendered: %v", language, err)
		}
	}
	invalid := personaAdminApplyPreviewSelection(got, "policy", "walt", "")
	if len(invalid.PreviewValidationFields) != 1 || invalid.PreviewValidationFields[0] != "conversation" || invalid.Preview.Conversation != "" {
		t.Fatalf("stale result retained: %+v", invalid)
	}
}

type personaAdminBrowserFixture struct {
	snapshot productui.PersonaAdminSnapshot
}

func (c personaAdminBrowserFixture) Snapshot(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return c.snapshot, nil
}
func (c personaAdminBrowserFixture) Preview(context.Context, productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return c.snapshot.Preview, nil
}
func (personaAdminBrowserFixture) RequestReview(string) error   { return nil }
func (personaAdminBrowserFixture) PublishPersona(string) error  { return nil }
func (personaAdminBrowserFixture) RollbackPersona(string) error { return nil }
func (personaAdminBrowserFixture) SuspendPersona(string) error  { return nil }
func (personaAdminBrowserFixture) RetirePersona(string) error   { return nil }

func TestAgentUXR7_K20_ReviewDecisionOutcome(t *testing.T) {
	if got := personaAdminReviewOutcomeAction("REVIEW", "REJECT"); got != "REJECT" {
		t.Fatalf("rejection shown as approval: %s", got)
	}
	if got := personaAdminReviewOutcomeAction("REVIEW", "APPROVE"); got != "REVIEW" {
		t.Fatalf("approval outcome: %s", got)
	}
	if got := personaAdminReviewOutcomeAction("PUBLISH", ""); got != "PUBLISH" {
		t.Fatalf("publication outcome: %s", got)
	}
}

func TestAgentUXR7_L20_HistoryLoadsAfterOwnerNavigation(t *testing.T) {
	if personaAdminShouldLoadHistory(false, false) {
		t.Fatal("history requested without an owner projection")
	}
	if !personaAdminShouldLoadHistory(true, false) {
		t.Fatal("late owner projection did not load run history")
	}
	if personaAdminShouldLoadHistory(true, true) {
		t.Fatal("render loop repeatedly requested history")
	}
}

func TestAgentUXR7_L20_HistorySurvivesAccessAndCatalogRefresh(t *testing.T) {
	previous := productui.PersonaAdminSnapshot{Personas: []productui.PersonaAdminPersona{{ID: "policy", RecentRuns: []productui.AgentControlRun{{ID: "failed-run", State: "FAILED"}}, RecentRunsUnavailable: true}}}
	next := productui.PersonaAdminSnapshot{Personas: []productui.PersonaAdminPersona{{ID: "other"}, {ID: "policy", Version: "7"}}, Preview: productui.PersonaAdminPreview{Subject: "walt", Conversation: "general"}}
	got := personaAdminPreserveRunHistory(previous, next)
	if len(got.Personas[0].RecentRuns) != 0 || len(got.Personas[1].RecentRuns) != 1 || !got.Personas[1].RecentRunsUnavailable || got.Personas[1].Version != "7" || got.Preview.Conversation != "general" {
		t.Fatalf("refresh lost or crossed agent history: %+v", got)
	}
	got.Personas[1].RecentRuns[0].ID = "changed"
	if previous.Personas[0].RecentRuns[0].ID != "failed-run" || len(next.Personas[1].RecentRuns) != 0 {
		t.Fatal("refresh mutated either input projection")
	}
}

func TestAgentUXR7_K16_ResumeOutcomeDiffersFromPublication(t *testing.T) {
	snapshot := &productui.PersonaAdminSnapshot{Personas: []productui.PersonaAdminPersona{{ID: "paused", Lifecycle: productui.PersonaSuspended}, {ID: "reviewed", Lifecycle: productui.PersonaInReview}}}
	if got := personaAdminLifecycleOutcomeAction(snapshot, "paused", "PUBLISH", ""); got != "RESUME" {
		t.Fatalf("resume reported as publication: %s", got)
	}
	if got := personaAdminLifecycleOutcomeAction(snapshot, "reviewed", "PUBLISH", ""); got != "PUBLISH" {
		t.Fatalf("publication reported as resume: %s", got)
	}
	if got := personaAdminLifecycleOutcomeAction(nil, "paused", "REVIEW", "REJECT"); got != "REJECT" {
		t.Fatalf("review rejection changed: %s", got)
	}
}
