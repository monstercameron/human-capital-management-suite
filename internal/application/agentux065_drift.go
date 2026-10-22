package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// DocumentLineLabels names what one reference reads and what its document
// latest publishes (AGENTUX-065).
type DocumentLineLabels struct {
	Reading string
	Latest  string
}

// personaDocumentDriftNotices raises one notice per readable reference
// whose document moved to a newer major line than the agent reads.
func personaDocumentDriftNotices(refs []productui.PersonaAdminDocumentReference, lines map[string]DocumentLineLabels) []productui.PersonaDocumentDriftNotice {
	var out []productui.PersonaDocumentDriftNotice
	for _, ref := range refs {
		if !ref.Readable {
			continue
		}
		line, ok := lines[ref.DocumentID]
		if !ok {
			continue
		}
		reading, err := semver.Parse(line.Reading)
		if err != nil {
			continue
		}
		latest, err := semver.Parse(line.Latest)
		if err != nil {
			continue
		}
		if latest.Major <= reading.Major {
			continue
		}
		title := ref.Title
		if title == "" {
			title = ref.Label
		}
		out = append(out, productui.PersonaDocumentDriftNotice{DocumentID: ref.DocumentID, Title: title, Reading: reading.String(), Latest: latest.String()})
	}
	return out
}

// ResolveDocumentLineLabels resolves what each reference reads and what
// its document latest publishes, skipping documents the actor may not
// read (AGENTUX-065). Reading follows the reference's own mode; latest is
// the newest published label.
func (s documentService) ResolveDocumentLineLabels(ctx context.Context, tenantID, actorID string, refs []agentdocref.Reference) (map[string]DocumentLineLabels, error) {
	if err := agentdocref.Validate(refs, agentdocref.MaxPersonaReferences); err != nil {
		return nil, err
	}
	out := map[string]DocumentLineLabels{}
	for _, ref := range refs {
		line, ok, err := s.store.ResolveLineLabels(ctx, tenantID, ref.DocumentID, "person", actorID, ref)
		if err != nil {
			return nil, err
		}
		if ok {
			out[ref.DocumentID] = DocumentLineLabels{Reading: line.Reading, Latest: line.Latest}
		}
	}
	return out, nil
}
