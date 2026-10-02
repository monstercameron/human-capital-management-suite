package productui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentRequestDocumentSuggestion is the request picker's authorized document
// projection. Owner and folder remain separate so equal titles are still
// distinguishable, and Snippet is present only for a body-text match.
type AgentRequestDocumentSuggestion struct {
	DocumentID       string
	Title            string
	Owner            string
	Folder           string
	Updated          string
	Snippet          string
	PublishedVersion uint64
}

// AgentRequestDocumentResult renders one full-width listbox result.
func AgentRequestDocumentResult(locale LocaleContext, suggestion AgentRequestDocumentSuggestion, index int) ui.Node {
	owner := strings.TrimSpace(suggestion.Owner)
	if owner == "" {
		owner = locale.Text("agents.document_owner_unknown")
	}
	folder := strings.TrimSpace(suggestion.Folder)
	if folder == "" {
		folder = locale.Text("agents.document_folder_none")
	}
	updated := strings.TrimSpace(suggestion.Updated)
	if updated == "" {
		updated = locale.Text("agents.document_updated_unknown")
	}
	if strings.TrimSpace(suggestion.Folder) == "" {
		children := []ui.Node{
			html.Strong(html.Props{Dir: "auto"}, ui.Text(strings.TrimSpace(suggestion.Title))),
			html.Small(html.Props{Class: "muted agents-document-result-meta"}, ui.Text(owner+" · "+locale.Text("agents.document_updated", map[string]string{"date": updated}))),
		}
		if snippet := strings.TrimSpace(suggestion.Snippet); snippet != "" {
			children = append(children, html.Small(html.Props{Class: "agents-document-result-snippet", Dir: "auto"}, ui.Text(snippet)))
		}
		return html.Li(html.Props{ID: "agents-document-option-" + strconv.Itoa(index), Role: "option", Raw: map[string]any{
			"tabindex": "0", "data-agent-document-option": suggestion.DocumentID, "data-agent-document-title": suggestion.Title,
			"data-agent-document-anchor": "", "aria-selected": "false",
		}}, children...)
	}
	children := []ui.Node{
		html.Strong(html.Props{Dir: "auto"}, ui.Text(strings.TrimSpace(suggestion.Title))),
		html.Small(html.Props{Class: "muted agents-document-result-meta"}, ui.Text(owner+" · "+folder+" · "+locale.Text("agents.document_updated", map[string]string{"date": updated}))),
	}
	if snippet := strings.TrimSpace(suggestion.Snippet); snippet != "" {
		children = append(children, html.Small(html.Props{Class: "agents-document-result-snippet", Dir: "auto"}, ui.Text(snippet)))
	}
	return html.Li(html.Props{ID: "agents-document-option-" + strconv.Itoa(index), Role: "option", Raw: map[string]any{
		"tabindex": "0", "data-agent-document-option": suggestion.DocumentID, "data-agent-document-title": suggestion.Title,
		"data-agent-document-anchor": "", "aria-selected": "false",
	}}, children...)
}

// AgentRequestDocumentChip renders an attached document as a document link
// with one compact, explicitly labelled remove control.
func AgentRequestDocumentChip(locale LocaleContext, documentID, title, anchor string) ui.Node {
	removeLabel := locale.Text("agents.document_remove_named", map[string]string{"title": title})
	href := "/workspace/app/docs?document=" + url.QueryEscape(documentID)
	if strings.TrimSpace(anchor) != "" {
		href += "#" + url.PathEscape(anchor)
	}
	return html.Li(html.Props{Class: "agents-document-chip", Raw: map[string]any{
		"data-agent-request-reference": "true", "data-document-id": documentID, "data-document-label": title, "data-section-anchor": anchor,
	}},
		productIcon("document", "agents-document-icon"),
		html.A(html.Props{Href: href, Dir: "auto"}, ui.Text(title)),
		html.Button(html.Props{Type: "button", Aria: map[string]string{"label": removeLabel}, Raw: map[string]any{"data-agent-document-remove": "true"}}, ui.Text("×")),
	)
}
