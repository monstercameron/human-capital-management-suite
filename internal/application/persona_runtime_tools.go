package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var errPersonaRuntimeTools = errors.New("application: current persona runtime tool denied")

func personaRuntimeToolAdmissionDigest(value string) string {
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

// PersonaRuntimeToolJournal stores locally executed results and reads them in
// the tenant/run scope needed by the independent output authority.
type PersonaRuntimeToolJournal interface {
	PutPersonaRuntimeToolResult(context.Context, agentpersonastore.ToolResultRecord) error
	ListPersonaRuntimeToolResults(context.Context, values.TenantId, string) ([]agentpersonastore.ToolResultRecord, error)
}

// PersonaRuntimeToolSourceValidator is the continuation-egress boundary. Its
// caller separately verifies the current accepted run identity, then this
// owner rechecks the locally executed source before any retained bytes leave.
type PersonaRuntimeToolSourceValidator interface {
	RecheckPersonaRuntimeToolResult(context.Context, agentpersonastore.ToolResultRecord) error
}

type DatabasePersonaRuntimeToolJournal struct{ Store *agentpersonastore.Store }

func (j DatabasePersonaRuntimeToolJournal) PutPersonaRuntimeToolResult(ctx context.Context, r agentpersonastore.ToolResultRecord) error {
	if j.Store == nil {
		return errPersonaRuntimeTools
	}
	s, err := j.Store.ForTenant(ctx, r.TenantID)
	if err != nil {
		return err
	}
	return s.PutToolResult(ctx, r)
}
func (j DatabasePersonaRuntimeToolJournal) ListPersonaRuntimeToolResults(ctx context.Context, tenant values.TenantId, runID string) ([]agentpersonastore.ToolResultRecord, error) {
	if j.Store == nil {
		return nil, errPersonaRuntimeTools
	}
	s, err := j.Store.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return s.ListToolResults(ctx, runID)
}

type PersonaRuntimeToolsConfig struct {
	Authority     agentrun.Authority
	Policy        *DatabasePersonaT0SkillPolicy
	Pins          PersonaRuntimeToolPinAuthority
	Catalog       PersonaT0SkillCatalog
	DocumentScope PersonaDocumentSearchScopeSource
	Gateway       PersonaRunT0CapabilityGateway
	Journal       PersonaRuntimeToolJournal
	Documents     PersonaRuntimeDocumentVersionSource
	Now           func() time.Time
}

// PersonaRuntimeTools projects only the currently authorized immutable skill
// pin with a registered local execution adapter. Each call rechecks admission,
// the dynamic T0 grant policy and the live capability registry.
type PersonaRuntimeTools struct{ cfg PersonaRuntimeToolsConfig }

func NewPersonaRuntimeTools(cfg PersonaRuntimeToolsConfig) (*PersonaRuntimeTools, error) {
	if isNilPersonaOutputPort(cfg.Authority) || (cfg.Policy == nil && (isNilPersonaOutputPort(cfg.Pins) || isNilPersonaOutputPort(cfg.Catalog) || isNilPersonaOutputPort(cfg.DocumentScope))) || (cfg.Policy != nil && !isNilPersonaOutputPort(cfg.Pins)) || isNilPersonaOutputPort(cfg.Gateway) || isNilPersonaOutputPort(cfg.Journal) || isNilPersonaOutputPort(cfg.Documents) || cfg.Now == nil {
		return nil, errPersonaRuntimeTools
	}
	if cfg.Policy != nil {
		cfg.Catalog = cfg.Policy.catalog
		cfg.DocumentScope = cfg.Policy
	}
	return &PersonaRuntimeTools{cfg: cfg}, nil
}

func (e *PersonaRuntimeTools) authorize(ctx context.Context, record agentrun.Record, run runstate.Run) (PersonaRunT0ToolInvocation, agentskills.SkillPin, error) {
	identity, err := personaRunT0ToolInvocation(record, run)
	if err != nil || e == nil || ctx == nil {
		return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
	}
	current, err := e.cfg.Authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
	}
	if e.cfg.Policy == nil {
		pins, err := e.cfg.Pins.ResolvePersonaRuntimeToolPins(ctx, record, run)
		if err != nil {
			return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
		}
		for _, pin := range pins {
			if pin.ID == personaPolicyHelperSkillID {
				if !personaRunT0PinHasDocumentSearchOperation(e.cfg.Catalog, pin) {
					return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
				}
				return identity, pin, nil
			}
		}
		return identity, agentskills.SkillPin{}, nil
	}
	request := personaRunInvocation(record.Request)
	store, err := e.cfg.Policy.grantStores.ForTenant(ctx, values.TenantId(identity.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
	}
	grant, err := store.Get(record.Request.Principal.DelegatedCredentialRef)
	if err != nil {
		return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
	}
	bindForegroundGrant(&request, grant)
	allowed, err := e.cfg.Policy.IsBoundT0Run(ctx, request)
	if err != nil || !allowed {
		return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
	}
	pins, digest, _, err := e.cfg.Policy.currentPins(ctx, request)
	if err != nil || digest != record.Request.Persona.Digest {
		return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
	}
	for _, pin := range pins {
		if pin.ID == personaPolicyHelperSkillID && len(request.Skills[pin.ID]) > 0 {
			if !sameScopes(request.Skills[pin.ID], []string{personaPolicySearchScope}) || !personaRunT0PinHasDocumentSearchOperation(e.cfg.Policy.catalog, pin) {
				return identity, agentskills.SkillPin{}, errPersonaRuntimeTools
			}
			return identity, pin, nil
		}
	}
	return identity, agentskills.SkillPin{}, nil
}

func (e *PersonaRuntimeTools) ToolSchemas(ctx context.Context, record agentrun.Record, run runstate.Run) ([]agentmodel.ToolSchema, error) {
	_, pin, err := e.authorize(ctx, record, run)
	if err != nil {
		return nil, err
	}
	if pin.ID == "" {
		return nil, nil
	}
	return []agentmodel.ToolSchema{{Name: personaDocumentSearchTool, Description: "Search official policy document placements in this conversation readable by the invoker and current installation. Returns deployed policy text with exact version, placement and content-digest citations.", InputSchema: bytes.Clone(personaDocumentSearchSchema)}}, nil
}

func (e *PersonaRuntimeTools) search(ctx context.Context, identity PersonaRunT0ToolInvocation, arguments []byte) ([]byte, error) {
	ctx = context.WithValue(ctx, personaDocumentSearchContextKey{}, personaDocumentSearchContext{identity: identity, source: e.cfg.DocumentScope})
	args, err := decodePersonaDocumentSearchArguments(arguments)
	if err != nil {
		return nil, errPersonaRuntimeTools
	}
	response, err := e.cfg.Gateway.Invoke(ctx, capability.InvokeRequest{Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion}, Payload: personaDocumentSearchCall{TenantID: values.TenantId(identity.TenantID), ConversationID: identity.ConversationID, AgentID: identity.AgentID, InvokerID: identity.InvokerID, Query: args.Query, Filters: transportdocument.SearchFilters{TeamID: args.TeamID, ChannelID: args.ChannelID}}, Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{personaPolicySearchScope}, SubjectRef: identity.InvokerID, Tenant: identity.TenantID}})
	if err != nil {
		return nil, errPersonaRuntimeTools
	}
	var result any
	switch value := response.Response.(type) {
	case transportdocument.SearchResult:
		result = value
	case PersonaPolicyDocumentSearchResult:
		result = value
	default:
		return nil, errPersonaRuntimeTools
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) == 0 || len(encoded) > 64*1024 {
		return nil, errPersonaRuntimeTools
	}
	return encoded, nil
}

func (e *PersonaRuntimeTools) Execute(ctx context.Context, record agentrun.Record, run runstate.Run, proposal agentmodel.ToolProposal) ([]byte, string, string, error) {
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	if proposal.Name != personaDocumentSearchTool || proposal.ID == "" || len(proposal.ID) > 512 || strings.TrimSpace(proposal.ID) != proposal.ID {
		return nil, "", "", errPersonaRuntimeTools
	}
	identity, pin, err := e.authorize(ctx, record, run)
	if err != nil || pin.ID == "" {
		return nil, "", "", errPersonaRuntimeTools
	}
	encoded, err := e.search(ctx, identity, proposal.Arguments)
	if err != nil {
		return nil, "", "", err
	}
	digest := personaRunT0ToolOutputDigest(encoded)
	class, err := e.classify(ctx, identity, encoded)
	if err != nil {
		return nil, "", "", err
	}
	r := agentpersonastore.ToolResultRecord{TenantID: values.TenantId(identity.TenantID), RunID: run.ID, AdmissionDigest: personaRuntimeToolAdmissionDigest(record.RequestDigest), InvocationID: identity.InvocationID, InvokerID: identity.InvokerID, ConversationID: identity.ConversationID, ThreadID: identity.ThreadID, PersonaID: identity.PersonaID, PersonaVersion: identity.PersonaVersion, InstallationID: identity.InstallationID, AgentID: identity.AgentID, ToolCallID: proposal.ID, ToolName: proposal.Name, SkillID: pin.ID, SkillVersion: pin.Version, SkillDigest: pin.Digest, Arguments: bytes.Clone(proposal.Arguments), Output: encoded, OutputDigest: digest, CreatedAt: e.cfg.Now().UTC()}
	r.DataClass = class
	if err := e.cfg.Journal.PutPersonaRuntimeToolResult(ctx, r); err != nil {
		return nil, "", "", errPersonaRuntimeTools
	}
	return encoded, "persona-tool-result:" + run.ID + ":" + proposal.ID, digest, nil
}

func (e *PersonaRuntimeTools) classify(ctx context.Context, identity PersonaRunT0ToolInvocation, encoded []byte) (trustdlp.DataClass, error) {
	var result transportdocument.SearchResult
	if json.Unmarshal(encoded, &result) != nil {
		return "", errPersonaRuntimeTools
	}
	// An empty search contains only internal execution metadata and counts.
	class := trustdlp.ClassInternal
	for i, hit := range result.Hits {
		resolved, err := e.cfg.Documents.PersonaRuntimeDocumentClass(ctx, identity, hit.DocumentID, hit.VersionID)
		if err != nil || !resolved.Valid() {
			return "", errPersonaRuntimeTools
		}
		if i > 0 && resolved != class {
			return "", errPersonaRuntimeTools
		}
		class = resolved
	}
	return class, nil
}

func (e *PersonaRuntimeTools) RecheckPersonaRuntimeToolResult(ctx context.Context, r agentpersonastore.ToolResultRecord) error {
	if e == nil || ctx == nil || r.ToolName != personaDocumentSearchTool || r.TenantID.Validate() != nil || r.ToolCallID == "" || r.InvokerID == "" || r.ConversationID == "" || r.AgentID == "" || r.OutputDigest != personaRunT0ToolOutputDigest(r.Output) || !r.DataClass.Valid() {
		return errPersonaRuntimeTools
	}
	pin := agentskills.SkillPin{ID: r.SkillID, Version: r.SkillVersion, Digest: r.SkillDigest}
	if !personaRunT0PinHasDocumentSearchOperation(e.cfg.Catalog, pin) {
		return errPersonaRuntimeTools
	}
	identity := PersonaRunT0ToolInvocation{TenantID: r.TenantID.String(), ConversationID: r.ConversationID, ThreadID: r.ThreadID, PersonaID: r.PersonaID, PersonaVersion: r.PersonaVersion, InstallationID: r.InstallationID, InvocationID: r.InvocationID, InvokerID: r.InvokerID, AgentID: r.AgentID}
	current, err := e.search(ctx, identity, r.Arguments)
	if err != nil || !bytes.Equal(current, r.Output) {
		return errPersonaRuntimeTools
	}
	class, err := e.classify(ctx, identity, current)
	if err != nil || class != r.DataClass {
		return errPersonaRuntimeTools
	}
	return nil
}

// ReadPersonaRunToolGrounding rechecks current tool authority and performs the
// exact governed search again. Changed document visibility or deployment
// invalidates the retained evidence instead of substituting newer facts.
func (e *PersonaRuntimeTools) ReadPersonaRunToolGrounding(ctx context.Context, record agentrun.Record, run runstate.Run, gateway *agentsecurity.ToolGateway) ([]agentsecurity.Datum, error) {
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	if gateway == nil {
		return nil, errPersonaRuntimeTools
	}
	identity, pin, err := e.authorize(ctx, record, run)
	if err != nil {
		return nil, err
	}
	rows, err := e.cfg.Journal.ListPersonaRuntimeToolResults(ctx, values.TenantId(identity.TenantID), run.ID)
	if err != nil {
		return nil, err
	}
	var grounding []agentsecurity.Datum
	for _, r := range rows {
		if r.TenantID.String() != identity.TenantID || r.RunID != run.ID || r.AdmissionDigest != personaRuntimeToolAdmissionDigest(record.RequestDigest) || r.InvocationID != identity.InvocationID || r.InvokerID != identity.InvokerID || r.ConversationID != identity.ConversationID || r.ThreadID != identity.ThreadID || r.PersonaID != identity.PersonaID || r.PersonaVersion != identity.PersonaVersion || r.InstallationID != identity.InstallationID || r.AgentID != identity.AgentID || r.ToolName != personaDocumentSearchTool || r.ToolCallID == "" || r.SkillID != pin.ID || r.SkillVersion != pin.Version || r.SkillDigest != pin.Digest || r.OutputDigest != personaRunT0ToolOutputDigest(r.Output) {
			return nil, errPersonaRuntimeTools
		}
		if err := e.RecheckPersonaRuntimeToolResult(ctx, r); err != nil {
			return nil, errPersonaRuntimeTools
		}
		var result PersonaPolicyDocumentSearchResult
		if json.Unmarshal(r.Output, &result) != nil {
			return nil, errPersonaRuntimeTools
		}
		for _, hit := range result.Hits {
			if hit.DocumentID == "" || hit.VersionID == "" || hit.Title == "" {
				return nil, errPersonaRuntimeTools
			}
			raw, err := json.Marshal(hit)
			if err != nil {
				return nil, err
			}
			content, digest := string(raw), personaRunT0ToolOutputDigest(raw)
			location := fmt.Sprintf("document:%s/version:%s", hit.DocumentID, hit.VersionID)
			if hit.Markdown != "" {
				if hit.ContentDigest != personaRunT0ToolOutputDigest([]byte(hit.Markdown)) || hit.ScopeID != identity.ConversationID || hit.PlacementID == "" {
					return nil, errPersonaRuntimeTools
				}
				content = hit.Markdown
				digest = hit.ContentDigest
				location += "/placement:" + hit.PlacementID + "/scope:" + hit.ScopeID
			}
			datum, err := gateway.Observe(agentsecurity.SourceDocument, content, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: "document:" + hit.DocumentID + "/version:" + hit.VersionID, Location: location, Digest: digest})
			if err != nil {
				return nil, err
			}
			grounding = append(grounding, datum)
		}
	}
	return grounding, nil
}

var _ PersonaRunT0ToolExecutionPort = (*PersonaRuntimeTools)(nil)
