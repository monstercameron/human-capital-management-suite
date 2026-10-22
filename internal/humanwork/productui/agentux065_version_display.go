package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Sealed version labels on agent surfaces (AGENTUX-065). Sentences keep
// their "Version" word and name the plain label; standalone chips show the
// v-form. Drafts and versions that predate labels keep the sequence.

// personaAdminVersionNumber renders the token for sentence templates that
// carry the word "Version": the plain sealed label, else the sequence.
func personaAdminVersionNumber(label, sequence string) string {
	if strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	return sequence
}

// personaAdminVersionToken renders a standalone version chip: the v-form
// for a sealed label, else the localized "Version N".
func personaAdminVersionToken(locale LocaleContext, label, sequence string) string {
	if display := chat.DisplayVersionLabel(label); display != "" {
		return display
	}
	return personaAdminText(locale, "version") + " " + personaAdminLocalizedNumber(locale, sequence)
}

// personaAdminLiveVersionLabel mirrors personaAdminLiveVersionValue: the
// sealed label of the live version, empty when the live version is
// unlabelled or unknown.
func personaAdminLiveVersionLabel(persona PersonaAdminPersona) string {
	if strings.TrimSpace(persona.LiveVersion) != "" {
		return persona.LiveVersionLabel
	}
	for _, installation := range persona.Installations {
		if strings.TrimSpace(installation.Version) != "" && installation.Version != persona.Version {
			return installation.VersionLabel
		}
	}
	return ""
}

// personaAdminVersionCell renders a bare version cell: the v-form for a
// sealed label, else the localized sequence.
func personaAdminVersionCell(locale LocaleContext, label, sequence string) string {
	if display := chat.DisplayVersionLabel(label); display != "" {
		return display
	}
	return personaAdminLocalizedNumber(locale, sequence)
}

// personaAdminDocumentNotices renders one notice per referenced document
// that moved to a newer major line than the agent reads, with a review
// link that opens the version editor (AGENTUX-065).
func personaAdminDocumentNotices(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	if len(persona.DocumentNotices) == 0 {
		return nil
	}
	editor := personaAdminVersionEditorID(persona)
	items := make([]ui.Node, 0, len(persona.DocumentNotices))
	for _, notice := range persona.DocumentNotices {
		text := strings.NewReplacer("{title}", notice.Title, "{latest}", "v"+strings.TrimPrefix(notice.Latest, "v"), "{reading}", "v"+strings.TrimPrefix(notice.Reading, "v")).Replace(personaAdminText(locale, "document_drift"))
		children := []ui.Node{ui.Text(text + " ")}
		if personaAdminVersionEditorAvailable(persona) {
			children = append(children, html.A(html.Props{Href: "#" + editor, Raw: map[string]any{"data-persona-editor-toggle": editor}}, ui.Text(personaAdminText(locale, "review_and_update"))))
		}
		items = append(items, html.P(html.Props{Class: "persona-admin-document-notice", Role: "status"}, children...))
	}
	return html.Div(html.Props{Class: "persona-admin-document-notices"}, items...)
}

// personaAdminHistoryVersionLabel reads one history row's sealed label by
// sequence, empty when the row is unlabelled or absent.
func personaAdminHistoryVersionLabel(persona PersonaAdminPersona, sequence string) string {
	for _, row := range persona.VersionHistory {
		if row.Version == sequence {
			return row.VersionLabel
		}
	}
	return ""
}
