package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3750Render(t *testing.T, nodes ...ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.Fragment(nodes...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func issue3750Ticket(t *testing.T, m Model, preview ProjectTaskPreview) string {
	t.Helper()
	m.EmbedOrigin = "https://hcm.example"
	preview.ProjectID, preview.TaskID = "proj-7", "task-19"
	m.ProjectTaskPreviews = map[string]ProjectTaskPreview{ProjectTaskPreviewKey("proj-7", "task-19"): preview}
	return issue3750Render(t, projectPreviewEmbeds(m, "https://hcm.example/workspace/app/project?project=proj-7&task=task-19")...)
}

// TestIssue3750_TicketStatesSayDifferentThings pins that no access, deleted,
// archived and failed tickets each print their own words, and only the failed
// one offers a retry.
func TestIssue3750_TicketStatesSayDifferentThings(t *testing.T) {
	restricted := issue3750Ticket(t, Model{}, ProjectTaskPreview{State: "restricted"})
	missing := issue3750Ticket(t, Model{}, ProjectTaskPreview{State: "missing"})
	archived := issue3750Ticket(t, Model{}, ProjectTaskPreview{State: "archived"})
	failed := issue3750Ticket(t, Model{}, ProjectTaskPreview{State: "failed"})
	seen := map[string]string{}
	for name, markup := range map[string]string{"restricted": restricted, "missing": missing, "archived": archived, "failed": failed} {
		text := markup[strings.Index(markup, "chat-doc-embed-locked"):]
		if other, dup := seen[text]; dup {
			t.Errorf("%s and %s render the same card text:\n%s", name, other, markup)
		}
		seen[text] = name
		if want := strings.Contains(markup, "work-preview-retry"); want != (name == "failed") {
			t.Errorf("%s: retry button present=%v:\n%s", name, want, markup)
		}
	}
	for state, want := range map[string]string{"missing": "deleted or no longer exists", "archived": "was archived", "failed": "Could not load this preview"} {
		if !strings.Contains(issue3750Ticket(t, Model{}, ProjectTaskPreview{State: state}), want) {
			t.Errorf("%s card must say %q", state, want)
		}
	}
	if !strings.Contains(restricted, "Project task unavailable") {
		t.Errorf("no-access card keeps the neutral line, got:\n%s", restricted)
	}
}

// TestIssue3750_ArchivedTicketShowsTitleWhenReadable pins that an archived
// ticket the reader may open keeps its title and says it is archived.
func TestIssue3750_ArchivedTicketShowsTitleWhenReadable(t *testing.T) {
	markup := issue3750Ticket(t, Model{}, ProjectTaskPreview{State: "archived", Readable: true, Title: "Fix payroll export"})
	if !strings.Contains(markup, "Fix payroll export") || !strings.Contains(markup, "Archived") || !strings.Contains(markup, `data-embed-state="archived"`) {
		t.Fatalf("archived readable ticket must show title and Archived, got:\n%s", markup)
	}
}

// TestIssue3750_DocumentFailedIsNotTheNeutralCard pins that a transient read
// failure on a document card is retryable and is not the neutral line.
func TestIssue3750_DocumentFailedIsNotTheNeutralCard(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example", DocPreviews: map[string]DocPreview{}}
	for state, retry := range map[string]bool{"unavailable": false, "failed": true} {
		m.DocPreviews["abc"] = DocPreview{ID: "abc", State: state}
		markup := issue3750Render(t, docPreviewEmbedsFor(m, "doc:abc", false)...)
		if got := strings.Contains(markup, `data-action="work-preview-retry"`); got != retry {
			t.Errorf("%s: retry present=%v, want %v:\n%s", state, got, retry, markup)
		}
		if state == "failed" && (strings.Contains(markup, "Document unavailable") || !strings.Contains(markup, "Could not load this preview")) {
			t.Errorf("failed document must not use the neutral line:\n%s", markup)
		}
		if state == "unavailable" && !strings.Contains(markup, "Document unavailable") {
			t.Errorf("unavailable document keeps the neutral line:\n%s", markup)
		}
	}
}

// TestIssue3750_StateCopyInEveryLanguage pins the new words in de-DE and ar.
func TestIssue3750_StateCopyInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		for _, key := range []string{keyIssue3750Missing, keyIssue3750Archived, keyIssue3750ArchivedLabel, keyIssue3750Failed} {
			if got := laneText(m, issue3750Copy, key); got == "" || got == key || (locale != "en-US" && got == laneText(Model{Locale: "en-US"}, issue3750Copy, key)) {
				t.Errorf("%s %s: missing translation, got %q", locale, key, got)
			}
		}
	}
}
