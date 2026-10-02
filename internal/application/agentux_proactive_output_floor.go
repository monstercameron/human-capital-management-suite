package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func announcementPublicSourceTitles(output agentsecurity.FinalOutputPersistence, supplied []agentdocref.ResolvedDocument) []agentdocref.ResolvedDocument {
	documents, _, err := outputAnnouncementDocuments(output)
	if err != nil {
		return nil
	}
	var titles []agentdocref.ResolvedDocument
	for _, document := range documents {
		for _, candidate := range supplied {
			if candidate.Reference.DocumentID == document.DocumentID {
				titles = append(titles, candidate)
				break
			}
		}
	}
	return titles
}

func outputAnnouncementDocuments(output agentsecurity.FinalOutputPersistence) ([]AgentAnnouncementResolvedDocument, []string, error) {
	documents := make([]AgentAnnouncementResolvedDocument, 0)
	cited := make([]string, 0)
	seen := map[string]bool{}
	for _, citation := range output.Citations() {
		if strings.HasPrefix(citation.SourceID, "chat:") {
			continue
		}
		document, err := announcementDocumentCitation(citation)
		if err != nil || seen[document.DocumentID] {
			return nil, nil, ErrAgentAnnouncementDenied
		}
		seen[document.DocumentID] = true
		documents = append(documents, document)
		cited = append(cited, document.DocumentID)
	}
	for _, material := range output.Materials() {
		if strings.HasPrefix(material.ID, "document:") {
			matched := false
			for _, citation := range output.Citations() {
				matched = matched || citation.SourceID == material.ID
			}
			if !matched {
				return nil, nil, ErrAgentAnnouncementDenied
			}
		}
	}
	return documents, cited, nil
}

func (f *PersonaRuntimeAudienceFloor) authorizeDocumentOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence, conversation chat.Conversation, body string, bodyClass dlp.DataClass) (chat.PersonaAudienceDecision, error) {
	documents, citations, err := outputAnnouncementDocuments(output)
	if err != nil || len(documents) == 0 || f.Documents == nil || !agentAnnouncementChannel(conversation.Kind) {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	var disclosures []chatrecipient.Disclosure
	digests := map[string]string{}
	for _, citation := range output.Citations() {
		if strings.HasPrefix(citation.SourceID, "document:") {
			continue
		}
		if isNilPersonaOutputPort(f.Classes) {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		post := strings.TrimPrefix(citation.SourceID, "chat:")
		if post == citation.SourceID || post == "" || citation.Location != "conversation:"+conversation.ID+"/"+post || !personaRequestDigest(citation.Digest) {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		if prior := digests[post]; prior != "" && prior != citation.Digest {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		digests[post] = citation.Digest
		class, err := f.Classes.PersonaPublicChatDisclosureClass(ctx, conversation.TenantID, conversation.ID, post, citation.Digest)
		if err != nil || class != dlp.ClassPublic && class != dlp.ClassInternal {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		disclosures = append(disclosures, chatrecipient.Disclosure{SourceID: citation.SourceID, RecordID: post, Field: "body", DataClass: class})
	}
	snapshot, err := f.Authority.CurrentAudience(ctx, conversation)
	if err != nil {
		return chat.PersonaAudienceDecision{}, err
	}
	allowed, _, err := authorizeAnnouncementAudience(ctx, conversation.TenantID, conversation.ID, f.Documents, snapshot, documents, citations)
	if err != nil || !allowed || f.Authority.AllowDataClass(ctx, conversation, bodyClass) != nil {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	if len(disclosures) > 0 {
		ctx = WithPersonaPublicAudienceConversation(ctx, conversation.TenantID, conversation.ID)
		ctx = WithPersonaPublicAudienceSourceDigests(ctx, digests)
		decision := chatrecipient.EvaluateAudienceFloor(ctx, chatrecipient.AudienceFloorRequest{Conversation: conversation, Disclosures: disclosures}, f.Authority)
		if decision.Route != chatrecipient.RoutePublic || decision.SnapshotRevision != snapshot.Revision {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
	}
	return chat.PersonaAudienceDecision{Revision: snapshot.Revision, Body: body, ParentID: output.Identity().ThreadID}, nil
}

func (f *PersonaRuntimeAudienceFloor) WithPersonaOutputFence(ctx context.Context, output agentsecurity.FinalOutputPersistence, fn func() error) error {
	if f == nil || ctx == nil || fn == nil {
		return ErrAgentAnnouncementDenied
	}
	documents, _, err := outputAnnouncementDocuments(output)
	if err != nil {
		return err
	}
	if len(documents) == 0 {
		return fn()
	}
	fence, ok := f.Documents.(interface {
		WithDocumentReadFence(context.Context, string, []string, func() error) error
	})
	if !ok {
		return ErrAgentAnnouncementDenied
	}
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.DocumentID)
	}
	return fence.WithDocumentReadFence(ctx, output.Identity().TenantID, ids, fn)
}
