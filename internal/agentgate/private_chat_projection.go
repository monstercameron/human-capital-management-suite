package agentgate

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	// PrivateChatReplyCapability is the only capability admitted by the
	// private persona reply scope projection.
	PrivateChatReplyCapability = "persona.reply"
	// PrivateChatReplyScope is the narrow delegated scope for the current
	// private conversation and invoking post.
	PrivateChatReplyScope = "chat.current"
)

// PrivateChatScopeRequest binds authorization to one verified invoker and
// the exact private conversation, thread, and invoking post.
type PrivateChatScopeRequest struct {
	User           UserContext
	Skill          agentskills.SkillPin
	Purpose        string
	Tenant         values.TenantId
	ConversationID string
	ThreadID       string
	InvokingPostID string
	At             time.Time
}

// PrivateChatScopeEvidence is the current policy owner's proof for an exact
// chat context. Private-only projection requires PrivateConversation; public
// projection additionally requires explicit admission-policy evidence. It
// contains no record fields or authority over another conversation or post.
type PrivateChatScopeEvidence struct {
	Allowed             bool
	Tenant              values.TenantId
	InvokerID           string
	ConversationID      string
	ThreadID            string
	InvokingPostID      string
	InvokingPostAuthor  string
	PrivateConversation bool
	PublicConversation  bool
	PublicPolicyRev     uint64
	ActiveMember        bool
	PostVisible         bool
	ConversationRev     uint64
	MembershipRev       uint64
	PostDigest          string
	EvaluatedAt         time.Time
}

// PrivateChatScopeAuthorizer is implemented by the current chat policy owner.
// Implementations must reread current membership and the invoking post; a
// cached installation or request-supplied visibility flag is not authority.
type PrivateChatScopeAuthorizer interface {
	AuthorizePrivateChat(context.Context, PrivateChatScopeRequest) (PrivateChatScopeEvidence, error)
}

// PrivateChatSkillAuthorization is a per-invocation capability projection
// for one T0 private persona reply. Its resource binding is the exact chat
// tuple; it intentionally has no workforce subjects or fields.
type PrivateChatSkillAuthorization struct {
	Skill       agentskills.SkillRecord
	Grant       SkillGrant
	Capability  capability.Key
	Scope       string
	Evidence    PrivateChatScopeEvidence
	EvaluatedAt time.Time
	Purpose     string
}

// ProjectPrivateChatSkillAuthorization resolves the current administrator
// skill grant and requires a separate current-chat policy decision. This is
// not a general record capability PDP path: the private-chat authorizer must
// prove active invoker membership and visibility of the exact invoking post.
func (g *Gate) ProjectPrivateChatSkillAuthorization(ctx context.Context, req PrivateChatScopeRequest, chat PrivateChatScopeAuthorizer) (PrivateChatSkillAuthorization, error) {
	return g.projectPrivateChatScope(ctx, req, chat, privateReplyCapability, PrivateChatReplyScope)
}

// ProjectPrivateChatDocumentSearchAuthorization admits only the pinned policy
// search capability within the exact current private chat context. This grants
// search, not document reads: the document owner must still filter every hit by
// the invoker's current ACL and official conversation placement at execution.
func (g *Gate) ProjectPrivateChatDocumentSearchAuthorization(ctx context.Context, req PrivateChatScopeRequest, chat PrivateChatScopeAuthorizer) (PrivateChatSkillAuthorization, error) {
	return g.projectPrivateChatScope(ctx, req, chat, privatePolicySearchCapability, "documents:search")
}

func (g *Gate) projectPrivateChatScope(ctx context.Context, req PrivateChatScopeRequest, chat PrivateChatScopeAuthorizer, resolveCapability func(agentskills.SkillRecord) (capability.Key, bool), scope string) (PrivateChatSkillAuthorization, error) {
	var authorize func(context.Context, PrivateChatScopeRequest) (PrivateChatScopeEvidence, error)
	if chat != nil {
		authorize = chat.AuthorizePrivateChat
	}
	return g.projectChatScope(ctx, req, authorize, privateChatEvidenceMatches, resolveCapability, scope)
}

func (g *Gate) projectChatScope(ctx context.Context, req PrivateChatScopeRequest, authorize func(context.Context, PrivateChatScopeRequest) (PrivateChatScopeEvidence, error), matches func(PrivateChatScopeEvidence, PrivateChatScopeRequest) bool, resolveCapability func(agentskills.SkillRecord) (capability.Key, bool), scope string) (PrivateChatSkillAuthorization, error) {
	key := req.Skill.Key()
	deny := func(code DenialCode, detail string) (PrivateChatSkillAuthorization, error) {
		return PrivateChatSkillAuthorization{}, &DeniedError{Code: code, Skill: key, Detail: detail}
	}
	if g == nil || authorize == nil || ctx == nil || req.Skill.ID == "" || req.Skill.Version == 0 || req.Skill.Digest == "" || req.Tenant.Validate() != nil || req.Purpose == "" || !cleanPrivateChatID(req.ConversationID) || !cleanPrivateChatID(req.ThreadID) || !cleanPrivateChatID(req.InvokingPostID) {
		return deny(DenyInvalid, "exact chat bindings and policy owner are required")
	}
	if err := contextErr(ctx); err != nil {
		return deny(DenyInvalid, "request context is unavailable")
	}
	at, err := g.resolveTime(req.At)
	if err != nil {
		return deny(DenyInvalid, "effective time is unavailable")
	}
	if req.User.Principal == nil || req.User.Principal.Tenant() != req.Tenant || req.User.Principal.SubjectKind() != trust.SubjectKindHuman || !req.User.Principal.AuthorizesPurpose(req.Purpose) || !at.Before(req.User.Principal.ExpiresAt()) || at.Before(req.User.Principal.IssuedAt()) {
		return deny(DenyAgent, "verified human invoker is not current for this purpose and tenant")
	}
	if err := validateUser(req.User, req.Purpose); err != nil {
		return deny(DenyAgent, "current invoker context is incomplete")
	}
	record, err := g.skills.ResolvePin(req.Skill)
	if err != nil || record.Definition.Key() != key || record.Status != agentskills.StatusActive || record.Digest != req.Skill.Digest {
		return deny(DenySkillNotFound, "exact active reply skill pin is unavailable")
	}
	if !currentSkillRecord(g.skills.List(), record) {
		return deny(DenySkillNotFound, "reply skill is absent or ambiguous in the current catalog")
	}
	if !skillAllowsPurpose(record.Definition.RequiredPurposes, req.Purpose) || record.Definition.SideEffectTier != agentskills.TierT0 {
		return deny(DenyPurpose, "reply skill is not available as a T0 skill for this purpose")
	}
	capKey, ok := resolveCapability(record)
	if !ok {
		return deny(DenyCapability, "skill does not resolve only to the exact chat domain capability")
	}
	grant, err := g.matchGrant(ctx, req.User, key, req.Purpose, at, nil)
	if err != nil {
		return PrivateChatSkillAuthorization{}, err
	}
	policyReq := req
	policyReq.At = at
	evidence, err := authorize(ctx, policyReq)
	if err != nil || !matches(evidence, policyReq) {
		return deny(DenySubject, "current membership or invoking-post policy denied this scope")
	}
	return PrivateChatSkillAuthorization{Skill: record, Grant: cloneGrant(grant), Capability: capKey, Scope: scope, Evidence: evidence, EvaluatedAt: at, Purpose: req.Purpose}, nil
}

func privatePolicySearchCapability(record agentskills.SkillRecord) (capability.Key, bool) {
	if record.Definition.ID != "hcmnext.skill.knowledge_search_with_citations" || len(record.ResolvedOperations) != 1 {
		return capability.Key{}, false
	}
	op := record.ResolvedOperations[0]
	d := op.Capability.Definition
	if !op.HasCapability || d.ID != "hcmnext.agent.document_search" || d.Version != 1 || d.OwnerDomain != "documents" ||
		d.AuthZScopeRef != "documents:search" || d.EffectClass != capability.EffectReadOnly || !d.AgentEligible || op.Capability.Status != capability.StatusActive ||
		len(d.ReadData.DataDomains) != 1 || d.ReadData.DataDomains[0] != "policy_document" || len(d.ReadData.FieldPaths) != 0 ||
		len(d.WriteData.DataDomains) != 0 || len(d.WriteData.FieldPaths) != 0 {
		return capability.Key{}, false
	}
	return d.Key(), true
}

func privateReplyCapability(record agentskills.SkillRecord) (capability.Key, bool) {
	if len(record.ResolvedOperations) != 1 {
		return capability.Key{}, false
	}
	operation := record.ResolvedOperations[0]
	if !operation.HasCapability {
		return capability.Key{}, false
	}
	definition := operation.Capability.Definition
	if definition.ID != PrivateChatReplyCapability || definition.Version != 1 || definition.AuthZScopeRef != PrivateChatReplyScope || definition.EffectClass != capability.EffectPure || !definition.AgentEligible || operation.Capability.Status != capability.StatusActive {
		return capability.Key{}, false
	}
	return definition.Key(), true
}

func currentSkillRecord(records []agentskills.SkillRecord, candidate agentskills.SkillRecord) bool {
	count := 0
	for _, record := range records {
		if record.Definition.Key() == candidate.Definition.Key() && record.Digest == candidate.Digest && record.Status == candidate.Status {
			count++
		}
	}
	return count == 1
}

func privateChatEvidenceMatches(e PrivateChatScopeEvidence, req PrivateChatScopeRequest) bool {
	return e.PrivateConversation && !e.PublicConversation && chatEvidenceMatches(e, req)
}

func chatEvidenceMatches(e PrivateChatScopeEvidence, req PrivateChatScopeRequest) bool {
	return e.Allowed && e.Tenant == req.Tenant && e.InvokerID == req.User.Principal.Subject() && e.ConversationID == req.ConversationID && e.ThreadID == req.ThreadID && e.InvokingPostID == req.InvokingPostID && e.InvokingPostAuthor == req.User.Principal.Subject() && e.ActiveMember && e.PostVisible && e.ConversationRev > 0 && e.MembershipRev > 0 && validDigest(e.PostDigest) && e.EvaluatedAt.Equal(req.At)
}

func cleanPrivateChatID(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
