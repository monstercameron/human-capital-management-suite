package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

var errPersonaChatAdmissionAdapter = errors.New("application: persona chat run admission unavailable")

// personaChatAdmissionRequestBuilder resolves owner-owned admission evidence
// such as the legal entity, immutable digest, current audience and context
// snapshots, deadline, and budget. It must not infer those values from chat
// text or request claims.
type personaChatAdmissionRequestBuilder interface {
	BuildPersonaChatAdmission(context.Context, agentinvoke.RunRequest) (agentrun.Request, error)
}

// personaChatRunAdmission persists the shared admission decision.
type personaChatRunAdmission interface {
	Admit(context.Context, agentrun.Request) (agentrun.Record, bool, error)
}

// personaChatAdmittedRunExecutor creates or resumes durable execution from an
// accepted admission. It must be idempotent by admission record ID.
type personaChatAdmittedRunExecutor interface {
	Start(context.Context, agentrun.Record) (runstate.Run, error)
}

// personaChatAdmissionAdapter translates a persona invocation into the shared
// AgentRun admission path. It does not synthesize missing authority facts or
// treat an admitted request as executed work.
type personaChatAdmissionAdapter struct {
	builder   personaChatAdmissionRequestBuilder
	admission personaChatRunAdmission
	executor  personaChatAdmittedRunExecutor
}

func newPersonaChatAdmissionAdapter(
	builder personaChatAdmissionRequestBuilder,
	admission personaChatRunAdmission,
	executor personaChatAdmittedRunExecutor,
) (*personaChatAdmissionAdapter, error) {
	if builder == nil || admission == nil || executor == nil {
		return nil, errPersonaChatAdmissionAdapter
	}
	return &personaChatAdmissionAdapter{builder: builder, admission: admission, executor: executor}, nil
}

// Start validates the on-behalf-of actor chain, asks the trusted request
// builder for current owner evidence, overwrites source and invocation identity
// from the committed chat event, then admits and starts the durable run.
func (a *personaChatAdmissionAdapter) Start(ctx context.Context, invocation agentinvoke.RunRequest) error {
	if a == nil || a.builder == nil || a.admission == nil || a.executor == nil {
		return errPersonaChatAdmissionAdapter
	}
	if ctx == nil || !validPersonaChatRunRequest(invocation) {
		return errPersonaChatAdmissionAdapter
	}
	request, err := a.builder.BuildPersonaChatAdmission(ctx, invocation)
	if err != nil {
		return fmt.Errorf("%w: build owner evidence: %v", errPersonaChatAdmissionAdapter, err)
	}
	bindPersonaChatAdmissionRequest(&request, invocation)
	if err := validatePersonaChatAdmissionBinding(request, invocation); err != nil {
		return err
	}

	record, _, err := a.admission.Admit(ctx, request)
	if err != nil {
		return fmt.Errorf("%w: persist AgentRun admission: %v", errPersonaChatAdmissionAdapter, err)
	}
	if err := validatePersonaChatAdmissionRecord(record, request); err != nil {
		return err
	}
	if record.Decision != agentrun.DecisionAccepted {
		return &agentrun.AdmissionRefusal{Code: record.RefusalCode}
	}
	if _, err := a.executor.Start(ctx, record); err != nil {
		return fmt.Errorf("%w: start admitted AgentRun: %v", errPersonaChatAdmissionAdapter, err)
	}
	return nil
}

func validPersonaChatRunRequest(request agentinvoke.RunRequest) bool {
	if request.Mode != agentinvoke.OnBehalfOf ||
		strings.TrimSpace(request.InvocationID) == "" ||
		strings.TrimSpace(request.TenantID) == "" ||
		strings.TrimSpace(request.ConversationID) == "" ||
		strings.TrimSpace(request.ThreadID) == "" ||
		strings.TrimSpace(request.InvokingPostID) == "" ||
		strings.TrimSpace(request.InvokerID) == "" ||
		strings.TrimSpace(request.PersonaID) == "" ||
		strings.TrimSpace(request.PersonaVersion) == "" ||
		strings.TrimSpace(request.InstallationID) == "" ||
		strings.TrimSpace(request.Grant.ID) == "" ||
		request.Grant.UserID != request.InvokerID ||
		request.Grant.TenantID != request.TenantID ||
		!agentinvoke.SkillScopesSubset(request.Grant.Skills, request.Skills) ||
		len(request.Grant.Skills) == 0 ||
		request.Actor.Validate() != nil {
		return false
	}
	return request.Actor.UserID == request.InvokerID &&
		request.Actor.PersonaID == request.PersonaID &&
		request.Actor.PersonaVersion == request.PersonaVersion &&
		request.Actor.InstallationID == request.InstallationID &&
		request.Actor.ConversationID == request.ConversationID &&
		request.Actor.InvokingPostID == request.InvokingPostID &&
		request.Actor.InvocationID == request.InvocationID
}

func bindPersonaChatAdmissionRequest(request *agentrun.Request, invocation agentinvoke.RunRequest) {
	request.Source = agentrun.SourceIdentity{
		TenantID: invocation.TenantID,
		Kind:     agentrun.SourcePersonaMention,
		Key:      invocation.InvocationID,
		Ref:      invocation.InvokingPostID,
	}

	request.InstallationID = invocation.InstallationID
	if request.Persona != nil {
		persona := *request.Persona
		persona.ID = invocation.PersonaID
		persona.Version = invocation.PersonaVersion
		request.Persona = &persona
	}
	request.Principal.Mode = agentrun.ModeOnBehalfOf
	request.Principal.InvokerID = invocation.InvokerID
	request.Principal.SponsorID = ""
	request.Principal.DelegatedCredentialRef = invocation.Grant.ID
	request.Audience.ID = invocation.ConversationID
	request.Context.ID = invocation.ThreadID
	request.CauseID = invocation.InvocationID
	request.Purpose = "persona-mention"
	request.Deadline = request.Deadline.UTC()
}

func validatePersonaChatAdmissionBinding(request agentrun.Request, invocation agentinvoke.RunRequest) error {
	if request.Source.TenantID != invocation.TenantID ||
		request.Source.Kind != agentrun.SourcePersonaMention ||
		request.Source.Key != invocation.InvocationID ||
		request.Source.Ref != invocation.InvokingPostID ||
		strings.TrimSpace(request.Agent.AgentID) == "" ||
		strings.TrimSpace(request.Agent.Version) == "" ||
		request.Persona == nil ||
		request.Persona.ID != invocation.PersonaID ||
		request.Persona.Version != invocation.PersonaVersion ||
		strings.TrimSpace(request.Persona.Digest) == "" ||

		request.InstallationID != invocation.InstallationID ||
		request.Principal.Mode != agentrun.ModeOnBehalfOf ||
		request.Principal.InvokerID != invocation.InvokerID ||
		request.Principal.DelegatedCredentialRef != invocation.Grant.ID ||
		request.Principal.SponsorID != "" ||
		request.Audience.ID != invocation.ConversationID ||
		request.Context.ID != invocation.ThreadID ||
		request.CauseID != invocation.InvocationID ||
		request.Purpose != "persona-mention" {
		return fmt.Errorf("%w: admission request escaped its chat invocation binding", errPersonaChatAdmissionAdapter)
	}
	return nil
}

func validatePersonaChatAdmissionRecord(record agentrun.Record, request agentrun.Request) error {
	if err := agentrun.ValidateAdmissionRecord(record); err != nil {
		return fmt.Errorf("%w: invalid persisted admission record: %v", errPersonaChatAdmissionAdapter, err)
	}
	if !reflect.DeepEqual(record.Request, request) {
		return fmt.Errorf("%w: persisted admission record changed the invocation", errPersonaChatAdmissionAdapter)
	}
	return nil
}
