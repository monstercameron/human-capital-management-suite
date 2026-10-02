package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentUXR7_K38_OpeningImportedDraftKeepsNameAndAuthor(t *testing.T) {
	previous := productui.AgentPortableReviewDraft{ID: "draft", Name: "Policy Helper", ImportedAt: "2026-10-01T13:00:00Z", ImportedBy: "Walt Brennan", Instructions: "Old instructions"}
	next := productui.AgentPortableReviewDraft{ID: "draft", Version: 2, Instructions: "Reviewed instructions"}
	got := agentUXR7PreserveDraftMetadata(previous, next)
	if got.Name != previous.Name || got.ImportedBy != previous.ImportedBy || got.ImportedAt != previous.ImportedAt || got.Instructions != next.Instructions || got.Version != next.Version {
		t.Fatalf("opening draft lost metadata or replaced current content: %+v", got)
	}
	next.ID = "other"
	if got := agentUXR7PreserveDraftMetadata(previous, next); got.Name != "" || got.ImportedBy != "" {
		t.Fatal("metadata crossed draft identities")
	}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := productui.ResolveProductLocale(language)
		markup, err := ui.RenderToString(productui.AgentRolloutPortableMount(locale, productui.AgentRolloutSnapshot{}, productui.AgentPortableSnapshot{Available: true, Draft: &got, Drafts: []productui.AgentPortableReviewDraft{got}}))
		if err != nil || !strings.Contains(markup, "Policy Helper") || !strings.Contains(markup, "Walt Brennan") || !strings.Contains(markup, "Reviewed instructions") {
			t.Fatalf("%s opened draft lost its named author or content: %v", language, err)
		}
	}
}

func TestAgentUXR7_L12_ImportAuthorUsesTrustedFullName(t *testing.T) {
	snapshot := &productui.PersonaAdminSnapshot{SubjectOptions: []productui.PersonaAdminTarget{{ID: "walt", Label: "Walt Brennan"}}, Personas: []productui.PersonaAdminPersona{{Steward: "loretta", StewardName: "Loretta Young"}}}
	if got := agentUXR7ImportedBy(snapshot, "walt"); got != "Walt Brennan" {
		t.Fatalf("import author: %s", got)
	}
	if got := agentUXR7ImportedBy(snapshot, "loretta"); got != "Loretta Young" {
		t.Fatalf("import steward: %s", got)
	}
	if agentUXR7ImportedBy(snapshot, "internal-user-id") != "" || agentUXR7ImportedBy(nil, "walt") != "" {
		t.Fatal("unknown importer became an invented name")
	}
}
