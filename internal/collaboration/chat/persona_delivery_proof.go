package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

var ErrPersonaProofUnavailable = errors.New("chat: persona delivery proof unavailable")

type personaDeliveryProof struct {
	tenantID, conversationID, authorID, outputDigest, bodyDigest, parentID string
	audienceRevision                                                       uint64
}

func (personaDeliveryProof) personaDeliveryProof() {}

func (p personaDeliveryProof) ValidFor(tenantID, conversationID, authorID string, revision uint64, outputDigest, body, parentID string) bool {
	return p.tenantID == tenantID && p.conversationID == conversationID && p.authorID == authorID && p.audienceRevision == revision && p.outputDigest == outputDigest && p.parentID == parentID && p.bodyDigest == digestBody(body, parentID)
}

// IssuePersonaDeliveryProof reauthorizes every material through the current
// audience floor, then binds the sanitized body and thread to the opaque
// validated output projection and exact audience revision. The floor must use
// a shared version lease when its audience state lives outside chat; otherwise
// it must refuse to issue a public proof.
func IssuePersonaDeliveryProof(ctx context.Context, output agentsecurity.FinalOutputPersistence, floor PersonaAudienceFloor) (PersonaDeliveryProof, error) {
	if ctx == nil || floor == nil {
		return nil, ErrPersonaProofUnavailable
	}
	identity := output.Identity()
	if strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.ConversationID) == "" || strings.TrimSpace(identity.ThreadID) == "" || strings.TrimSpace(identity.PersonaID) == "" || strings.TrimSpace(output.Digest()) == "" {
		return nil, ErrPersonaProofUnavailable
	}
	if _, _, err := output.Payload(); err != nil {
		return nil, ErrPersonaProofUnavailable
	}
	decision, err := floor.AuthorizePersonaOutput(ctx, output)
	if err != nil || decision.Revision == 0 || strings.TrimSpace(decision.Body) == "" || strings.TrimSpace(decision.ParentID) == "" || decision.ParentID != identity.ThreadID {
		return nil, ErrPersonaProofUnavailable
	}
	return personaDeliveryProof{tenantID: identity.TenantID, conversationID: identity.ConversationID, authorID: identity.PersonaID, outputDigest: output.Digest(), bodyDigest: digestBody(decision.Body, decision.ParentID), parentID: decision.ParentID, audienceRevision: decision.Revision}, nil
}

// IssueAnnouncementDeliveryProof binds a public root body to sealed output.
// Only the separate announcement committer accepts this parentless proof.
func IssueAnnouncementDeliveryProof(ctx context.Context, output agentsecurity.FinalOutputPersistence, floor PersonaAudienceFloor) (PersonaDeliveryProof, error) {
	if ctx == nil || floor == nil || output.Digest() == "" {
		return nil, ErrPersonaProofUnavailable
	}
	i := output.Identity()
	if i.TenantID == "" || i.ConversationID == "" || i.PersonaID == "" || i.AdmissionID == "" || i.InvocationID == "" || i.InvokerID == "" {
		return nil, ErrPersonaProofUnavailable
	}
	if _, _, err := output.Payload(); err != nil {
		return nil, ErrPersonaProofUnavailable
	}
	d, err := floor.AuthorizePersonaOutput(ctx, output)
	if err != nil || d.Revision == 0 || strings.TrimSpace(d.Body) == "" || d.ParentID != "" {
		return nil, ErrPersonaProofUnavailable
	}
	return personaDeliveryProof{tenantID: i.TenantID, conversationID: i.ConversationID, authorID: i.PersonaID, outputDigest: output.Digest(), bodyDigest: digestBody(d.Body, ""), audienceRevision: d.Revision}, nil
}

func digestBody(body, parentID string) string {
	sum := sha256.Sum256([]byte(body + "\x00" + parentID))
	return "sha256:" + hex.EncodeToString(sum[:])
}
