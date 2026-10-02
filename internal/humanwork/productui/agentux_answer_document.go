package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type AgentAnswerDocumentCitation struct{ VersionID, Anchor string }

type agentAnswerVersionState struct {
	Key            string
	Version        DocumentVersionProjection
	Label          string
	Loaded, Failed bool
}

func agentAnswerCitedDocument(props docsDetailProps) ui.Node {
	view := props.View
	citation := agentAnswerDocumentRoute()
	if view.Document.Citation != nil {
		citation = *view.Document.Citation
	}
	state := ui.UseState(agentAnswerVersionState{})
	compare := ui.UseState(false)
	key := view.Document.Summary.ID + "/" + citation.VersionID
	currentID := view.Document.Summary.VersionID
	wanted := citation.VersionID != "" && citation.VersionID != currentID
	ui.UseEffectOf(func() func() {
		if !wanted {
			return nil
		}
		active := true
		settle := func(version DocumentVersionProjection, err error) {
			ui.PostAsync(func() {
				if !active {
					return
				}
				valid := err == nil && version.Readable && version.DocumentID == view.Document.Summary.ID && version.VersionID == citation.VersionID
				label := chat.DisplayVersionLabel(version.Version)
				state.Set(agentAnswerVersionState{Key: key, Version: version, Label: label, Loaded: valid, Failed: !valid || label == "" && view.ListDocumentVersions == nil})
			})
			if err == nil && view.ListDocumentVersions != nil {
				view.ListDocumentVersions(view.Document.Summary.ID, func(versions []DocumentVersionSummary, err error) {
					ui.PostAsync(func() {
						if !active {
							return
						}
						current := state.Get()
						if current.Key != key {
							return
						}
						if err != nil {
							current.Loaded, current.Failed = false, true
							state.Set(current)
							return
						}
						for index, version := range versions {
							if version.VersionID == citation.VersionID && !version.Redacted {
								current.Label = chat.DisplayVersionLabel(version.Version)
								if current.Label == "" {
									current.Label = chat.DisplayVersion(uint64(index + 1))
								}
								state.Set(current)
								return
							}
						}
						current.Loaded, current.Failed = false, true
						state.Set(current)
					})
				})
			}
		}
		if view.CompareDocumentVersions == nil {
			state.Set(agentAnswerVersionState{Key: key, Failed: true})
		} else {
			view.CompareDocumentVersions(view.Document.Summary.ID, citation.VersionID, settle)
		}
		return func() { active = false }
	}, struct{ Key, Current string }{key, currentID})
	loaded := state.Get()
	ready := !wanted || loaded.Key == key && loaded.Loaded && !loaded.Failed && loaded.Label != ""
	ui.UseEffectOf(func() func() {
		if !ready || citation.Anchor == "" {
			return nil
		}
		return agentAnswerRevealSection(citation.Anchor)
	}, struct {
		Key, Anchor string
		Ready       bool
	}{key, citation.Anchor, ready})
	if wanted && !ready {
		message := agentAnswerDocsCopy(view.Locale.Resolved, "loading")
		if loaded.Key == key && loaded.Failed {
			message = agentAnswerDocsCopy(view.Locale.Resolved, "failed")
		}
		return html.Section(html.Props{Role: "status", Class: "docs-cited-version-state"}, html.P(html.Props{Text: message}), appLink(view, html.Props{}, "/workspace/app/docs?document="+url.QueryEscape(view.Document.Summary.ID), ui.Text(agentAnswerDocsCopy(view.Locale.Resolved, "current"))))
	}
	detail := *view.Document
	var banner ui.Node
	if wanted {
		detail.Summary.VersionID, detail.Summary.Version = loaded.Version.VersionID, loaded.Label
		detail.Summary.Title, detail.Markdown = loaded.Version.Title, loaded.Version.Markdown
		// Version-local comments and resolved embeds must be read for that version
		// rather than reused from the current version. Until loaded they are absent.
		detail.Comments, detail.Links, detail.ProjectTasks, detail.Journeys = nil, nil, nil, nil
		detail.Chat = DocumentChatRefs{}
		detail.ContentHash = ""
		detail.CanEdit, detail.CanComment = false, false
		detail.Summary.CanEdit, detail.Summary.CanComment = false, false
		view.Document = &detail
		banner = agentAnswerVersionNotice(view, loaded.Label, func() { compare.Set(true) })
	}
	children := []ui.Node{banner, ui.CreateElement(docsDetail, docsDetailProps{View: view})}
	if ready && citation.Anchor != "" && !agentAnswerDocumentHasSection(detail.Markdown, citation.Anchor) {
		children = append([]ui.Node{html.P(html.Props{Role: "status", Text: agentAnswerDocsCopy(view.Locale.Resolved, "missing")})}, children...)
	}
	if compare.Get() && view.CompareDocumentVersions != nil {
		children = append(children, ui.CreateElement(docsCompareDialog, docsCompareDialogProps{Locale: view.Locale.Resolved, DocumentID: detail.Summary.ID, Base: DocumentVersionProjection{DocumentID: detail.Summary.ID, VersionID: detail.Summary.VersionID, Title: detail.Summary.Title, Markdown: detail.Markdown, Readable: true}, ListVersions: view.ListDocumentVersions, CompareDocumentVersions: view.CompareDocumentVersions, Close: func() { compare.Set(false) }}))
	}
	return html.Div(html.Props{Class: "docs-cited-version"}, children...)
}

func agentAnswerVersionNotice(view View, label string, compare func()) ui.Node {
	message := agentAnswerDocsCopy(view.Locale.Resolved, "newer")
	if label != "" {
		message = strings.ReplaceAll(agentAnswerDocsCopy(view.Locale.Resolved, "viewing"), "{version}", label) + " " + message
	}
	return html.Aside(html.Props{Role: "status", Class: "docs-cited-version-notice"}, html.P(html.Props{Text: message}), appLink(view, html.Props{}, "/workspace/app/docs?document="+url.QueryEscape(view.Document.Summary.ID), ui.Text(agentAnswerDocsCopy(view.Locale.Resolved, "current"))), html.Button(html.Props{Type: "button", Text: agentAnswerDocsCopy(view.Locale.Resolved, "compare"), OnClick: ui.UseEvent(func() { compare() })}))
}

func agentAnswerDocsCopy(locale, key string) string {
	copy := map[string]string{"loading": "Opening the cited document…", "failed": "This document could not be opened. Open the current document or try again.", "viewing": "You are viewing {version}.", "newer": "A newer version exists.", "current": "Open current", "compare": "Compare", "missing": "This section is no longer in the document."}
	if strings.HasPrefix(locale, "de") {
		copy = map[string]string{"loading": "Das zitierte Dokument wird geöffnet…", "failed": "Dieses Dokument konnte nicht geöffnet werden. Öffnen Sie das aktuelle Dokument oder versuchen Sie es erneut.", "viewing": "Sie sehen {version}.", "newer": "Eine neuere Fassung ist verfügbar.", "current": "Aktuelle Fassung öffnen", "compare": "Vergleichen", "missing": "Dieser Abschnitt ist nicht mehr im Dokument."}
	}
	if strings.HasPrefix(locale, "ar") {
		copy = map[string]string{"loading": "جارٍ فتح المستند المشار إليه…", "failed": "تعذر فتح هذا المستند. افتح المستند الحالي أو حاول مرة أخرى.", "viewing": "أنت تعرض {version}.", "newer": "توجد نسخة أحدث.", "current": "افتح النسخة الحالية", "compare": "قارن", "missing": "لم يعد هذا القسم موجودًا في المستند."}
	}
	return copy[key]
}
