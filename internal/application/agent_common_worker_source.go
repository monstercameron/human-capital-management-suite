package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// NativeCommonAgentSource reads only a source occurrence that is still owned
// by Scheduling or Workflow. Context bytes live in the protected artifact
// store and must have been referenced by the independently granted scope ID.
type NativeCommonAgentSource struct {
	Core          dbport.Beginner
	CoreSchema    string
	ResolveTenant func(string) uuid.UUID
	Schedule      *AgentScheduleService
	Workflow      AgentWorkflowSourceAuthority
	Now           func() time.Time
	InputClasses  []model.ClassificationLabel
}

func (s NativeCommonAgentSource) ReadCommonAgentSourceContext(ctx context.Context, record agentrun.Record, run runstate.Run) (CommonAgentSourceContext, error) {
	r := record.Request
	if ctx == nil || s.Core == nil || s.CoreSchema == "" || s.ResolveTenant == nil || s.Now == nil ||
		record.Decision != agentrun.DecisionAccepted || agentrun.ValidateAdmissionRecord(record) != nil ||
		commonAgentCheckExecutionIdentity(record, run) != nil || commonAgentExecutionPrincipal(r) == "" {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	var execution runtime.ExecutionContext
	switch r.Source.Kind {
	case agentrun.SourceSchedule:
		if r.Principal.Mode != agentrun.ModeSponsored || r.Principal.AgentPrincipalID == "" || r.Principal.SponsorID == "" || r.Principal.InvokerID != "" || r.Principal.DelegatedCredentialRef != "" || s.Schedule == nil || s.Schedule.CheckRequest(ctx, r) != nil {
			return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
		}
		delivery, err := s.Schedule.SourceDelivery(ctx, r.Source.TenantID, r.Source.Key)
		if err != nil || !reflect.DeepEqual(delivery.Request, r) || delivery.TenantID != r.Source.TenantID || delivery.Key != r.Source.Key {
			return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
		}
	case agentrun.SourceWorkflow:
		if err := s.Workflow.CheckRequest(ctx, r); err != nil {
			return CommonAgentSourceContext{}, err
		}
		var err error
		execution, err = s.checkWorkflowExecution(ctx, r)
		if err != nil {
			return CommonAgentSourceContext{}, err
		}
	default:
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	material, err := s.readPinnedContextArtifact(ctx, r)
	if err != nil {
		return CommonAgentSourceContext{}, err
	}
	if r.Source.Kind == agentrun.SourceWorkflow {
		return commonAgentWorkflowExecutionMaterial(material, execution, strings.Split(r.Source.Ref, ":")[1])
	}
	return material, nil
}

func commonAgentWorkflowExecutionMaterial(material CommonAgentSourceContext, execution runtime.ExecutionContext, instanceID string) (CommonAgentSourceContext, error) {
	if execution.Validate() != nil || instanceID == "" {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	body, err := json.Marshal(struct {
		Locale             string                 `json:"locale"`
		Organization       string                 `json:"organization,omitempty"`
		LegalEntity        string                 `json:"legal_entity"`
		Purpose            string                 `json:"purpose"`
		ExecutionMode      workflow.ExecutionMode `json:"execution_mode"`
		WorkflowID         string                 `json:"workflow_id"`
		WorkflowVersion    uint32                 `json:"workflow_version"`
		CompiledPlanDigest string                 `json:"compiled_plan_digest"`
	}{execution.Locale, execution.Organization, execution.LegalEntity, execution.Purpose, execution.ExecutionMode, execution.WorkflowID, execution.WorkflowVersion, execution.CompiledPlanDigest})
	if err != nil || len(body) > 4096 {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	material.Messages = append([]agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Pinned workflow execution context: " + string(body)}}, material.Messages...)
	material.References = append(material.References, agentmodel.ContextReference{ID: "workflow-execution:" + instanceID, Version: execution.RuntimeVersion, Digest: execution.Digest()})
	return material, nil
}

func (s NativeCommonAgentSource) checkWorkflowExecution(ctx context.Context, r agentrun.Request) (runtime.ExecutionContext, error) {
	parts := strings.Split(r.Source.Ref, ":")
	if len(parts) != 3 || parts[0] != "workflow" || parts[2] == "" {
		return runtime.ExecutionContext{}, agentrun.ErrAuthorityRefusal
	}
	instanceID, err := uuid.Parse(parts[1])
	if err != nil || instanceID == uuid.Nil {
		return runtime.ExecutionContext{}, agentrun.ErrAuthorityRefusal
	}
	tenant := s.ResolveTenant(r.Source.TenantID)
	if tenant == uuid.Nil {
		return runtime.ExecutionContext{}, agentrun.ErrAuthorityRefusal
	}
	tx, err := s.Core.Begin(ctx)
	if err != nil {
		return runtime.ExecutionContext{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return runtime.ExecutionContext{}, err
	}
	execution, found, err := runtime.LoadExecutionContext(ctx, tx, tenant, instanceID)
	if err != nil {
		return runtime.ExecutionContext{}, err
	}
	if !found || execution.Validate() != nil || execution.Tenant != tenant.String() || execution.LegalEntity != r.LegalEntity || execution.Purpose != r.Purpose {
		return runtime.ExecutionContext{}, agentrun.ErrAuthorityRefusal
	}
	return execution, nil
}

func (s NativeCommonAgentSource) readPinnedContextArtifact(ctx context.Context, r agentrun.Request) (CommonAgentSourceContext, error) {
	const prefix = "artifact:"
	if !strings.HasPrefix(r.Context.SnapshotID, prefix) || r.Context.ID == "" || len(s.InputClasses) == 0 {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	contentID := strings.TrimPrefix(r.Context.SnapshotID, prefix)
	if !artifacts.ValidContentID(contentID) || r.Context.Digest != "sha256:"+contentID {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	for _, class := range s.InputClasses {
		if !class.Valid() {
			return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
		}
	}
	tenant := s.ResolveTenant(r.Source.TenantID)
	if tenant == uuid.Nil {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	tx, err := s.Core.Begin(ctx)
	if err != nil {
		return CommonAgentSourceContext{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return CommonAgentSourceContext{}, err
	}
	content, _, err := artifacts.Retrieve(ctx, tx, artifacts.Schema(s.CoreSchema), tenant, contentID, artifacts.RetrievalAuthorization{
		Purpose: r.Purpose, AllowedClassifications: s.InputClasses,
		Scope:       artifacts.SubjectScope{AllowedOwnerRefs: []string{r.Context.ID}},
		RequestedBy: commonAgentExecutionPrincipal(r), ExpiresAt: r.Deadline,
	}, s.Now().UTC())
	if err != nil {
		var denied artifacts.ErrRetrievalDenied
		if errors.As(err, &denied) {
			// Retrieve's refusal evidence is durable only after commit.
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return CommonAgentSourceContext{}, commitErr
			}
		}
		return CommonAgentSourceContext{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommonAgentSourceContext{}, err
	}
	if len(content) == 0 || len(content) > 64<<10 {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	var projection CommonAgentSourceContext
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&projection); err != nil {
		return CommonAgentSourceContext{}, err
	}
	if decoder.Decode(new(any)) != io.EOF || len(projection.Messages) == 0 || len(projection.Messages) > 32 || len(projection.Tools) != 0 {
		return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
	}
	for _, message := range projection.Messages {
		if message.Role != agentmodel.RoleUser || strings.TrimSpace(message.Content) == "" || len(message.Content) > 64<<10 || message.ToolCallID != "" || message.ToolName != "" || len(message.ToolArguments) != 0 {
			return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
		}
	}
	for _, ref := range projection.References {
		if ref.ID == "" || ref.Version == "" || ref.Digest == "" {
			return CommonAgentSourceContext{}, agentrun.ErrAuthorityRefusal
		}
	}
	projection.References = append(projection.References, agentmodel.ContextReference{ID: r.Context.ID, Version: r.Context.SnapshotID, Digest: r.Context.Digest})
	return projection, nil
}

// CommonAgentProtectedOutputPolicy supplies the exact schema pin and a
// classification/retention decision issued by the source and manifest owners.
// A caller cannot turn a model result into an accepted reference by naming it.
type CommonAgentProtectedOutputPolicy interface {
	ValidateOutput(context.Context, agentrun.Record, agentmodel.ModelResult) ([]byte, model.ClassificationLabel, string, error)
	DeliverOutput(context.Context, agentrun.Record, CommonAgentOutput, []byte) error
}

// CommonAgentProtectedOutput writes validated bytes into the existing
// content-addressed owner and records a durable reference held by this run.
type CommonAgentProtectedOutput struct {
	Core          dbport.Beginner
	CoreSchema    string
	ResolveTenant func(string) uuid.UUID
	Policy        CommonAgentProtectedOutputPolicy
	Current       *CommonAgentRuntime
	Now           func() time.Time
	OutputClasses []model.ClassificationLabel
	SourceClasses []model.ClassificationLabel
}

func (s CommonAgentProtectedOutput) ValidateAndPersistCommonAgentOutput(ctx context.Context, record agentrun.Record, run runstate.Run, result agentmodel.ModelResult) (CommonAgentOutput, error) {
	if ctx == nil || s.Core == nil || s.CoreSchema == "" || s.ResolveTenant == nil || s.Policy == nil || s.Current == nil || s.Now == nil ||
		result.Refusal != nil || result.Failure != nil || len(result.ToolProposals) != 0 ||
		commonAgentCheckExecutionIdentity(record, run) != nil {
		return CommonAgentOutput{}, ErrCommonAgentWorker
	}
	if err := s.Current.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return CommonAgentOutput{}, err
	}
	content, class, retention, err := s.Policy.ValidateOutput(ctx, record, result)
	if err != nil {
		return CommonAgentOutput{}, err
	}
	if len(content) == 0 || len(content) > 64<<10 || !json.Valid(content) || !class.Valid() || retention == "" || !commonAgentClassAllowed(s.OutputClasses, class) || !commonAgentOutputClassCovers(s.SourceClasses, class) {
		return CommonAgentOutput{}, ErrCommonAgentWorker
	}
	tenant := s.ResolveTenant(record.Request.Source.TenantID)
	if tenant == uuid.Nil {
		return CommonAgentOutput{}, ErrCommonAgentWorker
	}
	tx, err := s.Core.Begin(ctx)
	if err != nil {
		return CommonAgentOutput{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return CommonAgentOutput{}, err
	}
	schema := artifacts.Schema(s.CoreSchema)
	stored, _, err := artifacts.Put(ctx, tx, schema, artifacts.PutRequest{Tenant: tenant, Content: content, MediaType: "application/json", Classification: class, RetentionClass: retention, CreatorPrincipalRef: commonAgentExecutionPrincipal(record.Request), EvidenceID: record.ID})
	if err != nil {
		return CommonAgentOutput{}, err
	}
	if err := artifacts.AddReference(ctx, tx, schema, tenant, stored.ContentID, artifacts.OwnerRef{Kind: artifacts.OwnerReceipt, ID: record.ID}); err != nil {
		return CommonAgentOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommonAgentOutput{}, err
	}
	return CommonAgentOutput{Ref: "artifact:" + stored.ContentID, Digest: "sha256:" + stored.ContentID}, nil
}

func (s CommonAgentProtectedOutput) DeliverCommonAgentOutput(ctx context.Context, record agentrun.Record, run runstate.Run, output CommonAgentOutput) error {
	if ctx == nil || s.Core == nil || s.CoreSchema == "" || s.ResolveTenant == nil || s.Policy == nil || s.Current == nil || s.Now == nil || commonAgentCheckExecutionIdentity(record, run) != nil || output.Ref == "" || output.Ref != "artifact:"+strings.TrimPrefix(output.Digest, "sha256:") ||
		!artifacts.ValidContentID(strings.TrimPrefix(output.Digest, "sha256:")) {
		return ErrCommonAgentWorker
	}
	if err := s.Current.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return err
	}
	tenant := s.ResolveTenant(run.TenantID)
	if tenant == uuid.Nil || len(s.OutputClasses) == 0 {
		return ErrCommonAgentWorker
	}
	tx, err := s.Core.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	content, _, err := artifacts.Retrieve(ctx, tx, artifacts.Schema(s.CoreSchema), tenant, strings.TrimPrefix(output.Digest, "sha256:"), artifacts.RetrievalAuthorization{
		Purpose: record.Request.Purpose, AllowedClassifications: s.OutputClasses,
		Scope: artifacts.SubjectScope{AllowedOwnerRefs: []string{record.ID}}, RequestedBy: commonAgentExecutionPrincipal(record.Request),
		ExpiresAt: record.Request.Deadline,
	}, s.Now().UTC())
	if err != nil {
		var denied artifacts.ErrRetrievalDenied
		if errors.As(err, &denied) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return commitErr
			}
		}
		return err
	}
	if len(content) == 0 || len(content) > 64<<10 || !json.Valid(content) {
		return ErrCommonAgentWorker
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.Policy.DeliverOutput(ctx, record, output, content); err != nil {
		return fmt.Errorf("deliver protected agent output: %w", err)
	}
	return nil
}

func commonAgentClassAllowed(allowed []model.ClassificationLabel, value model.ClassificationLabel) bool {
	for _, candidate := range allowed {
		if candidate == value {
			return true
		}
	}
	return false
}

func commonAgentOutputClassCovers(sources []model.ClassificationLabel, output model.ClassificationLabel) bool {
	if len(sources) == 0 || !output.Valid() {
		return false
	}
	for _, source := range sources {
		if !source.Valid() || output.Rank() < source.Rank() {
			return false
		}
	}
	return true
}

// DatabaseCommonAgentOutputPolicy accepts only a currently published exact
// immutable reply schema. This initial native output is plain text with no
// model supplied citations, fields or tool effects.
type DatabaseCommonAgentOutputPolicy struct {
	Manifests      CommonAgentManifestReader
	Contracts      *AgentModelPolicyRegistry
	TenantUUID     func(values.TenantId) uuid.UUID
	Now            func() time.Time
	Classification model.ClassificationLabel
	RetentionClass string
}

type commonAgentProtectedReply struct {
	RunID         string                  `json:"run_id"`
	TenantID      string                  `json:"tenant_id"`
	Source        agentrun.SourceIdentity `json:"source"`
	Audience      agentrun.AudienceScope  `json:"audience"`
	ContextDigest string                  `json:"context_digest"`
	Schema        agentmanifest.Reference `json:"schema"`
	Text          string                  `json:"text"`
}

func (p DatabaseCommonAgentOutputPolicy) currentSchema(ctx context.Context, record agentrun.Record) (agentmanifest.Reference, error) {
	if p.Manifests == nil || p.Contracts == nil || p.TenantUUID == nil || p.Now == nil || !p.Classification.Valid() || p.RetentionClass == "" {
		return agentmanifest.Reference{}, ErrCommonAgentWorker
	}
	tenant := p.TenantUUID(values.TenantId(record.Request.Source.TenantID))
	version, err := strconv.ParseUint(record.Request.Agent.Version, 10, 64)
	if tenant == uuid.Nil || err != nil || version == 0 || p.Contracts.cfg.TenantUUID == nil || p.Contracts.cfg.TenantUUID(values.TenantId(record.Request.Source.TenantID)) != tenant || !record.Request.Deadline.After(p.Now().UTC()) {
		return agentmanifest.Reference{}, ErrCommonAgentWorker
	}
	manifest, err := p.Manifests.ManifestVersion(ctx, tenant, record.Request.Agent.AgentID, version)
	if err != nil || manifest.ID != record.Request.Agent.AgentID || manifest.Version != version || personaRunManifestDigest(manifest) != record.Request.Agent.Digest || manifest.Purpose != record.Request.Purpose || !validPersonaChatReplySchemaPin(manifest.OutputSchema) {
		return agentmanifest.Reference{}, ErrCommonAgentWorker
	}
	current, err := p.Contracts.resolve(ctx, values.TenantId(record.Request.Source.TenantID), agentmodelpolicystore.OutputSchema, manifest.OutputSchema)
	if err != nil || current.Record.Kind != agentmodelpolicystore.OutputSchema || current.Record.Reference != manifest.OutputSchema || string(current.Record.Content) != PersonaChatReplyJSONSchema || agentmodelpolicystore.ValidateRecord(current.Record) != nil {
		return agentmanifest.Reference{}, ErrCommonAgentWorker
	}
	return manifest.OutputSchema, nil
}

func (p DatabaseCommonAgentOutputPolicy) ValidateOutput(ctx context.Context, record agentrun.Record, result agentmodel.ModelResult) ([]byte, model.ClassificationLabel, string, error) {
	schema, err := p.currentSchema(ctx, record)
	if err != nil {
		return nil, "", "", err
	}
	if result.Finish != agentmodel.FinishComplete || result.Refusal != nil || result.Failure != nil || len(result.ToolProposals) != 0 || len(result.Structured) != 0 || validatePersonaChatReply(result.Text) != nil {
		return nil, "", "", ErrCommonAgentWorker
	}
	content, err := json.Marshal(commonAgentProtectedReply{RunID: record.ID, TenantID: record.Request.Source.TenantID, Source: record.Request.Source, Audience: record.Request.Audience, ContextDigest: record.Request.Context.Digest, Schema: schema, Text: result.Text})
	if err != nil {
		return nil, "", "", err
	}
	return content, p.Classification, p.RetentionClass, nil
}

func (p DatabaseCommonAgentOutputPolicy) DeliverOutput(ctx context.Context, record agentrun.Record, output CommonAgentOutput, content []byte) error {
	if output.Ref == "" || output.Digest == "" {
		return ErrCommonAgentWorker
	}
	schema, err := p.currentSchema(ctx, record)
	if err != nil {
		return err
	}
	var retained commonAgentProtectedReply
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&retained) != nil || decoder.Decode(new(any)) != io.EOF || retained.RunID != record.ID || retained.TenantID != record.Request.Source.TenantID || retained.Source != record.Request.Source || retained.Audience != record.Request.Audience || retained.ContextDigest != record.Request.Context.Digest || retained.Schema != schema || validatePersonaChatReply(retained.Text) != nil {
		return ErrCommonAgentWorker
	}
	return nil
}

// NativeCommonAgentExecutionSource composes the actual source artifact reader,
// pinned model work builder, and protected output owner for the worker.
type NativeCommonAgentExecutionSource struct {
	Context NativeCommonAgentSource
	Work    *DatabaseCommonAgentModelWorkSource
	Output  CommonAgentProtectedOutput
}

func (s NativeCommonAgentExecutionSource) BuildCommonAgentModelWork(ctx context.Context, record agentrun.Record, run runstate.Run) (AgentModelExecutorRequest, error) {
	if s.Work == nil {
		return AgentModelExecutorRequest{}, ErrCommonAgentWorker
	}
	material, err := s.Context.ReadCommonAgentSourceContext(ctx, record, run)
	if err != nil {
		return AgentModelExecutorRequest{}, err
	}
	return s.Work.BuildCommonAgentModelWork(ctx, record, run, material)
}

func (s NativeCommonAgentExecutionSource) ValidateAndPersistCommonAgentOutput(ctx context.Context, record agentrun.Record, run runstate.Run, result agentmodel.ModelResult) (CommonAgentOutput, error) {
	return s.Output.ValidateAndPersistCommonAgentOutput(ctx, record, run, result)
}

func (s NativeCommonAgentExecutionSource) DeliverCommonAgentOutput(ctx context.Context, record agentrun.Record, run runstate.Run, output CommonAgentOutput) error {
	return s.Output.DeliverCommonAgentOutput(ctx, record, run, output)
}

var _ CommonAgentContextSource = NativeCommonAgentSource{}
var _ CommonAgentExecutionSource = NativeCommonAgentExecutionSource{}
