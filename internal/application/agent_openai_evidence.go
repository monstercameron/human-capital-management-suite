package application

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type openAIModelDispatchContextKey struct{}
type openAIModelDispatchBinding struct{ tenant, runID, stepID string }

// PersonaOpenAIModelOwnerConfig binds model evidence to current durable
// admission, persona, manifest, route and invoker-readable thread owners.
type PersonaOpenAIModelOwnerConfig struct {
	DB          personaRunTenantTxRunner
	Personas    personaRunAuthorityFactory
	Manifests   personaRunManifestResolverFactory
	Routes      PersonaModelRoutePolicyReader
	Threads     agentinvoke.ThreadReader
	Authority   agentrun.Authority
	Audit       agentmodel.AuditStore
	ToolJournal PersonaRuntimeToolJournal
	ToolSources PersonaRuntimeToolSourceValidator
	ChatClasses PersonaPublicChatDisclosureClassificationSource
	Documents   agentdocref.Resolver
	TenantUUID  func(values.TenantId) uuid.UUID
	Now         func() time.Time
}

// PersonaOpenAIModelEvidence verifies source classifications and durably
// records route decisions. It does not grant egress or manufacture route policy.
type PersonaOpenAIModelEvidence struct{ cfg PersonaOpenAIModelOwnerConfig }

func NewPersonaOpenAIModelEvidence(cfg PersonaOpenAIModelOwnerConfig) (*PersonaOpenAIModelEvidence, error) {
	if cfg.DB == nil || cfg.Personas == nil || cfg.Manifests == nil || cfg.Routes == nil || cfg.Threads == nil || cfg.Authority == nil || cfg.Audit == nil || isNilPersonaOutputPort(cfg.ChatClasses) || cfg.TenantUUID == nil || cfg.Now == nil {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	return &PersonaOpenAIModelEvidence{cfg: cfg}, nil
}

func (e *PersonaOpenAIModelEvidence) admission(ctx context.Context, binding openAIModelDispatchBinding) (agentrun.Record, error) {
	if e == nil || ctx == nil || binding.tenant == "" || binding.runID == "" {
		return agentrun.Record{}, ErrAgentModelGatewayTenant
	}
	key := e.cfg.TenantUUID(values.TenantId(binding.tenant))
	if key == uuid.Nil {
		return agentrun.Record{}, ErrAgentModelGatewayTenant
	}
	reader, err := agentrunstore.NewAdmissionRepository(e.cfg.DB, key, values.TenantId(binding.tenant))
	if runtime, ok := ctx.Value(announcementRuntimeKey{}).(*AgentAnnouncementRuntime); ok && runtime != nil {
		// Announcements use their source owner's occurrence key. The repository
		// still verifies the stored source digest and full admission record.
		reader, err = agentrunstore.NewAdmissionRepositoryWithSourceResolver(e.cfg.DB, key, values.TenantId(binding.tenant), announcementEvidenceSourceKeys{runtime})
	}
	if err != nil {
		return agentrun.Record{}, err
	}
	record, err := reader.GetByID(ctx, binding.runID)
	if err != nil || record.ID != binding.runID || record.Request.Source.TenantID != binding.tenant || record.Decision != agentrun.DecisionAccepted || record.Request.Persona == nil {
		return agentrun.Record{}, ErrAgentModelGatewayTenant
	}
	current, err := e.cfg.Authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return agentrun.Record{}, ErrAgentModelGatewayTenant
	}
	return record, nil
}

func (e *PersonaOpenAIModelEvidence) RecordRoute(ctx context.Context, route agentmodel.RouteRecord) error {
	if e == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	binding, ok := ctx.Value(openAIModelDispatchContextKey{}).(openAIModelDispatchBinding)
	if !ok || binding.stepID != route.TraceID {
		return ErrAgentModelGatewayTenant
	}
	record, err := e.admission(ctx, binding)
	if err != nil {
		return err
	}
	if route.AgentVersionDigest != record.Authority.Agent.Digest || route.Digest == "" {
		return ErrAgentModelGatewayPin
	}
	encoded, err := json.Marshal(route)
	if err != nil {
		return err
	}
	eventID := "persona-model-route:" + route.TraceID + ":" + route.Digest
	userID, grant := record.Request.Principal.InvokerID, record.Request.Principal.DelegatedCredentialRef
	if record.Request.Source.Kind == agentrun.SourceAnnouncement {
		userID, grant = record.Request.Principal.RequesterID, record.Authority.GrantRef
	}
	_, err = e.cfg.Audit.Append(ctx, agentaudit.Entry{EventID: eventID, TenantID: binding.tenant, Kind: agentaudit.EventModelCall,
		Actor:  agentaudit.ActorChain{UserID: userID, AgentVersion: record.Authority.Agent.AgentID + "@" + record.Authority.Agent.Version, InstallationID: record.Request.InstallationID, TaskID: record.ID, PlanRevision: record.RequestDigest, StepID: route.TraceID, DelegationGrantID: grant},
		Action: "model.route", ResultDigest: route.Digest, Fields: []agentaudit.Field{{Name: "model_route", Value: string(encoded), Classification: agentaudit.ClassificationInternal}}, Edges: []agentaudit.Edge{{Kind: agentaudit.EdgeTask, From: record.ID, To: eventID}}, OccurredAt: record.AdmittedAt})
	return err
}

func (e *PersonaOpenAIModelEvidence) VerifySourceClassification(ctx context.Context, source agentegress.SourceClassificationRequest) error {
	if e == nil || ctx == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	binding, ok := ctx.Value(openAIModelDispatchContextKey{}).(openAIModelDispatchBinding)
	if !ok || binding.tenant != source.Tenant || (!slices.Contains(source.Provenance, "persona-run:"+binding.runID) && !slices.Contains(source.Provenance, "persona-run:"+binding.stepID)) {
		return ErrAgentModelGatewayTenant
	}
	record, err := e.admission(ctx, binding)
	if err != nil {
		return err
	}
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	work := &DatabasePersonaRunModelWorkSource{personas: e.cfg.Personas, manifests: e.cfg.Manifests}
	profile, manifest, err := work.resolveProfileAndManifest(ctx, record)
	if err != nil {
		return err
	}
	ref := manifest.ModelPolicy
	stored, err := currentPersonaAgentModelRoute(ctx, e.cfg.Routes, e.cfg.TenantUUID(values.TenantId(binding.tenant)), record.Request.LegalEntity, ref, record.Request.Agent.Digest, e.cfg.Now().UTC())
	if err != nil {
		return err
	}
	var route PersonaRunModelRoute
	if stored.TenantID != e.cfg.TenantUUID(values.TenantId(binding.tenant)) || stored.LegalEntityID != record.Request.LegalEntity || stored.PolicyID != ref.ID || stored.PolicyVersion != int64(ref.Version) || stored.PolicySchemaVersion != int64(ref.SchemaVersion) || stored.PolicyDigest != ref.Digest || stored.Revision <= 0 || json.Unmarshal(stored.RoutePayload, &route) != nil || route.Purpose != source.Purpose {
		return ErrAgentModelGatewayPin
	}
	authoritativeDocumentFields, err := personaAuthoritativeDocumentFields(ctx, e.cfg.Documents, e.cfg.Threads, record, profile, manifest, route)
	if err != nil {
		return fmt.Errorf("%w: re-resolve reference documents: %v", ErrAgentModelGatewayPin, err)
	}
	var allowed bool
	switch source.SourceClass {
	case "persona-profile":
		if personaAuthoritativeDocumentField(authoritativeDocumentFields, source) {
			return nil
		}
		return ErrAgentModelGatewayPin
	case "persona-untrusted-reference-document", "persona-reference-document":
		if personaAuthoritativeDocumentField(authoritativeDocumentFields, source) {
			return nil
		}
		return ErrAgentModelGatewayPin
	case "persona-invoking-post":
		if record.Request.Source.Kind == agentrun.SourceAnnouncement {
			if personaAuthoritativeDocumentField(authoritativeDocumentFields, source) {
				return nil
			}
			return ErrAgentModelGatewayPin
		}
		allowed = source.DataClass == route.InvokerClass && strings.HasPrefix(source.FieldName, "model.message.")
	case "persona-thread-context":
		allowed = source.DataClass == route.ThreadClass && strings.HasPrefix(source.FieldName, "model.context.")
	case "persona-model-tool-proposal", "persona-untrusted-tool-result":
		return e.verifyToolSource(ctx, binding, record, source, route)
	default:
		return fmt.Errorf("application: model source requires durable tool provenance")
	}
	if !allowed {
		return ErrAgentModelGatewayPin
	}
	posts, err := e.cfg.Threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: source.Tenant, ConversationID: record.Request.Audience.ID, ThreadID: record.Request.Context.ID, InvokerID: record.Request.Principal.InvokerID, InvokingPostID: record.Request.Source.Ref, Limit: agentinvoke.MaxThreadPosts})
	if err != nil || len(posts) == 0 {
		return ErrAgentModelGatewayTenant
	}
	for _, post := range posts {
		if post.TenantID != source.Tenant || post.ConversationID != record.Request.Audience.ID || post.ThreadID != record.Request.Context.ID {
			return ErrAgentModelGatewayTenant
		}
	}
	switch source.SourceClass {
	case "persona-invoking-post":
		for _, post := range posts {
			if post.AuthorID == record.Request.Principal.InvokerID && !post.Bot && source.ValueDigest == personaRunBytesDigest([]byte(post.Body)) {
				class, err := e.cfg.ChatClasses.PersonaPublicChatDisclosureClass(ctx, source.Tenant, record.Request.Audience.ID, post.ID, digestPersonaThreadPost(post))
				if err == nil && personaOpenAISourceClassCovers(source.DataClass, class) {
					return nil
				}
				return ErrAgentModelGatewayPin
			}
		}
	case "persona-thread-context":
		for _, post := range posts {
			value := fmt.Sprintf("%s\x00thread\x00%s", post.ID, digestPersonaThreadPost(post))
			if source.ValueDigest == personaRunBytesDigest([]byte(value)) {
				class, err := e.cfg.ChatClasses.PersonaPublicChatDisclosureClass(ctx, source.Tenant, record.Request.Audience.ID, post.ID, digestPersonaThreadPost(post))
				if err == nil && personaOpenAISourceClassCovers(source.DataClass, class) {
					return nil
				}
				return ErrAgentModelGatewayPin
			}
		}
	}
	return ErrAgentModelGatewayPin
}

func personaOpenAISourceClassCovers(declared, actual trustdlp.DataClass) bool {
	return declared.Valid() && actual.Valid() && (declared == actual || (actual == trustdlp.ClassPublic && declared == trustdlp.ClassInternal))
}

func (e *PersonaOpenAIModelEvidence) verifyToolSource(ctx context.Context, binding openAIModelDispatchBinding, record agentrun.Record, source agentegress.SourceClassificationRequest, route PersonaRunModelRoute) error {
	if isNilPersonaOutputPort(e.cfg.ToolJournal) || isNilPersonaOutputPort(e.cfg.ToolSources) || !strings.HasPrefix(source.FieldName, "model.message.") {
		return ErrAgentModelGatewayPin
	}
	callID := ""
	for _, provenance := range source.Provenance {
		if strings.HasPrefix(provenance, "tool-call:") {
			if callID != "" {
				return ErrAgentModelGatewayPin
			}
			callID = strings.TrimPrefix(provenance, "tool-call:")
		}
	}
	if callID == "" {
		return ErrAgentModelGatewayPin
	}
	results, err := e.cfg.ToolJournal.ListPersonaRuntimeToolResults(ctx, values.TenantId(binding.tenant), binding.runID)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.ToolCallID != callID {
			continue
		}
		if string(result.TenantID) != binding.tenant || result.RunID != record.ID || result.AdmissionDigest != personaRuntimeToolAdmissionDigest(record.RequestDigest) || result.InvocationID != record.Request.Source.Key || result.InvokerID != record.Request.Principal.InvokerID || result.ConversationID != record.Request.Audience.ID || result.ThreadID != record.Request.Context.ID || result.PersonaID != record.Request.Persona.ID || result.PersonaVersion != record.Request.Persona.Version || result.InstallationID != record.Request.InstallationID || result.AgentID != record.Authority.Agent.AgentID {
			return ErrAgentModelGatewayPin
		}
		if source.SourceClass == "persona-model-tool-proposal" {
			if source.DataClass != route.InvokerClass || source.ValueDigest != personaRunBytesDigest(result.Arguments) {
				return ErrAgentModelGatewayPin
			}
		} else if source.DataClass != route.ThreadClass || source.DataClass != result.DataClass || source.ValueDigest != result.OutputDigest || source.ValueDigest != personaRunBytesDigest(result.Output) {
			return ErrAgentModelGatewayPin
		}
		return e.cfg.ToolSources.RecheckPersonaRuntimeToolResult(ctx, result)
	}
	return ErrAgentModelGatewayPin
}
