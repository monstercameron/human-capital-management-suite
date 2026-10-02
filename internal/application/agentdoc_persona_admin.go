package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

// ErrPersonaDocumentUnreadable is safe for the persona editor to explain.
// It intentionally discloses neither document content nor a backend reason.
var ErrPersonaDocumentUnreadable = errors.New("You cannot read this document")

// PersonaAdminDocumentReader is the documentation hub's existing authorized
// batched preview port. The tenant and actor are always taken from trust context.
type PersonaAdminDocumentReader interface {
	GetDocumentPreviews(context.Context, string, string, []string) ([]transportdocument.Preview, error)
}

// bindAdminDocumentReader attaches the late-composed documentation service to
// the already-composed catalog and, when available, its command factory.
func (w *personaServeWiring) bindAdminDocumentReader(documents PersonaAdminDocumentReader) error {
	if w == nil || documents == nil {
		return ErrPersonaAdminCommandUnavailable
	}
	catalog, ok := w.adminCatalog.(readOnlyPersonaAdminCatalog)
	if !ok || catalog.service == nil {
		return ErrPersonaCatalogDenied
	}
	catalog.service.Documents = documents
	if factory, ok := w.adminFactory.(*PersonaAdminCommandFactory); ok && factory != nil {
		factory.documents = documents
	}
	return nil
}

// NewPersonaAdminCommandFactoryWithDocumentReader enables admission-time
// document authorization without making the documentation hub mandatory for
// deployments that do not expose document references yet.
func NewPersonaAdminCommandFactoryWithDocumentReader(catalog productui.PersonaAdminClient, authorizer PersonaAdminCommandAuthorizer, executor PersonaAdminCommandExecutor, documents PersonaAdminDocumentReader) (*PersonaAdminCommandFactory, error) {
	factory, err := NewPersonaAdminCommandFactory(catalog, authorizer, executor)
	if err != nil {
		return nil, err
	}
	factory.documents = documents
	return factory, nil
}

// NewPersonaAdminCatalogClientWithDocumentReader composes the catalog with
// viewer-specific document titles while preserving the existing read-only
// catalog contract.
func NewPersonaAdminCatalogClientWithDocumentReader(deps PersonaAdminCatalogComposition, documents PersonaAdminDocumentReader) (productui.PersonaAdminClient, error) {
	client, err := NewPersonaAdminCatalogClient(deps)
	if err != nil {
		return nil, err
	}
	catalog, ok := client.(readOnlyPersonaAdminCatalog)
	if !ok || catalog.service == nil {
		return nil, ErrPersonaCatalogDenied
	}
	catalog.service.Documents = documents
	return catalog, nil
}

// NewPersonaAdminCommandSurfaceWithDocumentReader composes the reviewed
// lifecycle surface with admission-time document readability checks.
func NewPersonaAdminCommandSurfaceWithDocumentReader(config PersonaAdminCommandSurfaceConfig, documents PersonaAdminDocumentReader) (*PersonaAdminCommandFactory, error) {
	factory, err := NewPersonaAdminCommandSurface(config)
	if err != nil {
		return nil, err
	}
	factory.documents = documents
	return factory, nil
}

// ExecutePersonaAdminCommandWithDocumentReferences is the typed extension
// used by the HTTP draft and create-version boundaries. An explicit empty
// slice clears the references; nil keeps a previous version's references.
func (f *PersonaAdminCommandFactory) ExecutePersonaAdminCommandWithDocumentReferences(ctx context.Context, request productui.PersonaAdminCommandRequest, refs []agentdocref.Reference) error {
	if (request.Action != string(PersonaAdminCreateDraft) && request.Action != string(PersonaAdminCreateVersion)) || refs == nil {
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	if err := agentdocref.Validate(refs, agentdocref.MaxPersonaReferences); err != nil {
		return personaAdminCommandError(personaAdminCodeInvalid, err)
	}
	if request.Action == string(PersonaAdminCreateDraft) {
		if !personaAdminRequestHasSupportedFields(request) {
			return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
		}
		starter := &PersonaStarterDraftRequest{StarterID: request.StarterID, StarterVersion: request.StarterVersion, PersonaID: request.PersonaID, AvatarRef: request.AvatarRef, OrganizationScopes: append([]string(nil), request.OrganizationScopes...), ManifestID: request.ManifestID, BusinessOwnerID: request.BusinessOwnerID, TechnicalStewardID: request.TechnicalStewardID, Guidance: personaAdminGuidance(request.Instructions), DocumentReferences: cloneAgentDocumentReferences(refs)}
		return f.Execute(ctx, PersonaAdminCommand{Action: PersonaAdminCreateDraft, PersonaID: request.PersonaID, Starter: starter})
	}
	channels, ok := personaAdminChannelClasses(request.AllowedChannels)
	if !ok || len(request.OrganizationScopes) > 0 || request.AvatarRef != "" || request.ManifestID != "" || request.ReviewID != "" || request.EvaluationRunID != "" || request.Decision != "" || request.Reason != "" || request.ConversationID != "" {
		return personaAdminCommandError(personaAdminCodeInvalid, ErrPersonaAdminCommandUnavailable)
	}
	edit := &PersonaStarterVersionRequest{StarterID: request.StarterID, StarterVersion: request.StarterVersion, Handle: request.Handle, DisplayName: request.DisplayName, Purpose: request.Purpose, Guidance: clonePersonaAdminGuidance(request.Instructions), DocumentReferences: cloneAgentDocumentReferences(refs), ChannelClasses: channels, BusinessOwnerID: request.BusinessOwnerID, TechnicalStewardID: request.TechnicalStewardID}
	return f.Execute(ctx, PersonaAdminCommand{Action: PersonaAdminCreateVersion, PersonaID: request.PersonaID, StarterVersionEdit: edit})
}

func cloneAgentDocumentReferences(refs []agentdocref.Reference) []agentdocref.Reference {
	if refs == nil {
		return nil
	}
	out := make([]agentdocref.Reference, len(refs))
	copy(out, refs)
	return out
}

func resolvePersonaAdminDocumentReferences(ctx context.Context, reader PersonaAdminDocumentReader, tenant, actor string, refs []agentdocref.Reference) ([]productui.PersonaAdminDocumentReference, error) {
	if err := agentdocref.Validate(refs, agentdocref.MaxPersonaReferences); err != nil {
		return nil, err
	}
	out := make([]productui.PersonaAdminDocumentReference, len(refs))
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = ref.DocumentID
		out[i] = productui.PersonaAdminDocumentReference{DocumentID: ref.DocumentID, VersionMode: string(ref.VersionMode), PinnedVersion: ref.PinnedVersion, SectionAnchor: ref.SectionAnchor, Label: ref.Label}
	}
	if len(refs) == 0 || reader == nil {
		return out, nil
	}
	rows, err := reader.GetDocumentPreviews(ctx, tenant, actor, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve persona document previews: %w", err)
	}
	byID := make(map[string]transportdocument.Preview, len(rows))
	for _, row := range rows {
		if _, expected := byID[row.DocumentID]; expected || !containsAgentDocumentID(ids, row.DocumentID) {
			return nil, errors.New("resolve persona document previews: invalid result")
		}
		byID[row.DocumentID] = row
	}
	for i := range out {
		row, found := byID[out[i].DocumentID]
		if found && row.Readable && strings.TrimSpace(row.Title) != "" {
			out[i].Readable = true
			out[i].Title = row.Title
			if personaAdminReferenceNeedsLocation(out[i].Label) {
				out[i].Location = strings.TrimSpace(row.OwnerName)
			}
		}
	}
	return out, nil
}

func personaAdminReferenceNeedsLocation(label string) bool {
	label = strings.TrimSpace(label)
	return label == "" || (!strings.Contains(label, "·") && !strings.Contains(label, "("))
}

func containsAgentDocumentID(ids []string, wanted string) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}
