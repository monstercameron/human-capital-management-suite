package productui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// AgentDocInstructionMentionLabel is the administrator-visible document name.
// Persisted labels win because they preserve a folder/owner disambiguator that
// was chosen when the reference was added.
func AgentDocInstructionMentionLabel(reference PersonaAdminDocumentReference, references []PersonaAdminDocumentReference) string {
	title := strings.TrimSpace(reference.Title)
	label := strings.TrimSpace(reference.Label)
	if label != "" && label != title {
		return label
	}
	if title == "" {
		title = label
	}
	if title == "" {
		title = reference.DocumentID
	}
	duplicate := false
	for _, candidate := range references {
		if candidate.DocumentID != reference.DocumentID && strings.EqualFold(strings.TrimSpace(candidate.Title), strings.TrimSpace(reference.Title)) && strings.TrimSpace(reference.Title) != "" {
			duplicate = true
			break
		}
	}
	if duplicate && strings.TrimSpace(reference.Location) != "" {
		return title + " (" + strings.TrimSpace(reference.Location) + ")"
	}
	return title
}

// AgentDocInstructionDisplayText converts sealed document tokens to the form
// an administrator edits. Internal document identifiers never reach the field.
func AgentDocInstructionDisplayText(stored string, references []PersonaAdminDocumentReference) string {
	byID := make(map[string]PersonaAdminDocumentReference, len(references))
	for _, reference := range references {
		byID[reference.DocumentID] = reference
	}
	var out strings.Builder
	for offset := 0; offset < len(stored); {
		start := strings.Index(stored[offset:], "{{doc:")
		if start < 0 {
			out.WriteString(stored[offset:])
			break
		}
		start += offset
		end := strings.Index(stored[start+len("{{doc:"):], "}}")
		if end < 0 {
			out.WriteString(stored[offset:])
			break
		}
		end += start + len("{{doc:")
		documentID := stored[start+len("{{doc:") : end]
		out.WriteString(stored[offset:start])
		if reference, ok := byID[documentID]; ok {
			out.WriteString("@[")
			out.WriteString(AgentDocInstructionMentionLabel(reference, references))
			out.WriteString("]")
		} else {
			// An invalid historic token is intentionally not exposed as an ID.
			out.WriteString("@[unavailable document]")
		}
		offset = end + len("}}")
	}
	return out.String()
}

// AgentDocInstructionStoredText validates and converts visible mentions at the
// submission edge. It also returns only references still mentioned in text, so
// deleting a mention removes its reference.
func AgentDocInstructionStoredText(visible string, references []PersonaAdminDocumentReference) (string, []PersonaAdminDocumentReference, []string) {
	byLabel := make(map[string][]PersonaAdminDocumentReference, len(references))
	for _, reference := range references {
		label := AgentDocInstructionMentionLabel(reference, references)
		byLabel[label] = append(byLabel[label], reference)
	}
	keptByID := make(map[string]PersonaAdminDocumentReference, len(references))
	unknown := make([]string, 0)
	var out strings.Builder
	for offset := 0; offset < len(visible); {
		start := strings.Index(visible[offset:], "@[")
		if start < 0 {
			out.WriteString(visible[offset:])
			break
		}
		start += offset
		end := strings.Index(visible[start+2:], "]")
		if end < 0 {
			out.WriteString(visible[offset:])
			break
		}
		end += start + 2
		label := visible[start+2 : end]
		out.WriteString(visible[offset:start])
		matches := byLabel[label]
		if len(matches) != 1 {
			unknown = append(unknown, label)
			out.WriteString(visible[start : end+1])
		} else {
			out.WriteString(agentdocref.Token(matches[0].DocumentID))
			keptByID[matches[0].DocumentID] = matches[0]
		}
		offset = end + 1
	}
	kept := make([]PersonaAdminDocumentReference, 0, len(keptByID))
	for _, reference := range references {
		if _, ok := keptByID[reference.DocumentID]; ok {
			kept = append(kept, reference)
		}
	}
	return out.String(), kept, unknown
}
