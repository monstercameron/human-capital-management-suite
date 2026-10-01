package agentaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
)

// PersonaSkillExecution identifies the versioned skill and plan step whose
// operation is being recorded. The runtime supplies these values from its
// resolved plan and skill registry.
type PersonaSkillExecution struct {
	TaskID       string
	PlanRevision string
	StepID       string
	SkillID      string
	SkillVersion string
}

// PersonaContextEvidence is the content-free context lineage pinned by an
// invocation's audit events.
type PersonaContextEvidence struct {
	PostID       string
	Digest       string
	Taint        string
	AttachmentID string
}

// PersonaAuditTrace is a viewer-authorized reconstruction of one invocation.
// It contains no prompt, peer message, tool arguments, or result payload.
type PersonaAuditTrace struct {
	InvocationID   string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	ConversationID string
	InvokingPostID string
	Events         []string
	Context        []PersonaContextEvidence
}

// ResolvePersonaTrace joins projected audit views that expose the requested
// invocation ID. Callers must query with explicit field access for persona
// identity and context evidence; redacted or inconsistent evidence fails
// closed.
func ResolvePersonaTrace(views []View, invocationID string) (PersonaAuditTrace, error) {
	if strings.TrimSpace(invocationID) == "" {
		return PersonaAuditTrace{}, fmt.Errorf("%w: invocation ID is required", ErrInvalidEntry)
	}
	trace := PersonaAuditTrace{InvocationID: invocationID}
	seenEvents := make(map[string]struct{})
	contextByIndex := make(map[int]PersonaContextEvidence)
	found := false
	for _, view := range views {
		fields := make(map[string]FieldView, len(view.Fields))
		for _, field := range view.Fields {
			fields[field.Name] = field
		}
		invocationField, exists := fields["persona.invocation_id"]
		if !exists || invocationField.Redacted || invocationField.Value != invocationID {
			continue
		}
		found = true
		if err := applyPersonaIdentity(fields, &trace); err != nil {
			return PersonaAuditTrace{}, err
		}
		if _, exists := seenEvents[view.EventID]; !exists {
			trace.Events = append(trace.Events, view.EventID)
			seenEvents[view.EventID] = struct{}{}
		}
		if err := collectPersonaContext(fields, contextByIndex); err != nil {
			return PersonaAuditTrace{}, err
		}
	}
	if !found {
		return PersonaAuditTrace{}, fmt.Errorf("%w: invocation %q not present in authorized views", ErrInvalidEntry, invocationID)
	}
	indices := make([]int, 0, len(contextByIndex))
	for index := range contextByIndex {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		item := contextByIndex[index]
		if item.PostID == "" || !validSHA256Digest(item.Digest) ||
			(item.Taint != agentinvoke.TaintInvokerInstruction && item.Taint != agentinvoke.TaintUntrustedPeer) {
			return PersonaAuditTrace{}, fmt.Errorf("%w: persona context evidence is incomplete", ErrChainTampered)
		}
		trace.Context = append(trace.Context, item)
	}
	sort.Strings(trace.Events)
	return trace, nil
}

func applyPersonaIdentity(fields map[string]FieldView, trace *PersonaAuditTrace) error {
	for name, target := range map[string]*string{
		"persona.id":               &trace.PersonaID,
		"persona.version":          &trace.PersonaVersion,
		"persona.installation_id":  &trace.InstallationID,
		"persona.conversation_id":  &trace.ConversationID,
		"persona.invoking_post_id": &trace.InvokingPostID,
	} {
		field, ok := fields[name]
		if !ok || field.Redacted || field.Value == "" {
			return fmt.Errorf("%w: trace field %s is unavailable", ErrUnauthorized, name)
		}
		if *target != "" && *target != field.Value {
			return fmt.Errorf("%w: invocation identity changed across events", ErrChainTampered)
		}
		*target = field.Value
	}
	return nil
}

func collectPersonaContext(fields map[string]FieldView, evidence map[int]PersonaContextEvidence) error {
	for name, field := range fields {
		if !strings.HasPrefix(name, "persona.context.") {
			continue
		}
		if field.Redacted {
			return fmt.Errorf("%w: persona context evidence is redacted", ErrUnauthorized)
		}
		separator := strings.Index(name[len("persona.context."):], ".")
		if separator < 0 {
			return fmt.Errorf("%w: malformed persona context field", ErrChainTampered)
		}
		stem := name[len("persona.context.") : len("persona.context.")+separator]
		index, err := strconv.Atoi(stem)
		if err != nil || index < 0 {
			return fmt.Errorf("%w: malformed persona context index", ErrChainTampered)
		}
		suffix := name[len("persona.context.")+separator+1:]
		item := evidence[index]
		if prior := personaContextValue(item, suffix); prior != "" && prior != field.Value {
			return fmt.Errorf("%w: context evidence changed across events", ErrChainTampered)
		}
		switch suffix {
		case "post_id":
			item.PostID = field.Value
		case "digest":
			item.Digest = field.Value
		case "taint":
			item.Taint = field.Value
		case "attachment_id":
			item.AttachmentID = field.Value
		default:
			return fmt.Errorf("%w: unknown persona context field %q", ErrChainTampered, suffix)
		}
		evidence[index] = item
	}
	return nil
}

func personaContextValue(item PersonaContextEvidence, suffix string) string {
	switch suffix {
	case "post_id":
		return item.PostID
	case "digest":
		return item.Digest
	case "taint":
		return item.Taint
	case "attachment_id":
		return item.AttachmentID
	default:
		return ""
	}
}

// NewPersonaAuditEntry binds one persona skill operation to the server-owned
// invocation and bounded context. Only post IDs, content digests, and taint
// labels enter the audit entry; prompt and peer content are deliberately
// excluded. The resulting entry uses the existing immutable hash-chain store.
func NewPersonaAuditEntry(eventID, tenantID, action, argumentsDigest, resultDigest string, execution PersonaSkillExecution, request agentinvoke.RunRequest, occurredAt time.Time) (Entry, error) {
	if err := validatePersonaExecution(execution); err != nil {
		return Entry{}, err
	}
	if err := validatePersonaRun(request); err != nil {
		return Entry{}, err
	}
	if !hasPersonaSkill(request.Skills, execution.SkillID) {
		return Entry{}, fmt.Errorf("%w: skill %q was not admitted for the invocation", ErrInvalidEntry, execution.SkillID)
	}
	if !hasPersonaSkill(request.Grant.Skills, execution.SkillID) {
		return Entry{}, fmt.Errorf("%w: skill %q was not present in the invocation grant", ErrInvalidEntry, execution.SkillID)
	}
	if err := validatePersonaContext(request); err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(tenantID) == "" || tenantID != request.TenantID {
		return Entry{}, fmt.Errorf("%w: audit tenant does not match the persona invocation", ErrTenantBoundary)
	}
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(argumentsDigest) == "" || strings.TrimSpace(resultDigest) == "" || occurredAt.IsZero() {
		return Entry{}, fmt.Errorf("%w: persona audit event identity, digests, action, and time are required", ErrInvalidEntry)
	}

	chain := request.Actor
	actor := personaAuditActor(chain, execution, request.Grant.ID)
	fields := personaAuditFields(chain, execution, request)
	entry := Entry{
		EventID: eventID, TenantID: tenantID, Kind: EventSkillCall, Actor: actor,
		Action: action, ArgumentsDigest: argumentsDigest, ResultDigest: resultDigest,
		Fields: fields,
		Edges: []Edge{
			{Kind: EdgeTask, From: execution.TaskID, To: eventID},
			{Kind: EdgeCaused, From: eventID, To: chain.InvocationID},
		},
		OccurredAt: occurredAt,
	}
	if err := entry.validate(); err != nil {
		return Entry{}, fmt.Errorf("%w: %v", ErrInvalidEntry, err)
	}
	return entry, nil
}

func personaAuditActor(chain agentinvoke.ActorChain, execution PersonaSkillExecution, grantID string) ActorChain {
	return ActorChain{
		UserID: chain.UserID, AgentVersion: chain.PersonaVersion, InstallationID: chain.InstallationID,
		TaskID: execution.TaskID, PlanRevision: execution.PlanRevision, StepID: execution.StepID,
		DelegationGrantID: grantID,
	}
}

func personaAuditFields(chain agentinvoke.ActorChain, execution PersonaSkillExecution, request agentinvoke.RunRequest) []Field {
	fields := personaIdentityFields(chain)
	fields = append(fields,
		Field{Name: "persona.step_id", Value: execution.StepID, Classification: ClassificationConfidential},
		Field{Name: "persona.skill_id", Value: execution.SkillID, Classification: ClassificationConfidential},
		Field{Name: "persona.skill_version", Value: execution.SkillVersion, Classification: ClassificationConfidential},
		Field{Name: "persona.skill_set_digest", Value: digestSkillScopes(request.Skills), Classification: ClassificationConfidential},
	)
	for index, contextEntry := range request.Context.Entries {
		prefix := fmt.Sprintf("persona.context.%06d.", index)
		fields = append(fields,
			Field{Name: prefix + "post_id", Value: contextEntry.PostID, Classification: ClassificationConfidential},
			Field{Name: prefix + "digest", Value: contextEntry.Digest, Classification: ClassificationConfidential},
			Field{Name: prefix + "taint", Value: contextEntry.Taint, Classification: ClassificationConfidential},
		)
		if contextEntry.Attachment {
			fields = append(fields, Field{Name: prefix + "attachment_id", Value: contextEntry.AttachmentID, Classification: ClassificationConfidential})
		}
	}
	return fields
}

func validatePersonaExecution(execution PersonaSkillExecution) error {
	for name, value := range map[string]string{
		"task_id": execution.TaskID, "plan_revision": execution.PlanRevision,
		"step_id": execution.StepID, "skill_id": execution.SkillID,
		"skill_version": execution.SkillVersion,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: persona execution %s is required and canonical", ErrInvalidEntry, name)
		}
	}
	return nil
}

func validatePersonaRun(request agentinvoke.RunRequest) error {
	if err := request.Actor.Validate(); err != nil {
		return fmt.Errorf("%w: invalid persona actor: %v", ErrInvalidEntry, err)
	}
	chain := request.Actor
	if request.Mode != agentinvoke.OnBehalfOf || request.InvocationID != chain.InvocationID ||
		request.TenantID == "" || request.InvokerID != chain.UserID ||
		request.PersonaID != chain.PersonaID || request.PersonaVersion != chain.PersonaVersion ||
		request.InstallationID != chain.InstallationID || request.ConversationID != chain.ConversationID ||
		request.InvokingPostID != chain.InvokingPostID || request.Grant.ID == "" ||
		request.Grant.UserID != request.InvokerID || request.Grant.TenantID != request.TenantID {
		return fmt.Errorf("%w: persona invocation and grant do not agree with the actor chain", ErrInvalidEntry)
	}
	return nil
}

func validatePersonaContext(request agentinvoke.RunRequest) error {
	if len(request.Context.Entries) == 0 {
		return fmt.Errorf("%w: persona audit requires context post evidence", ErrInvalidEntry)
	}
	foundInvoker := false
	for _, entry := range request.Context.Entries {
		if strings.TrimSpace(entry.PostID) == "" || !validSHA256Digest(entry.Digest) {
			return fmt.Errorf("%w: persona context requires a post ID and sha256 digest", ErrInvalidEntry)
		}
		switch entry.Taint {
		case agentinvoke.TaintInvokerInstruction:
			if entry.PostID != request.InvokingPostID || entry.AuthorID != request.InvokerID || entry.Attachment {
				return fmt.Errorf("%w: invoker instruction does not match the invoking post", ErrInvalidEntry)
			}
			foundInvoker = true
		case agentinvoke.TaintUntrustedPeer:
			if entry.PostID == request.InvokingPostID && entry.AuthorID == request.InvokerID && !entry.Attachment {
				return fmt.Errorf("%w: invoking post is tainted as an untrusted peer", ErrInvalidEntry)
			}
		default:
			return fmt.Errorf("%w: unknown persona context taint %q", ErrInvalidEntry, entry.Taint)
		}
		if entry.Attachment && strings.TrimSpace(entry.AttachmentID) == "" {
			return fmt.Errorf("%w: context attachment ID is required", ErrInvalidEntry)
		}
	}
	if !foundInvoker {
		return fmt.Errorf("%w: invoking post is absent from persona context evidence", ErrInvalidEntry)
	}
	return nil
}

func hasPersonaSkill(skills agentinvoke.SkillScopes, skillID string) bool {
	scopes, ok := skills[skillID]
	if !ok || len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == "" || strings.TrimSpace(scope) != scope {
			return false
		}
	}
	return true
}

func validSHA256Digest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == sha256.Size
}

func personaIdentityFields(chain agentinvoke.ActorChain) []Field {
	return []Field{
		{Name: "persona.id", Value: chain.PersonaID, Classification: ClassificationConfidential},
		{Name: "persona.version", Value: chain.PersonaVersion, Classification: ClassificationConfidential},
		{Name: "persona.installation_id", Value: chain.InstallationID, Classification: ClassificationConfidential},
		{Name: "persona.conversation_id", Value: chain.ConversationID, Classification: ClassificationConfidential},
		{Name: "persona.invoking_post_id", Value: chain.InvokingPostID, Classification: ClassificationConfidential},
		{Name: "persona.invocation_id", Value: chain.InvocationID, Classification: ClassificationConfidential},
	}
}

func digestSkillScopes(skills agentinvoke.SkillScopes) string {
	keys := make([]string, 0, len(skills))
	for skill := range skills {
		keys = append(keys, skill)
	}
	sort.Strings(keys)
	var material strings.Builder
	for _, skill := range keys {
		writePersonaDigestPart(&material, skill)
		for _, scope := range sortedPersonaScopes(skills[skill]) {
			writePersonaDigestPart(&material, scope)
		}
	}
	sum := sha256.Sum256([]byte(material.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writePersonaDigestPart(material *strings.Builder, value string) {
	material.WriteString(strconv.Itoa(len(value)))
	material.WriteByte(':')
	material.WriteString(value)
}

func sortedPersonaScopes(scopes []string) []string {
	copyOfScopes := append([]string(nil), scopes...)
	sort.Strings(copyOfScopes)
	return copyOfScopes
}
