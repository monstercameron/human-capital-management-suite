package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

func personaAdminInstructionsSection(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	instructions := strings.TrimSpace(persona.Guidance)
	if instructions == "" {
		return nil
	}
	language := strings.TrimSpace(persona.PurposeLanguage)
	if language == "" {
		language = "und"
	}
	return html.Details(html.Props{Class: "persona-admin-instructions", Raw: map[string]any{"data-agentdoc-instructions-readmode": "true"}},
		html.Summary(html.Props{}, ui.Text(agentDocInstructionText(locale, "label"))),
		html.P(html.Props{Dir: "auto", Raw: map[string]any{"lang": language}}, personaAdminInstructionParts(instructions, persona.DocumentReferences)...),
	)
}

func personaAdminInstructionParts(text string, references []PersonaAdminDocumentReference) []ui.Node {
	byID := make(map[string]PersonaAdminDocumentReference, len(references))
	for _, reference := range references {
		byID[reference.DocumentID] = reference
	}
	parts := make([]ui.Node, 0)
	for offset := 0; offset < len(text); {
		start := strings.Index(text[offset:], "{{doc:")
		if start < 0 {
			parts = append(parts, ui.Text(text[offset:]))
			break
		}
		start += offset
		end := strings.Index(text[start+len("{{doc:"):], "}}")
		if end < 0 {
			parts = append(parts, ui.Text(text[offset:]))
			break
		}
		end += start + len("{{doc:")
		documentID := text[start+len("{{doc:") : end]
		parts = append(parts, ui.Text(text[offset:start]))
		if reference, ok := byID[documentID]; ok {
			parts = append(parts, personaAdminInstructionDocumentChip(reference))
		} else {
			parts = append(parts, ui.Text(agentdocref.Token(documentID)))
		}
		offset = end + len("}}")
	}
	return parts
}

func personaAdminInstructionDocumentChip(reference PersonaAdminDocumentReference) ui.Node {
	title := strings.TrimSpace(reference.Title)
	if title == "" {
		title = strings.TrimSpace(reference.Label)
	}
	if title == "" {
		title = reference.DocumentID
	}
	props := html.Props{Class: "persona-admin-instruction-chip", Dir: "auto"}
	if reference.Readable {
		return html.A(html.Props{Class: "persona-admin-instruction-chip", Href: agentDocumentHubHref(reference.DocumentID), Target: "_blank", Raw: map[string]any{"rel": "noopener noreferrer"}}, ui.Text(title))
	}
	return html.Span(props, ui.Text(title))
}
