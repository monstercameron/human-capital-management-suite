package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The agent-facing capability and skills (UXBLIND-122). Every capability in
// the cell's own registry is AgentEligible=false and the bootstrap
// definitions are hashed by drift gates, so the agent runtime publishes its
// own private registry holding one agent-eligible, read-only capability. It
// reads the signed-in user's OWN worker facts through the same
// people.ExplainWorkerState the cell's workspace uses, over the cell's own
// worker-facts port.
const (
	agentReadCapabilityID = "hcmnext.agent.read_own_worker_state"
	agentReadSkillID      = "agent.read_own_worker_state"
	agentSummarizeSkillID = "agent.summarize_request"
	agentSkillVersion     = uint32(1)

	// agentPurpose is the purpose every agent task runs under. It is the
	// agent's own vocabulary, not one of the user's session purposes.
	agentPurpose = "agent.self_service"
)

var (
	errAgentSubjectUnknown  = errors.New("application: the delegated subject is not an active worker of the tenant")
	errAgentCallInvalid     = errors.New("application: the agent worker-state call is malformed")
	errAgentWorkerNotLocked = errors.New("application: agent worker reader is not composed")
)

// agentReadFields is the self-service projection the agent may read: who the
// worker is and where they sit. It carries no compensation, no manager edge
// and no legal identifiers; a new field is a deliberate edit here.
func agentReadFields() []people.FieldID {
	return []people.FieldID{
		people.FieldPreferredName, people.FieldLegalName, people.FieldWorkerNumber,
		people.FieldLifecycleStatus, people.FieldWorkerType, people.FieldJobCode,
		people.FieldOrgUnit, people.FieldLocation,
	}
}

// agentWorkerStateCall is the only payload the capability accepts. Tenant,
// Subject and Purpose come from verified delegated claims (see toolOwner);
// nothing in it is read from the task prompt.
type agentWorkerStateCall struct {
	Tenant  values.TenantId
	Subject string
	Purpose string
}

// agentWorkerState is the capability's typed answer: value-only facts of the
// caller's own worker record plus a digest of the governed explanation.
type agentWorkerState struct {
	Tenant    string
	Subject   string
	Facts     []agentWorkerFact
	Digest    string
	FactCount int
}

type agentWorkerFact struct {
	Field string
	Value string
}

// ownWorkerReader is the capability handler. It resolves the delegated
// subject (a worker key) to the tenant's own worker row, requires the worker
// to be active, and asks people.ExplainWorkerState under a self-service
// decision that rules only on agentReadFields.
type ownWorkerReader struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
	workers    people.WorkerFacts
	now        func() time.Time
}

func (r ownWorkerReader) handle(ctx context.Context, payload any) (any, error) {
	call, ok := payload.(agentWorkerStateCall)
	if !ok {
		return nil, fmt.Errorf("%w: got %T", errAgentCallInvalid, payload)
	}
	if r.db == nil || r.tenantUUID == nil || r.workers == nil {
		return nil, errAgentWorkerNotLocked
	}
	if err := call.Tenant.Validate(); err != nil || strings.TrimSpace(call.Subject) == "" || strings.TrimSpace(call.Purpose) == "" {
		return nil, fmt.Errorf("%w: tenant, subject and purpose are required", errAgentCallInvalid)
	}
	ref, err := r.locate(ctx, call)
	if err != nil {
		return nil, err
	}
	instant := values.NewInstant(r.now())
	known, err := values.NewKnownAt(instant)
	if err != nil {
		return nil, err
	}
	at := r.now().UTC()
	effective, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return nil, err
	}
	fields := agentReadFields()
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = people.FieldRuling{Effect: people.EffectAllow, Reason: "agent.self_service.own_record"}
	}
	explanation, err := people.ExplainWorkerState(ctx, r.workers, people.ExplainWorkerStateRequest{
		Tenant: call.Tenant, Worker: ref, AsOf: people.AsOf{EffectiveOn: effective, KnownAt: known}, Fields: fields,
		Authorization: people.AuthorizationDecision{
			PolicyVersion: "agent.self_service.v1", Purpose: call.Purpose, SubjectDisclosable: true, Fields: rulings,
		},
	})
	if err != nil {
		return nil, err
	}
	if explanation.Disclosure == people.DisclosureWithheld || explanation.Presence == people.SubjectAbsent {
		return nil, errAgentSubjectUnknown
	}
	out := agentWorkerState{Tenant: call.Tenant.String(), Subject: call.Subject, Digest: explanation.ResultDigest}
	for _, fact := range explanation.AuthorizedFields() {
		if value, present := fact.Value.Get(); present {
			out.Facts = append(out.Facts, agentWorkerFact{Field: string(fact.Field), Value: value})
		}
	}
	out.FactCount = len(out.Facts)
	return out, nil
}

// locate maps the delegated subject onto the tenant's own worker entity. It
// is the lookup the chat authority uses (worker key or id under tenant row
// security) and additionally requires an active worker.
func (r ownWorkerReader) locate(ctx context.Context, call agentWorkerStateCall) (values.EntityRef, error) {
	tenantID := r.tenantUUID(call.Tenant)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return values.EntityRef{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return values.EntityRef{}, err
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, call.Subject)
	if err != nil {
		return values.EntityRef{}, err
	}
	if !found || !strings.EqualFold(row.LifecycleStatus, "active") {
		return values.EntityRef{}, errAgentSubjectUnknown
	}
	return values.EntityRef{Tenant: call.Tenant, Kind: people.KindWorker, Id: row.WorkerID.String()}, nil
}

// newAgentCapabilities builds the agent-facing capability registry and its
// gateway. The gateway writes the same evidence sink the cell's own gateway
// does, so an agent's read is on the same evidence list as a person's.
func newAgentCapabilities(reader ownWorkerReader, evidence capability.EvidenceSink, now func() time.Time) (*capability.Registry, *capability.Gateway, error) {
	if evidence == nil {
		return nil, nil, errors.New("application: agent capability gateway needs an evidence sink")
	}
	registry := capability.NewRegistry()
	definition := capability.Definition{
		ID: agentReadCapabilityID, Version: 1, OwnerDomain: "people",
		RequestSchema:        capability.SchemaRef{SchemaID: agentReadCapabilityID + ".request", Version: 1, ProtobufFullName: "hcmnext.agent.v1.ReadOwnWorkerStateRequest"},
		ResponseSchema:       capability.SchemaRef{SchemaID: agentReadCapabilityID + ".response", Version: 1, ProtobufFullName: "hcmnext.agent.v1.ReadOwnWorkerStateResponse"},
		ErrorSchema:          capability.SchemaRef{SchemaID: agentReadCapabilityID + ".error", Version: 1, ProtobufFullName: "hcmnext.agent.v1.ReadOwnWorkerStateError"},
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{"worker", "employment"}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AgentEligible:        true,
		AuthZScopeRef:        "scope:people.read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "test:TestTodo_UXBLIND_122",
	}
	if err := registry.Register(definition, reader.handle); err != nil {
		return nil, nil, err
	}
	var options []capability.GatewayOption
	if now != nil {
		options = append(options, capability.WithClock(now))
	}
	return registry, capability.NewGateway(registry, evidence, options...), nil
}

// newAgentSkills publishes the two skills a self-service task uses: a T0 read
// of the caller's own worker state and a T1 private-draft summary produced by
// the model. Both name the same capability, so one delegated scope covers
// both; the summary route receives only the reviewed public task goal, never
// the worker-state result retained in the task ledger. The declared data
// classes follow from that: the read skill reads a personnel record (PII),
// and the summary skill, whose class is the one a model request carries,
// declares PUBLIC, the only class a model provider is cleared for here.
func newAgentSkills(caps agentskills.CapabilityCatalog) (*agentskills.Registry, error) {
	registry := agentskills.NewRegistry(caps)
	operation := agentskills.OperationRef{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: agentReadCapabilityID, Version: 1}}
	inputSchema := json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"}}}`)
	outputSchema := json.RawMessage(`{"type":"object"}`)
	for _, def := range []agentskills.SkillDefinition{
		{
			ID: agentReadSkillID, Version: agentSkillVersion, Owner: "people",
			Description: "Read the signed-in user's own worker record: name, number, status, job code, org unit and location.",
			InputSchema: inputSchema, OutputSchema: outputSchema, Operations: []agentskills.OperationRef{operation},
			SideEffectTier: agentskills.TierRead, RequiredPurposes: []string{agentPurpose},
			DataClassesRead: []string{"PII"}, IdempotencyRule: "read-only", CostClass: "LOW",
			EvalRefs: []string{"eval:TestTodo_UXBLIND_122"},
		},
		{
			ID: agentSummarizeSkillID, Version: agentSkillVersion, Owner: "people",
			Description: "Draft a short private answer to the user's request with the configured model; no worker records are changed.",
			InputSchema: inputSchema, OutputSchema: outputSchema, Operations: []agentskills.OperationRef{operation},
			SideEffectTier: agentskills.TierPrivateDraft, RequiredPurposes: []string{agentPurpose},
			DataClassesRead: []string{"PUBLIC"}, IdempotencyRule: "read-only", CostClass: "LOW",
			EvalRefs: []string{"eval:TestTodo_UXBLIND_122"},
		},
	} {
		if err := registry.Publish(def); err != nil {
			return nil, fmt.Errorf("publish agent skill %s: %w", def.ID, err)
		}
	}
	return registry, nil
}
