package application

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// AgentActionModelJSONSchema admits business material only. Run, principal,
// provenance, model and approval evidence are resolved by the server owners.
const AgentActionModelJSONSchema = `{"type":"object","additionalProperties":false,"properties":{"definition":{"type":"object","additionalProperties":false,"properties":{"intent_type_id":{"type":"string"},"version":{"type":"integer"}},"required":["intent_type_id","version"]},"subjects":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"subject_kind":{"type":"string"},"subject_id":{"type":"string"},"authority_domain":{"type":"string"}},"required":["subject_kind","subject_id","authority_domain"]}},"argument_fields":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"value_json":{"type":"string"}},"required":["name","value_json"]}},"uncertainty":{"type":"string"}},"required":["definition","subjects","argument_fields","uncertainty"]}`

type AgentActionModelDefinition struct {
	IntentTypeID string `json:"intent_type_id" schemaflux:"required"`
	Version      uint32 `json:"version" schemaflux:"required"`
}
type AgentActionModelSubject struct {
	SubjectKind     string `json:"subject_kind" schemaflux:"required"`
	SubjectID       string `json:"subject_id" schemaflux:"required"`
	AuthorityDomain string `json:"authority_domain" schemaflux:"required"`
}
type AgentActionModelArgument struct {
	Name      string `json:"name" schemaflux:"required"`
	ValueJSON string `json:"value_json" schemaflux:"required"`
}
type AgentActionModelCandidate struct {
	Definition  AgentActionModelDefinition `json:"definition" schemaflux:"required"`
	Subjects    []AgentActionModelSubject  `json:"subjects" schemaflux:"required"`
	Arguments   []AgentActionModelArgument `json:"argument_fields" schemaflux:"required"`
	Uncertainty string                     `json:"uncertainty" schemaflux:"required"`
}

type agentActionValidatedOutput struct {
	Kind              string                    `json:"kind"`
	TenantID          string                    `json:"tenant_id"`
	RunID             string                    `json:"run_id"`
	ForUser           string                    `json:"for_user"`
	RequestDigest     string                    `json:"request_digest"`
	ModelResultDigest string                    `json:"model_result_digest"`
	Schema            agentmanifest.Reference   `json:"schema"`
	Proposal          AgentActionCompileRequest `json:"proposal"`
}

type AgentActionModelOutputPolicy struct{ Authority *AgentActionRuntimeAuthority }

func (a *AgentActionRuntimeAuthority) currentActionRoute(ctx context.Context, record agentrun.Record, run runstate.Run) (agentmanifest.Reference, PersonaRunModelRoute, error) {
	if a == nil || record.Request.Principal.Mode != agentrun.ModeOnBehalfOf || record.Authority.Principal.InvokerID == "" {
		return agentmanifest.Reference{}, PersonaRunModelRoute{}, ErrAgentActionApproval
	}
	work := DatabaseCommonAgentModelWorkSource{cfg: CommonAgentModelWorkConfig{Runtime: a.cfg.Runtime, Manifests: a.cfg.Manifests, Routes: a.cfg.Routes, TenantUUID: a.cfg.TenantUUID, Now: a.cfg.Now}}
	manifest, _, route, err := work.current(ctx, record, run)
	if err != nil {
		return agentmanifest.Reference{}, PersonaRunModelRoute{}, err
	}
	version, err := strconv.ParseUint(record.Authority.Agent.Version, 10, 64)
	if err != nil || manifest.Version != version || manifest.OutputSchema.Digest != taskModelDigest([]byte(AgentActionModelJSONSchema)) {
		return agentmanifest.Reference{}, PersonaRunModelRoute{}, ErrAgentActionApproval
	}
	contract, err := a.cfg.Contracts.resolve(ctx, values.TenantId(run.TenantID), agentmodelpolicystore.OutputSchema, manifest.OutputSchema)
	if err != nil || contract.Record.Reference != manifest.OutputSchema || string(contract.Record.Content) != AgentActionModelJSONSchema || agentmodelpolicystore.ValidateRecord(contract.Record) != nil {
		return agentmanifest.Reference{}, PersonaRunModelRoute{}, ErrAgentActionApproval
	}
	return manifest.OutputSchema, route, nil
}

func strictAgentActionJSON(content []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(content))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrAgentActionInput
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrAgentActionInput
	}
	return nil
}

func (p AgentActionModelOutputPolicy) ValidateOutput(ctx context.Context, record agentrun.Record, result agentmodel.ModelResult) ([]byte, model.ClassificationLabel, string, error) {
	a := p.Authority
	if a == nil || ctx == nil || result.Finish != agentmodel.FinishComplete || result.Refusal != nil || result.Failure != nil || len(result.ToolProposals) != 0 {
		return nil, "", "", ErrAgentActionApproval
	}
	if err := a.cfg.Runtime.Recheck(ctx, record.Request.Source.TenantID, record.ID); err != nil {
		return nil, "", "", err
	}
	run, err := a.cfg.Runtime.GetRun(ctx, record.Request.Source.TenantID, record.ID)
	if err != nil {
		return nil, "", "", err
	}
	schema, route, err := a.currentActionRoute(ctx, record, run)
	if err != nil || result.Provider != route.Route.Pin.Primary.Identity {
		return nil, "", "", ErrAgentActionApproval
	}
	raw := result.Structured
	if len(raw) == 0 {
		raw = json.RawMessage(result.Text)
	}
	var candidate AgentActionModelCandidate
	if strictAgentActionJSON(raw, &candidate) != nil || len(candidate.Subjects) == 0 || len(candidate.Arguments) == 0 || len(candidate.Arguments) > 128 || strings.TrimSpace(candidate.Uncertainty) == "" {
		return nil, "", "", ErrAgentActionInput
	}
	ref, err := protomap.DefinitionRefFromProto(&intentsv1.DefinitionReference{IntentTypeId: candidate.Definition.IntentTypeID, Version: candidate.Definition.Version})
	if err != nil {
		return nil, "", "", ErrAgentActionInput
	}
	definition, err := a.cfg.Cell.Definitions.ResolveForInstantiation(ref)
	if err != nil {
		return nil, "", "", err
	}
	published, err := protomap.DefinitionToProto(definition)
	if err != nil {
		return nil, "", "", err
	}
	arguments := map[string]any{}
	for _, field := range candidate.Arguments {
		if strings.TrimSpace(field.Name) == "" || !json.Valid([]byte(field.ValueJSON)) {
			return nil, "", "", ErrAgentActionInput
		}
		if _, exists := arguments[field.Name]; exists {
			return nil, "", "", ErrAgentActionInput
		}
		var value any
		if json.Unmarshal([]byte(field.ValueJSON), &value) != nil {
			return nil, "", "", ErrAgentActionInput
		}
		arguments[field.Name] = value
	}
	message, err := structpb.NewStruct(arguments)
	if err != nil {
		return nil, "", "", ErrAgentActionInput
	}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return nil, "", "", err
	}
	var subjects []*intentsv1.SubjectReference
	for _, subject := range candidate.Subjects {
		if subject.SubjectKind == "" || subject.SubjectID == "" || subject.AuthorityDomain == "" {
			return nil, "", "", ErrAgentActionInput
		}
		subjects = append(subjects, &intentsv1.SubjectReference{SubjectKind: subject.SubjectKind, SubjectId: subject.SubjectID, AuthorityDomain: subject.AuthorityDomain})
	}
	proposal := AgentActionCompileRequest{Definition: published, Request: &intentsv1.CreateIntentRequest{Definition: published.Reference, Subjects: subjects, Request: &intentsv1.TypedPayload{Schema: published.InputSchema, ProtobufWireBytes: wire}, ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE}, AgentRunID: run.ID, AgentVersionRef: run.AgentID + "@" + run.AgentVersion, ModelDigest: "sha256:" + route.Route.Pin.Primary.ProfileDigest, Sources: []string{record.Request.Source.Ref, record.Request.Context.SnapshotID}, Taint: []string{"AGENT_DERIVED"}, Uncertainty: candidate.Uncertainty}
	resultDigest, err := commonAgentWorkerDigest(result)
	if err != nil {
		return nil, "", "", err
	}
	if !agentActionHasResultCheckpoint(run, resultDigest) {
		return nil, "", "", ErrAgentActionApproval
	}
	content, err := json.Marshal(agentActionValidatedOutput{Kind: "business-intent-proposal/v1", TenantID: run.TenantID, RunID: run.ID, ForUser: record.Authority.Principal.InvokerID, RequestDigest: record.RequestDigest, ModelResultDigest: resultDigest, Schema: schema, Proposal: proposal})
	return content, a.cfg.Classification, a.cfg.RetentionClass, err
}

func (p AgentActionModelOutputPolicy) DeliverOutput(ctx context.Context, record agentrun.Record, output CommonAgentOutput, content []byte) error {
	if p.Authority == nil || !commonAgentOutputValid(output) || output.Digest != taskModelDigest(content) {
		return ErrAgentActionApproval
	}
	run, err := p.Authority.cfg.Runtime.GetRun(ctx, record.Request.Source.TenantID, record.ID)
	if err != nil {
		return err
	}
	_, err = p.Authority.validateRetained(ctx, record, run, content)
	return err
}

func agentActionHasResultCheckpoint(run runstate.Run, digest string) bool {
	for _, checkpoint := range run.Checkpoints {
		if checkpoint.Phase == runstate.PhaseModelCall && checkpoint.Attempt == 2 && checkpoint.Digest == digest && checkpoint.Ref != "" {
			return true
		}
	}
	return false
}
