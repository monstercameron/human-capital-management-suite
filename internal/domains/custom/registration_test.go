// TestTodo_WF_DATA_008 proves publishing a custom type registers its
// create, update, read and query capabilities in the tenant-owned namespace,
// that writes carry idempotency keys and a reversal contract, that no tenant
// can invoke another's capability or shadow a platform one, and that a
// workflow creates and reads a custom record through the registered surface.
package custom_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

var wfd8FixedTime = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func wfd8Def() custom.CustomObjectDefinition {
	classification := custom.FieldClassification{
		AuthZDomain: "worker.core", Classification: "INTERNAL",
		ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
	}
	return custom.CustomObjectDefinition{
		Kind: "VendorProfile", Namespace: "procurement", Version: 1,
		Fields: map[string]custom.FieldDefinition{
			"name":     {Type: "string", Classification: classification},
			"owner_id": {Type: "string", Classification: classification},
		},
	}
}

func wfd8Deps(store *custom.EventStore) custom.PublishDeps {
	return custom.PublishDeps{
		Store:  store,
		Access: &custom.AccessDeps{Platform: custom.AllowPlatformPolicy()},
		Clock:  func() time.Time { return wfd8FixedTime },
		Answer: wfd8Answer,
	}
}

// wfd8Answer is the test composition's runner-answer builder: the served
// composition passes its own builder through PublishDeps.Answer, and the
// runner only ever asserts the answer TYPE, which is real here.
func wfd8Answer(op custom.Operation, record custom.RecordResult, query custom.QueryResult, isQuery bool) (any, error) {
	strType := workflow.ValueType{Kind: workflow.KindString}
	if isQuery {
		ids := make([]string, 0, len(query.Report.Records))
		for _, rec := range query.Report.Records {
			ids = append(ids, rec.ObjectID)
		}
		return capabilityrunner.CapabilityAnswer{
			Outcome: workflow.OutcomeSucceeded,
			Outputs: map[string]capabilityrunner.ResolvedValue{
				"count":      {Type: strType, Text: fmt.Sprint(len(ids))},
				"object_ids": {Type: strType, Text: strings.Join(ids, ",")},
			},
			Detail: "queried " + strings.Join(ids, ","),
		}, nil
	}
	outputs := map[string]capabilityrunner.ResolvedValue{
		"object_id": {Type: strType, Text: record.ObjectID},
		"revision":  {Type: strType, Text: fmt.Sprint(record.Revision)},
		"retired":   {Type: strType, Text: "false"},
	}
	if record.Retired {
		outputs["retired"] = capabilityrunner.ResolvedValue{Type: strType, Text: "true"}
	}
	for name, tv := range record.Fields {
		outputs["field."+name] = capabilityrunner.ResolvedValue{Type: strType, Text: sprintValue(tv.Value)}
	}
	return capabilityrunner.CapabilityAnswer{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: outputs,
		Detail:  strings.ToLower(string(op)) + " " + record.ObjectID,
	}, nil
}

func sprintValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "?"
}

func wfd8RecordPayload(tenant, object string, fields map[string]string) custom.RecordPayload {
	return custom.RecordPayload{
		Tenant: tenant, Principal: "alice", ObjectID: object, Fields: fields,
		IdempotencyKey: "key-" + object, Actor: "alice",
		Purpose: "procurement.operations", EvidenceDigest: "evidence-" + object,
	}
}

func TestTodo_WF_DATA_008(t *testing.T) {
	ctx := context.Background()
	def := wfd8Def()
	if err := def.Validate(); err != nil {
		t.Fatalf("fixture definition: %v", err)
	}

	t.Run("publish_registers_four_tenant_namespaced_capabilities", func(t *testing.T) {
		reg := capability.NewRegistry()
		published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore()))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if len(published.Capabilities) != 4 {
			t.Fatalf("registered %d capabilities, want create/update/read/query", len(published.Capabilities))
		}
		wantIDs := []string{
			"tenant.acme.custom.procurement.vendorprofile.create",
			"tenant.acme.custom.procurement.vendorprofile.update",
			"tenant.acme.custom.procurement.vendorprofile.read",
			"tenant.acme.custom.procurement.vendorprofile.query",
		}
		for i, want := range wantIDs {
			got := published.Capabilities[i].ID
			if got != want {
				t.Fatalf("capability %d = %q, want %q", i, got, want)
			}
			rec, found := reg.Lookup(published.Capabilities[i].Key())
			if !found || rec.Status != capability.StatusActive {
				t.Fatalf("capability %q not actively registered", want)
			}
			if rec.Definition.OwnerDomain != "tenant.acme.custom" {
				t.Fatalf("capability %q owner = %q", want, rec.Definition.OwnerDomain)
			}
			if rec.Definition.Version != 1 {
				t.Fatalf("capability %q version = %d, want the pinned definition version", want, rec.Definition.Version)
			}
		}
	})

	t.Run("schemas_pin_the_definition_and_effects_follow_the_manifest", func(t *testing.T) {
		reg := capability.NewRegistry()
		published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore()))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		digest := def.Digest()
		for _, c := range published.Capabilities {
			for slot, ref := range map[string]capability.SchemaRef{"request": c.RequestSchema, "response": c.ResponseSchema} {
				if !strings.Contains(ref.SchemaID, digest) {
					t.Fatalf("%s %s schema %q does not pin the definition digest", c.ID, slot, ref.SchemaID)
				}
				if ref.Version != 1 {
					t.Fatalf("%s %s schema version = %d, want the pinned definition version", c.ID, slot, ref.Version)
				}
			}
			if c.TestRef != "TestTodo_WF_DATA_008" {
				t.Fatalf("%s test ref = %q", c.ID, c.TestRef)
			}
		}
		byID := map[string]capability.Definition{}
		for _, c := range published.Capabilities {
			byID[c.ID] = c
		}
		for _, id := range []string{
			"tenant.acme.custom.procurement.vendorprofile.create",
			"tenant.acme.custom.procurement.vendorprofile.update",
		} {
			if byID[id].EffectClass != capability.EffectInternalMutation {
				t.Fatalf("%s effect = %s, want the manifest write class", id, byID[id].EffectClass)
			}
			if byID[id].IdempotencyPolicyRef == "" {
				t.Fatalf("%s names no idempotency policy", id)
			}
			if len(byID[id].WriteData.DataDomains) == 0 {
				t.Fatalf("%s declares no write data domains", id)
			}
		}
		for _, id := range []string{
			"tenant.acme.custom.procurement.vendorprofile.read",
			"tenant.acme.custom.procurement.vendorprofile.query",
		} {
			if byID[id].EffectClass != capability.EffectReadOnly {
				t.Fatalf("%s effect = %s, want the manifest read class", id, byID[id].EffectClass)
			}
		}
	})

	t.Run("writes_carry_idempotency_keys_and_reversal_contracts", func(t *testing.T) {
		reg := capability.NewRegistry()
		published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore()))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		create := wfd8RecordPayload("acme", "vendor-1", map[string]string{"name": "Acme Parts", "owner_id": "alice"})
		bare := create
		bare.IdempotencyKey = ""
		if _, err := published.ExecuteRecord(ctx, custom.OperationCreate, bare); err == nil {
			t.Fatal("a write without an idempotency key must be refused")
		}
		first, err := published.ExecuteRecord(ctx, custom.OperationCreate, create)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if first.Replayed || first.Revision != 1 || first.Receipt.Head != 1 {
			t.Fatalf("first create result is wrong: %+v", first)
		}
		if first.Reversal == nil || first.Reversal.Operation != custom.OperationRetire ||
			first.Reversal.ObjectID != "vendor-1" || first.Reversal.ExpectedRevision != 1 {
			t.Fatalf("create carries no retire reversal: %+v", first.Reversal)
		}
		again, err := published.ExecuteRecord(ctx, custom.OperationCreate, create)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !again.Replayed || again.Receipt.Event.EventID != first.Receipt.Event.EventID {
			t.Fatal("replaying the idempotency key must return the first receipt")
		}
		change := wfd8RecordPayload("acme", "vendor-1", map[string]string{"name": "Acme Parts Ltd", "owner_id": "alice"})
		change.IdempotencyKey = "key-vendor-1-change"
		change.ExpectedRevision = 1
		changed, err := published.ExecuteRecord(ctx, custom.OperationChange, change)
		if err != nil {
			t.Fatalf("change: %v", err)
		}
		if changed.Revision != 2 || changed.Reversal == nil || changed.Reversal.Operation != custom.OperationCorrect {
			t.Fatalf("change carries no correct reversal: %+v", changed)
		}
		stale := change
		stale.IdempotencyKey = "key-vendor-1-stale"
		if _, err := published.ExecuteRecord(ctx, custom.OperationChange, stale); err == nil {
			t.Fatal("a change against a stale revision must be refused")
		}
	})

	t.Run("reversal_executes_the_inverse", func(t *testing.T) {
		reg := capability.NewRegistry()
		published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore()))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		created, err := published.ExecuteRecord(ctx, custom.OperationCreate, wfd8RecordPayload("acme", "vendor-9", map[string]string{"name": "Nine", "owner_id": "alice"}))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		reversed, err := published.ExecuteReversal(ctx, *created.Reversal, "alice", "procurement.operations", "evidence-reverse-9")
		if err != nil {
			t.Fatalf("reverse the create: %v", err)
		}
		if !reversed.Retired {
			t.Fatalf("reversed record must read retired: %+v", reversed)
		}
		readBack, err := published.ExecuteRead(ctx, wfd8RecordPayload("acme", "vendor-9", nil))
		if err == nil {
			t.Fatalf("retired record still reads: %+v", readBack)
		}
	})

	t.Run("publish_is_idempotent_and_rejects_conflicts", func(t *testing.T) {
		reg := capability.NewRegistry()
		deps := wfd8Deps(custom.NewEventStore())
		first, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", deps)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		second, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", deps)
		if err != nil {
			t.Fatalf("republishing the identical definition must succeed: %v", err)
		}
		if len(second.Capabilities) != len(first.Capabilities) {
			t.Fatal("republish returned a different capability set")
		}
		other := def
		other.Fields = map[string]custom.FieldDefinition{}
		for name, field := range def.Fields {
			other.Fields[name] = field
		}
		other.Fields["name"] = custom.FieldDefinition{Type: "string", Classification: custom.FieldClassification{
			AuthZDomain: "worker.core", Classification: "RESTRICTED",
			ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
		}}
		if _, err := custom.RegisterGeneratedCapabilities(reg, "acme", other, "procurement.operations", deps); err == nil {
			t.Fatal("a conflicting same-version definition must be refused")
		}
		if _, err := custom.RegisterGeneratedCapabilities(reg, "  ", def, "procurement.operations", deps); err == nil {
			t.Fatal("a blank tenant must be refused")
		}
		if _, err := custom.RegisterGeneratedCapabilities(reg, "acme corp", def, "procurement.operations", deps); err == nil {
			t.Fatal("a tenant key with whitespace must be refused")
		}
		broken := def
		broken.Kind = ""
		if _, err := custom.RegisterGeneratedCapabilities(reg, "acme", broken, "procurement.operations", deps); err == nil {
			t.Fatal("an invalid definition must be refused")
		}
		badDeps := deps
		badDeps.Store = nil
		if _, err := custom.RegisterGeneratedCapabilities(capability.NewRegistry(), "acme", def, "procurement.operations", badDeps); err == nil {
			t.Fatal("a nil store must be refused")
		}
	})
}

// wfd8Sink records gateway evidence with deterministic ids and keeps the
// last record for tenant assertions.
type wfd8Sink struct {
	called int
	last   capability.InvocationEvidence
}

func (s *wfd8Sink) RecordInvocation(_ context.Context, evt capability.InvocationEvidence) (string, error) {
	s.called++
	s.last = evt
	return fmt.Sprintf("ev-%d", s.called), nil
}

func (s *wfd8Sink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocation(ctx, evt)
}

func wfd8GatewayCode(t *testing.T, err error) string {
	t.Helper()
	var gateErr *capability.GatewayError
	if !errors.As(err, &gateErr) {
		t.Fatalf("expected a gateway refusal, got %v", err)
	}
	return gateErr.Code
}

// TestTodo_WF_DATA_008_Security proves a tenant capability cannot be invoked
// by another tenant and cannot shadow a platform capability.
func TestTodo_WF_DATA_008_Security(t *testing.T) {
	ctx := context.Background()
	def := wfd8Def()

	t.Run("cross_tenant_invoke_is_refused_at_the_scope_gate", func(t *testing.T) {
		reg := capability.NewRegistry()
		published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore()))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		sink := &wfd8Sink{}
		gateway := capability.NewGateway(reg, sink, capability.WithClock(func() time.Time { return wfd8FixedTime }))
		readKey := published.Capabilities[2].Key() // read
		_, err = gateway.Invoke(ctx, capability.InvokeRequest{
			Capability: readKey,
			Payload:    custom.RecordPayload{Tenant: "acme", ObjectID: "vendor-1", Purpose: "procurement.operations"},
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{"scope:tenant.other.custom"},
				Reason: "other tenant session", SubjectRef: "mallory", Tenant: "other",
			},
		})
		if code := wfd8GatewayCode(t, err); code != capability.CodeUnauthorized {
			t.Fatalf("cross-tenant invoke code = %s, want unauthorized", code)
		}
		if sink.last.Tenant != "other" || sink.last.Decision != "REFUSED" {
			t.Fatalf("refusal evidence does not name the calling tenant: %+v", sink.last)
		}
	})

	t.Run("forged_payload_tenant_is_refused_by_the_handler", func(t *testing.T) {
		reg := capability.NewRegistry()
		published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore()))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		forged := wfd8RecordPayload("other", "vendor-1", map[string]string{"name": "Smuggled", "owner_id": "mallory"})
		if _, err := published.Handler(custom.OperationCreate)(ctx, forged); err == nil {
			t.Fatal("a payload naming another tenant must be refused")
		} else if !strings.Contains(err.Error(), "tenant") {
			t.Fatalf("refusal must name the tenant wall, got: %v", err)
		}
		if _, err := published.ExecuteRead(ctx, wfd8RecordPayload("other", "vendor-1", nil)); err == nil {
			t.Fatal("a cross-tenant read must be refused")
		}
		if _, err := published.ExecuteQuery(ctx, custom.QueryPayload{Tenant: "other", Purpose: "procurement.operations"}); err == nil {
			t.Fatal("a cross-tenant query must be refused")
		}
	})

	t.Run("tenant_capabilities_cannot_shadow_platform_ones", func(t *testing.T) {
		reg, err := capability.NewBootstrapRegistry()
		if err != nil {
			t.Fatalf("bootstrap registry: %v", err)
		}
		before := len(reg.List())
		if _, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore())); err != nil {
			t.Fatalf("register alongside platform capabilities: %v", err)
		}
		if len(reg.List()) != before+4 {
			t.Fatal("tenant registration must add exactly four capabilities to the shared registry")
		}
		platform, found := reg.Lookup(capability.Key{ID: "hcmnext.registry.resolve_capability", Version: 1})
		if !found || platform.Definition.OwnerDomain != "registry" {
			t.Fatal("platform capability lost or re-owned after tenant registration")
		}
		squatter := capability.Definition{
			ID: "tenant.acme.custom.procurement.vendorprofile.read", Version: 1, OwnerDomain: "intruder",
			RequestSchema:  capability.SchemaRef{SchemaID: "x.request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			ResponseSchema: capability.SchemaRef{SchemaID: "x.response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			ErrorSchema:    capability.SchemaRef{SchemaID: "x.error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			EffectClass:    capability.EffectReadOnly, RiskClass: "LOW",
			IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:intruder",
			LegalBasisRef: "legal.test/v1", EntitlementRef: "entitlement.test/v1",
			SLOClassRef: "slo.test/v1", TestRef: "conformance:intruder/v1",
		}
		fresh := capability.NewRegistry()
		if err := fresh.Register(squatter, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
			t.Fatalf("seed squatter: %v", err)
		}
		if _, err := custom.RegisterGeneratedCapabilities(fresh, "acme", def, "procurement.operations", wfd8Deps(custom.NewEventStore())); err == nil {
			t.Fatal("registering over another owner's capability id must be refused")
		}
		if _, found := fresh.Lookup(capability.Key{ID: "tenant.acme.custom.procurement.vendorprofile.create", Version: 1}); found {
			t.Fatal("a refused publish must register nothing")
		}
	})
}

func wfd8StrType() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

// TestTodo_WF_DATA_008_Integration runs a workflow that creates and reads a
// custom record: the create travels the exact runner-to-handler call the
// served path will make, the read runs end to end through the real runner,
// gateway and registry, and the P1A write gate is asserted, not assumed.
func TestTodo_WF_DATA_008_Integration(t *testing.T) {
	ctx := context.Background()
	def := wfd8Def()
	reg := capability.NewRegistry()
	store := custom.NewEventStore()
	published, err := custom.RegisterGeneratedCapabilities(reg, "acme", def, "procurement.operations", wfd8Deps(store))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	byOp := map[custom.Operation]capability.Definition{}
	for i, op := range []custom.Operation{custom.OperationCreate, custom.OperationChange, custom.OperationRead, custom.OperationQuery} {
		byOp[op] = published.Capabilities[i]
	}
	queryDef := byOp[custom.OperationQuery]

	strInputs := func(texts map[string]string) map[string]capabilityrunner.ResolvedValue {
		out := make(map[string]capabilityrunner.ResolvedValue, len(texts))
		for k, v := range texts {
			out[k] = capabilityrunner.ResolvedValue{Type: wfd8StrType(), Text: v}
		}
		return out
	}

	// The workflow's create step: the exact CapabilityCall the runner builds,
	// served by the bound create handler. The P1A gateway refuses write
	// effects, so the test asserts that refusal and serves the call through
	// the handler the write gateway will front.
	createCall := capabilityrunner.CapabilityCall{
		NodeID: "create_vendor", Capability: byOp[custom.OperationCreate].Key(),
		OperationMode: workflow.ModeExecute, At: wfd8FixedTime,
		Inputs: strInputs(map[string]string{
			"tenant": "acme", "object_id": "vendor-1",
			"field.name": "Acme Parts", "field.owner_id": "alice",
			"idempotency_key": "key-vendor-1", "actor": "alice",
			"purpose": "procurement.operations", "evidence_digest": "evidence-vendor-1",
		}),
	}
	createdAny, err := published.Handler(custom.OperationCreate)(ctx, createCall)
	if err != nil {
		t.Fatalf("workflow create call: %v", err)
	}
	created, ok := createdAny.(capabilityrunner.CapabilityAnswer)
	if !ok {
		t.Fatalf("create answer is %T, want a runner answer", createdAny)
	}
	if created.Outcome != workflow.OutcomeSucceeded || created.Outputs["object_id"].Text != "vendor-1" {
		t.Fatalf("create answer is wrong: %+v", created)
	}
	sink := &wfd8Sink{}
	gateway := capability.NewGateway(reg, sink, capability.WithClock(func() time.Time { return wfd8FixedTime }))
	_, err = gateway.Invoke(ctx, capability.InvokeRequest{
		Capability: byOp[custom.OperationCreate].Key(), Payload: createCall,
		Authorization: capability.Authorization{
			Decision: capability.Allow, Scopes: []string{byOp[custom.OperationCreate].AuthZScopeRef},
			Reason: "workflow node scopes", SubjectRef: "alice", Tenant: "acme",
		},
		Invocation: &capability.Invocation{
			Purpose: "procurement.operations", Deadline: wfd8FixedTime.Add(time.Minute),
			IdempotencyKey:  "workflow:acme:instance:create_vendor:1",
			DeclaredEffects: []capability.EffectClass{capability.EffectInternalMutation},
		},
	})
	if code := wfd8GatewayCode(t, err); code != capability.CodeWriteEffectRefusedP1A {
		t.Fatalf("gateway create code = %s, want the P1A write refusal", code)
	}

	// The workflow's read step runs end to end through the real runner,
	// gateway, registry and handler.
	readDef := byOp[custom.OperationRead]
	rec, found := reg.Lookup(readDef.Key())
	if !found {
		t.Fatal("read capability not registered")
	}
	constant := func(target, value string) workflow.CompiledMapping {
		return workflow.CompiledMapping{
			Target: target, TargetType: wfd8StrType(), SourceKind: workflow.SourceConstant,
			SourceType: wfd8StrType(), Constant: value, PinnedInput: true,
		}
	}
	readNode := workflow.CompiledNode{
		ID:   "read_vendor",
		Type: workflow.StepCapability,
		Mappings: []workflow.CompiledMapping{
			constant("tenant", "acme"), constant("principal", "alice"),
			constant("object_id", "vendor-1"), constant("purpose", "procurement.operations"),
		},
		Capability: &workflow.CompiledCapability{
			ID: rec.Definition.ID, Version: rec.Definition.Version, Digest: rec.Digest,
			Status: rec.Status, OwnerDomain: rec.Definition.OwnerDomain,
			EffectClass: rec.Definition.EffectClass, AuthZScopeRef: rec.Definition.AuthZScopeRef,
			OperationMode: workflow.ModeExecute, AuthorityScopes: []string{rec.Definition.AuthZScopeRef},
		},
		EffectClass: capability.EffectReadOnly,
	}
	runner := &capabilityrunner.Runner{
		Gateway: gateway, SubjectRef: "alice", Tenant: "acme",
		Purpose: "procurement.operations", Now: func() time.Time { return wfd8FixedTime },
	}
	outcome, refs, err := runner.Run(ctx, execute.StepRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(), Attempt: 1,
		Node: readNode, RecordedAt: wfd8FixedTime,
	})
	if err != nil {
		t.Fatalf("workflow read step: %v", err)
	}
	if outcome.Outcome != workflow.OutcomeSucceeded || refs.CapabilityExecutionID == "" {
		t.Fatalf("read outcome is wrong: %+v %+v", outcome, refs)
	}
	fields := map[string]string{}
	for _, v := range outcome.Outputs.Values {
		fields[v.Path] = v.Value.Text
	}
	if fields["field.name"] != "Acme Parts" || fields["object_id"] != "vendor-1" {
		t.Fatalf("read outputs are wrong: %v", fields)
	}
	if sink.last.Tenant != "acme" || sink.last.Decision != "INVOKED" {
		t.Fatalf("read evidence is wrong: %+v", sink.last)
	}

	// The workflow's query step lists through the bound query handler.
	queryCall := capabilityrunner.CapabilityCall{
		NodeID: "query_vendors", Capability: queryDef.Key(),
		OperationMode: workflow.ModeExecute, At: wfd8FixedTime,
		Inputs: strInputs(map[string]string{
			"tenant": "acme", "principal": "alice", "purpose": "procurement.operations",
			"filter.name": "Acme Parts",
		}),
	}
	queriedAny, err := published.Handler(custom.OperationQuery)(ctx, queryCall)
	if err != nil {
		t.Fatalf("workflow query call: %v", err)
	}
	queried, ok := queriedAny.(capabilityrunner.CapabilityAnswer)
	if !ok {
		t.Fatalf("query answer is %T, want a runner answer", queriedAny)
	}
	if queried.Outputs["count"].Text != "1" || queried.Outputs["object_ids"].Text != "vendor-1" {
		t.Fatalf("query answer is wrong: %+v", queried)
	}

	// A workflow running as another tenant is refused at the handler wall.
	evilNode := readNode
	evilNode.ID = "read_vendor_evil"
	evilNode.Mappings = []workflow.CompiledMapping{
		constant("tenant", "other"), constant("principal", "mallory"),
		constant("object_id", "vendor-1"), constant("purpose", "procurement.operations"),
	}
	_, _, err = runner.Run(ctx, execute.StepRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(), Attempt: 1,
		Node: evilNode, RecordedAt: wfd8FixedTime,
	})
	if err == nil {
		t.Fatal("a read naming another tenant must be refused")
	} else if !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("refusal must name the tenant wall, got: %v", err)
	}
}
