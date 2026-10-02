package productui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// AgentRequestDocumentPicker is the request-specific shell around the shared
// documentation-hub suggestion service. Requests always track the latest
// published version and have the decision-record limit of five references.
func AgentRequestDocumentPicker(locale LocaleContext) ui.Node {
	limit := strconv.Itoa(agentdocref.MaxRequestReferences)
	localizedLimit := locale.FormatNumber(limit, 0)
	return html.Div(html.Props{Class: "agents-request-documents", Raw: map[string]any{
		"data-agent-request-documents": "true",
		"data-agentdoc-limit":          limit,
		"data-msg-loading":             locale.Text("agents.documents_searching"),
		"data-msg-empty":               locale.Text("agents.documents_empty"),
		"data-msg-failed":              locale.Text("agents.documents_search_failed"),
		"data-msg-limit":               locale.Text("agents.documents_limit"),
		"data-msg-remove":              locale.Text("agents.document_remove"),
		"data-msg-open":                locale.Text("agents.add_document"),
		"data-msg-done":                locale.Text("agents.document_done"),
		"data-msg-count":               locale.Text("agents.documents_count", map[string]string{"count": "__COUNT__", "limit": localizedLimit}),
	}},
		html.Div(html.Props{Class: "agents-document-heading"},
			html.Strong(html.Props{}, ui.Text(locale.Text("agents.documents_label"))),
			html.P(html.Props{Class: "muted agents-document-access"}, ui.Text(locale.Text("agents.documents_access_help"))),
		),
		html.Button(html.Props{Class: "button secondary agents-add-document", Type: "button", Raw: map[string]any{"data-agent-document-open": "true", "aria-expanded": "false", "aria-controls": "agents-document-picker-panel"}}, ui.Text(locale.Text("agents.add_document"))),
		html.Div(html.Props{ID: "agents-document-picker-panel", Class: "agents-document-picker-panel", Hidden: true, Raw: map[string]any{"data-agent-document-panel": "true"}},
			html.Label(html.Props{For: "agents-document-search"}, ui.Text(locale.Text("agents.document_search_label"))),
			html.Input(html.Props{ID: "agents-document-search", Type: "search", Placeholder: locale.Text("agents.document_search_placeholder"), Raw: map[string]any{
				"role": "combobox", "autocomplete": "off", "aria-autocomplete": "list", "aria-controls": "agents-document-results", "aria-expanded": "false", "data-agent-document-search": "true",
			}}),
			html.P(html.Props{ID: "agents-document-search-status", Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite", "data-agent-document-status": "true"}}, ui.Text("")),
			html.Ul(html.Props{ID: "agents-document-results", Class: "agents-document-results", Role: "listbox", Hidden: true, Raw: map[string]any{"data-agent-document-results": "true"}}),
		),
		html.Div(html.Props{Class: "agents-document-selection", Raw: map[string]any{"data-agent-document-selection": "true"}},
			html.Ul(html.Props{Class: "agents-document-chips", Raw: map[string]any{"data-agent-document-chips": "true"}}),
			html.P(html.Props{Class: "muted agents-document-count", Hidden: true, Raw: map[string]any{"data-agent-document-count": "true"}}),
		),
	)
}
