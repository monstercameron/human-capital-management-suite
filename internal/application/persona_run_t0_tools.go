package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
)

var errPersonaRunT0Tool = errors.New("application: persona T0 tool execution denied")

const personaDocumentSearchTool = "documents_search"
const personaPolicyHelperSkillID = "hcmnext.skill.knowledge_search_with_citations"
const personaDocumentSearchCapabilityID = "hcmnext.agent.document_search"
const personaDocumentSearchCapabilityVersion = uint32(1)

var personaDocumentSearchSchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500},"team_id":{"type":"string"},"channel_id":{"type":"string"}},"required":["query"],"additionalProperties":false}`)

// PersonaRunT0ToolInvocation is reconstructed from a durable accepted AgentRun
// record. It is never accepted from model arguments or prompt content.
type PersonaRunT0ToolInvocation struct {
	TenantID       string
	ConversationID string
	ThreadID       string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	InvocationID   string
	InvokerID      string
	AgentID        string
}

// PersonaRunT0ToolPolicy resolves the exact current persona pin and current
// per-call AGENT2-005 grant for the canonical skill ID. Implementations must
// read the immutable persona version and grant store; they may not infer either
// from a model proposal.
type PersonaRunT0ToolPolicy interface {
	ResolveAndAuthorize(context.Context, agentrun.Record, runstate.Run, PersonaRunT0ToolInvocation, string) (PersonaT0SkillPin, error)
}

// PersonaRunT0DocumentSearcher is the existing application document-search
// service. Its implementation rechecks the tenant:conversation installation
// and the invoker's document grants on every call.
type PersonaRunT0CapabilityGateway interface {
	Invoke(context.Context, capability.InvokeRequest) (capability.InvokeResult, error)
}

// PersonaRunT0DocumentSearcher is the HUB-031 service port consumed only by
// the registered capability handler; persona workers invoke that handler
// through PersonaRunT0CapabilityGateway, never through this service directly.
type PersonaRunT0DocumentSearcher interface {
	AgentSearchDocuments(context.Context, string, string, string, string, string, transportdocument.SearchFilters) (transportdocument.SearchResult, error)
}

// PersonaRunT0ToolResultJournal durably stores the bounded output before the
// run resolves its tool effect. Implementations must bind the row to all
// invocation identity fields and enforce tenant isolation.
type PersonaRunT0ToolResultJournal interface {
	PersistPersonaRunT0ToolResult(context.Context, PersonaRunT0ToolInvocation, string, string, []byte) (string, string, error)
}

// PersonaRunT0ToolExecutor exposes only the reviewed T0 tools for one durable
// invocation. Tool grants and installation authority are resolved at each
// schema projection and again immediately before execution.
type PersonaRunT0ToolExecutor struct {
	policy  PersonaRunT0ToolPolicy
	t0      *PersonaT0SkillPolicy
	gateway PersonaRunT0CapabilityGateway
	journal PersonaRunT0ToolResultJournal
}

// NewPersonaRunT0ToolExecutor requires current policy, document installation,
// requester authorization and durable result-journal ports.
func NewPersonaRunT0ToolExecutor(policy PersonaRunT0ToolPolicy, t0 *PersonaT0SkillPolicy, gateway PersonaRunT0CapabilityGateway, journal PersonaRunT0ToolResultJournal) (*PersonaRunT0ToolExecutor, error) {
	if policy == nil || t0 == nil || gateway == nil || journal == nil {
		return nil, errPersonaRunT0Tool
	}
	return &PersonaRunT0ToolExecutor{policy: policy, t0: t0, gateway: gateway, journal: journal}, nil
}

// ToolSchemas returns the single Policy Helper document-search projection
// only when the durable persona pin, current grant and T0 operation agree.
func (e *PersonaRunT0ToolExecutor) ToolSchemas(ctx context.Context, admission agentrun.Record, run runstate.Run) ([]agentmodel.ToolSchema, error) {
	if e == nil || e.policy == nil || e.t0 == nil || ctx == nil {
		return nil, errPersonaRunT0Tool
	}
	invocation, err := personaRunT0ToolInvocation(admission, run)
	if err != nil {
		return nil, err
	}
	if _, ok := personaRunChatPrincipal(ctx, invocation.TenantID, invocation.InvokerID); !ok {
		return nil, errPersonaRunT0Tool
	}
	if _, err := e.resolvePin(ctx, admission, run, invocation); err != nil {
		return nil, err
	}
	schemas := []agentmodel.ToolSchema{{Name: personaDocumentSearchTool, Description: "Search deployed policy documents the requesting user and this installation may read. Returns document-version citations.", InputSchema: bytes.Clone(personaDocumentSearchSchema)}}
	for _, binding := range e.t0.bindings {
		if binding.Invocation == (PersonaT0Invocation{TenantID: invocation.TenantID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, InvocationID: invocation.InvocationID}) && binding.Pin.ID == personaWorkspaceSearchSkillID {
			if _, err := e.resolveSearchSkillPin(ctx, admission, run, invocation, personaWorkspaceSearchSkillID); err != nil {
				return nil, err
			}
			schemas = append(schemas, workspaceDocumentToolSchema())
			break
		}
	}
	return schemas, nil
}

// Execute validates the one supported proposal, re-resolves the current pin
// and grant, then invokes documentService.AgentSearchDocuments with identity
// taken only from the accepted durable admission.
func (e *PersonaRunT0ToolExecutor) Execute(ctx context.Context, admission agentrun.Record, run runstate.Run, proposal agentmodel.ToolProposal) ([]byte, string, string, error) {
	if e == nil || e.policy == nil || e.t0 == nil || e.gateway == nil || e.journal == nil || ctx == nil || (proposal.Name != personaDocumentSearchTool && proposal.Name != personaWorkspaceSearchTool) || strings.TrimSpace(proposal.ID) == "" {
		return nil, "", "", errPersonaRunT0Tool
	}
	invocation, err := personaRunT0ToolInvocation(admission, run)
	if err != nil {
		return nil, "", "", err
	}
	if _, ok := personaRunChatPrincipal(ctx, invocation.TenantID, invocation.InvokerID); !ok {
		return nil, "", "", errPersonaRunT0Tool
	}
	skill, capabilityID, scope := personaPolicyHelperSkillID, personaDocumentSearchCapabilityID, personaConversationSearchScope
	if proposal.Name == personaWorkspaceSearchTool {
		skill, capabilityID, scope = personaWorkspaceSearchSkillID, personaWorkspaceSearchCapabilityID, personaWorkspaceSearchScope
	}
	if _, err := e.resolveSearchSkillPin(ctx, admission, run, invocation, skill); err != nil {
		return nil, "", "", err
	}
	if scope == personaWorkspaceSearchScope {
		source, ok := e.policy.(PersonaDocumentSearchScopeSource)
		if !ok {
			return nil, "", "", errPersonaRunT0Tool
		}
		ctx = context.WithValue(ctx, personaDocumentSearchContextKey{}, personaDocumentSearchContext{identity: invocation, source: source})
	}
	arguments, err := decodePersonaDocumentSearchArguments(proposal.Arguments)
	if err != nil {
		return nil, "", "", err
	}
	invoked, err := e.gateway.Invoke(ctx, capability.InvokeRequest{
		Capability:    capability.Key{ID: capabilityID, Version: personaDocumentSearchCapabilityVersion},
		Payload:       personaDocumentSearchCall{TenantID: values.TenantId(invocation.TenantID), ConversationID: invocation.ConversationID, AgentID: invocation.AgentID, InvokerID: invocation.InvokerID, Query: arguments.Query, Filters: transportdocument.SearchFilters{TeamID: arguments.TeamID, ChannelID: arguments.ChannelID}, Scope: scope},
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"documents:search"}, SubjectRef: invocation.InvokerID, Tenant: invocation.TenantID},
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: governed document search refused", errPersonaRunT0Tool)
	}
	var result any
	switch value := invoked.Response.(type) {
	case transportdocument.SearchResult:
		result = value
	case PersonaPolicyDocumentSearchResult:
		result = value
	default:
		return nil, "", "", errPersonaRunT0Tool
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, "", "", errPersonaRunT0Tool
	}
	if len(encoded) == 0 || len(encoded) > 64*1024 {
		return nil, "", "", errPersonaRunT0Tool
	}
	ref, digest, err := e.journal.PersistPersonaRunT0ToolResult(ctx, invocation, run.ID+":"+proposal.ID, proposal.ID, encoded)
	if err != nil || strings.TrimSpace(ref) == "" || digest != personaRunT0ToolOutputDigest(encoded) {
		return nil, "", "", errPersonaRunT0Tool
	}
	return encoded, ref, digest, nil
}

func (e *PersonaRunT0ToolExecutor) resolvePin(ctx context.Context, admission agentrun.Record, run runstate.Run, invocation PersonaRunT0ToolInvocation) (PersonaT0SkillPin, error) {
	return e.resolveSearchSkillPin(ctx, admission, run, invocation, personaPolicyHelperSkillID)
}

func (e *PersonaRunT0ToolExecutor) resolveSearchSkillPin(ctx context.Context, admission agentrun.Record, run runstate.Run, invocation PersonaRunT0ToolInvocation, skill string) (PersonaT0SkillPin, error) {
	binding, err := e.policy.ResolveAndAuthorize(ctx, admission, run, invocation, skill)
	if err != nil || binding.Invocation != (PersonaT0Invocation{TenantID: invocation.TenantID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, InvocationID: invocation.InvocationID}) || binding.Pin.ID != skill || binding.Pin.Version == 0 || strings.TrimSpace(binding.Pin.Digest) == "" || len(binding.Scopes) != 1 || binding.Scopes[0] != "documents:search" {
		return PersonaT0SkillPin{}, errPersonaRunT0Tool
	}
	if ok, err := e.t0.IsBoundT0Skill(ctx, binding.Invocation, binding.Pin, binding.Scopes); err != nil || !ok {
		return PersonaT0SkillPin{}, errPersonaRunT0Tool
	}
	if !(skill == personaPolicyHelperSkillID && personaRunT0PinHasDocumentSearchOperation(e.t0.catalog, binding.Pin) || skill == personaWorkspaceSearchSkillID && personaWorkspacePinHasSearchOperation(e.t0.catalog, binding.Pin)) {
		return PersonaT0SkillPin{}, errPersonaRunT0Tool
	}
	return binding, nil
}

func personaRunT0PinHasDocumentSearchOperation(catalog PersonaT0SkillCatalog, pin agentskills.SkillPin) bool {
	if catalog == nil {
		return false
	}
	record, err := catalog.ResolvePin(pin)
	if err != nil || record.Definition.ID != personaPolicyHelperSkillID || record.Definition.Version != pin.Version || record.Digest != pin.Digest || record.Status != agentskills.StatusActive || record.Definition.SideEffectTier != agentskills.TierT0 || record.HighestCapabilityTier > agentskills.TierT0 {
		return false
	}
	if len(record.ResolvedOperations) != 1 {
		return false
	}
	count := 0
	for _, operation := range record.ResolvedOperations {
		key := capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion}
		if operation.HasCapability && operation.Reference.Kind == agentskills.OperationCapability && operation.Reference.Capability == key && operation.Capability.Definition.ID == key.ID && operation.Capability.Definition.Version == key.Version && operation.Capability.Definition.AuthZScopeRef == "documents:search" && operation.Capability.Definition.EffectClass == capability.EffectReadOnly && operation.Capability.Status == capability.StatusActive {
			count++
		}
	}
	return count == 1
}

type personaDocumentSearchCall struct {
	TenantID       values.TenantId
	ConversationID string
	AgentID        string
	InvokerID      string
	Query          string
	Filters        transportdocument.SearchFilters
	Scope          string
}

func personaRunT0ToolInvocation(admission agentrun.Record, run runstate.Run) (PersonaRunT0ToolInvocation, error) {
	req := admission.Request
	identity := PersonaRunT0ToolInvocation{
		TenantID: req.Source.TenantID, ConversationID: req.Audience.ID, ThreadID: req.Context.ID,
		InvocationID: req.Source.Key, InvokerID: req.Principal.InvokerID,
		InstallationID: req.InstallationID, AgentID: req.Agent.AgentID,
	}
	if admission.Decision != agentrun.DecisionAccepted || admission.ID == "" || run.ID != admission.ID || run.AdmissionID != admission.ID || run.RequestDigest != admission.RequestDigest || run.TenantID != identity.TenantID || run.AgentDigest != req.Agent.Digest || req.Source.Kind != agentrun.SourcePersonaMention || req.Source.Key == "" || req.Source.Ref == "" || req.Persona == nil || req.Persona.ID == "" || req.Persona.Version == "" || req.Persona.Digest == "" || req.Principal.Mode != agentrun.ModeOnBehalfOf || req.Principal.InvokerID == "" || req.Principal.SponsorID != "" || req.InstallationID == "" || req.Agent.AgentID == "" || req.Agent.Version == "" || req.Agent.Digest == "" || identity.ConversationID == "" || identity.ThreadID == "" || admission.RequestDigest == "" || admission.Authority.PolicyDigest == "" || admission.Authority.GrantRef == "" || admission.Authority.InstallationID != req.InstallationID || admission.Authority.Principal != req.Principal || admission.Authority.Audience != req.Audience || admission.Authority.Context != req.Context || admission.Authority.Agent != req.Agent {
		return PersonaRunT0ToolInvocation{}, errPersonaRunT0Tool
	}
	identity.PersonaID, identity.PersonaVersion = req.Persona.ID, req.Persona.Version
	if strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.InvokerID) == "" {
		return PersonaRunT0ToolInvocation{}, errPersonaRunT0Tool
	}
	return identity, nil
}

type personaDocumentSearchArguments struct {
	Query     string `json:"query"`
	TeamID    string `json:"team_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

func decodePersonaDocumentSearchArguments(raw []byte) (personaDocumentSearchArguments, error) {
	var args personaDocumentSearchArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 || len(raw) > 4096 || decoder.Decode(&args) != nil {
		return personaDocumentSearchArguments{}, errPersonaRunT0Tool
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return personaDocumentSearchArguments{}, errPersonaRunT0Tool
	}
	// A model pads a query with whitespace or fills both optional filters with
	// guesses often enough that refusing those forms fails real requests. The
	// query is trimmed, and a pair of filters, which cannot both apply, is
	// dropped: the search scope always comes from the durable run identity,
	// never from these arguments, so ignoring a filter can only widen the
	// search to what the invoker may already read in this conversation.
	//
	// The same holds for one filter on its own: a model cannot know a team or
	// channel identifier, so whatever it supplies is a guess ("tenant_default",
	// "policy-channel"), and a guess that differs from the run's conversation
	// refused the whole search. Both are accepted for the schema and ignored.
	args.Query, args.TeamID, args.ChannelID = strings.TrimSpace(args.Query), strings.TrimSpace(args.TeamID), strings.TrimSpace(args.ChannelID)
	if args.Query == "" || len(args.Query) > 500 || len(args.TeamID) > 128 || len(args.ChannelID) > 128 {
		return personaDocumentSearchArguments{}, errPersonaRunT0Tool
	}
	args.TeamID, args.ChannelID = "", ""
	return args, nil
}

func personaRunT0ToolOutputDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
