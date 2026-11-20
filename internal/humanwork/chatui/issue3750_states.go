package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3750: a linked ticket or document says what happened to it. No access
// keeps the neutral line, a deleted record, an archived ticket and a read that
// failed each get their own words, and only the failed read offers a retry.
const (
	keyIssue3750Missing       = "chat.issue3750.missing"
	keyIssue3750Archived      = "chat.issue3750.archived"
	keyIssue3750ArchivedLabel = "chat.issue3750.archived_label"
	keyIssue3750Failed        = "chat.issue3750.failed"
)

// issue3750Styles keeps the retry button on a failed card a comfortable touch
// target on phones and lets it sit under the line in a narrow card.
const issue3750Styles = `.chat-embed .chat-embed-retry{justify-self:start;max-inline-size:100%}@media(max-width:767px),(pointer:coarse){.chat-workspace .chat-embed .chat-embed-retry{min-block-size:44px;min-inline-size:44px}}`

var issue3750Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3750Missing:       "This was deleted or no longer exists",
		keyIssue3750Archived:      "This was archived",
		keyIssue3750ArchivedLabel: "Archived",
		keyIssue3750Failed:        "Could not load this preview",
	},
	"de-DE": {
		keyIssue3750Missing:       "Dies wurde gelöscht oder existiert nicht mehr",
		keyIssue3750Archived:      "Dies wurde archiviert",
		keyIssue3750ArchivedLabel: "Archiviert",
		keyIssue3750Failed:        "Diese Vorschau konnte nicht geladen werden",
	},
	"ar": {
		keyIssue3750Missing:       "تم حذف هذا العنصر أو لم يعد موجوداً",
		keyIssue3750Archived:      "تمت أرشفة هذا العنصر",
		keyIssue3750ArchivedLabel: "مؤرشف",
		keyIssue3750Failed:        "تعذّر تحميل هذه المعاينة",
	},
}

// issue3750WorkStateText is the line a settled project card prints for state.
// Anything but a known state is the neutral no-access line.
func issue3750WorkStateText(m Model, state string) string {
	switch state {
	case "missing":
		return laneText(m, issue3750Copy, keyIssue3750Missing)
	case "archived":
		return laneText(m, issue3750Copy, keyIssue3750Archived)
	case "failed":
		return laneText(m, issue3750Copy, keyIssue3750Failed)
	}
	return m.t(KeyProjectTaskRestricted)
}

// issue3750FailedCard is the body of a card whose read failed: the line, and a
// real button that reads the preview again.
func issue3750FailedCard(m Model) []ui.Node {
	return []ui.Node{
		html.Span(html.Props{Class: "chat-doc-embed-locked", Role: "status", Text: laneText(m, issue3750Copy, keyIssue3750Failed)}),
		html.Button(html.Props{Class: "button secondary small chat-embed-retry", Type: "button", Data: map[string]string{"action": "work-preview-retry"}, Text: m.t(KeyRetry)}),
	}
}

// issue3750ArchivedChip marks a readable card whose ticket or board is archived.
func issue3750ArchivedChip(m Model) ui.Node {
	return html.Span(html.Props{Class: "chat-project-embed-status chat-work-card-status", Data: map[string]string{"tone": "archived"}, Text: laneText(m, issue3750Copy, keyIssue3750ArchivedLabel)})
}

// issue3750WorkData names the archived answer on its card; other cards keep
// their attributes unchanged.
func issue3750WorkData(preview ProjectTaskPreview, data map[string]string) map[string]string {
	if preview.State == "archived" {
		data["embed-state"] = "archived"
	}
	return data
}
