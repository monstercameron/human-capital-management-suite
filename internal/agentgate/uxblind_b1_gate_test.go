package agentgate

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const (
	testPurpose = "workforce:read"
	testTenant  = values.TenantId("tenant-a")
	testOrg     = "org-a"
)

type mutableGrants struct{ grants []SkillGrant }

func (m *mutableGrants) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]SkillGrant, error) {
	var out []SkillGrant
	for _, grant := range m.grants {
		if grant.Tenant == tenant && grant.Skill == key {
			out = append(out, cloneGrant(grant))
		}
	}
	return out, nil
}

type recordingPDP struct {
	decision CapabilityDecision
	calls    []CapabilityRequest
	err      error
}

func (p *recordingPDP) Authorize(_ context.Context, req CapabilityRequest) (CapabilityDecision, error) {
	p.calls = append(p.calls, req)
	if p.err != nil {
		return CapabilityDecision{}, p.err
	}
	return cloneCapabilityDecision(p.decision), nil
}

func testCapability() capability.Definition {
	return capability.Definition{
		ID: "people.read_worker", Version: 1, OwnerDomain: "people",
		RequestSchema:  capability.SchemaRef{SchemaID: "worker.request", Version: 1, ProtobufFullName: "test.WorkerRequest"},
		ResponseSchema: capability.SchemaRef{SchemaID: "worker.response", Version: 1, ProtobufFullName: "test.WorkerResponse"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "worker.error", Version: 1, ProtobufFullName: "test.WorkerError"},
		EffectClass:    capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{
			DataDomains: []string{"worker"}, FieldPaths: []string{"worker.worker_number"},
		}, RiskClass: "LOW", IdempotencyPolicyRef: "read-only", AgentEligible: true,
		AuthZScopeRef: "scope:people.read", LegalBasisRef: "legal:workforce", EntitlementRef: "entitlement:workforce", SLOClassRef: "slo:standard", TestRef: "test:people.read_worker",
	}
}

type gateFixture struct {
	gate    *Gate
	skills  *agentskills.Registry
	user    UserContext
	subject Subject
	key     agentskills.SkillKey
	pdp     *recordingPDP
	grants  *mutableGrants
	now     time.Time
}

func newGateFixture(t *testing.T) gateFixture {
	t.Helper()
	now := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	def := testCapability()
	capabilities := capability.NewRegistry()
	if err := capabilities.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatalf("register capability: %v", err)
	}
	skills := agentskills.NewRegistry(capabilities)
	skill := agentskills.SkillDefinition{
		ID: "hcmnext.skill.worker_state", Version: 1, Owner: "people", Description: "Read worker state.",
		InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{"type":"object"}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: def.Key()}},
		SideEffectTier: agentskills.TierRead, RequiredPurposes: []string{testPurpose}, IdempotencyRule: "read-only", CostClass: "LOW",
	}
	if err := skills.Publish(skill); err != nil {
		t.Fatalf("publish skill: %v", err)
	}
	key := skill.Key()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: testTenant, Subject: "user-1", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: testOrg,
		Roles: []string{"manager"}, Purposes: []string{testPurpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-1",
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	user := UserContext{Principal: principal, Population: "managers", Roles: []string{"manager"}, OrganizationScopes: []string{testOrg}}
	subjectRef := values.EntityRef{Tenant: testTenant, Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}
	subject := Subject{Ref: subjectRef, Organization: authz.OrgUnitRef{Tenant: testTenant, ID: testOrg}}
	pdp := &recordingPDP{decision: CapabilityDecision{Capability: def.Key(), Allowed: true, Subjects: []SubjectDecision{{Subject: subjectRef, Fields: map[authz.FieldID]authz.Effect{authz.FieldWorkerNumber: authz.EffectAllow, authz.FieldBaseSalary: authz.EffectRedacted}}}}}
	grants := &mutableGrants{grants: []SkillGrant{{ID: "grant-manager", Tenant: testTenant, Skill: key, Roles: []string{"manager"}, Population: "managers", OrganizationScopes: []string{testOrg}, Purposes: []string{testPurpose}}}}
	gate, err := New(Config{Skills: skills, Grants: grants, PDP: pdp, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new gate: %v", err)
	}
	return gateFixture{gate: gate, skills: skills, user: user, subject: subject, key: key, pdp: pdp, grants: grants, now: now}
}

func (f gateFixture) request() CallRequest {
	return CallRequest{User: f.user, Actor: AgentActor{AgentVersion: "agent-v1", InstallationID: "install-1", RunID: "run-1", StepID: "step-1"}, Skill: agentskills.SkillPin{ID: f.key.ID, Version: f.key.Version, Digest: f.skillDigest()}, Purpose: testPurpose, Subjects: []Subject{f.subject}, Fields: []authz.FieldID{authz.FieldWorkerNumber}, At: f.now}
}

func (f gateFixture) skillDigest() string {
	record, ok := f.gate.skills.List()[0], true
	if !ok {
		return ""
	}
	return record.Digest
}

func deniedCode(t *testing.T, err error) DenialCode {
	t.Helper()
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %v, want *DeniedError", err)
	}
	return denied.Code
}

func TestTodo_AGENT2_005(t *testing.T) {
	f := newGateFixture(t)
	request := f.request()
	decision, err := f.gate.Authorize(context.Background(), request)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if decision.GrantID != "grant-manager" || len(decision.Capabilities) != 1 || len(f.pdp.calls) != 1 {
		t.Fatalf("decision = %+v, PDP calls = %d", decision, len(f.pdp.calls))
	}

	f.grants.grants[0].Roles = []string{"employee"}
	if _, err := f.gate.Authorize(context.Background(), request); deniedCode(t, err) != DenyRole {
		t.Fatalf("role change was not rechecked: %v", err)
	}
}

func TestTodo_AGENT2_005_Golden(t *testing.T) {
	f := newGateFixture(t)
	decision, err := f.gate.Authorize(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Skill":{"ID":"hcmnext.skill.worker_state","Version":1},"GrantID":"grant-manager","EvaluatedAt":"2026-09-28T18:00:00Z","Purpose":"workforce:read","Capabilities":[{"Capability":{"ID":"people.read_worker","Version":1},"Allowed":true,"Subjects":[{"Subject":"eref:v1:tenant-a:worker:00000000-0000-4000-8000-000000000001","Fields":{"worker.worker_number":1}}],"Reason":""}],"Subjects":[{"Subject":"eref:v1:tenant-a:worker:00000000-0000-4000-8000-000000000001","Fields":{"worker.worker_number":1}}]}`
	if string(encoded) != want {
		t.Fatalf("golden = %s, want %s", encoded, want)
	}
}

func TestTodo_AGENT2_005_Security(t *testing.T) {
	cases := []struct {
		name string
		edit func(*gateFixture, *CallRequest)
		want DenialCode
	}{
		{name: "wrong organization", edit: func(f *gateFixture, req *CallRequest) { req.User.OrganizationScopes = []string{"org-other"} }, want: DenyOrganization},
		{name: "wrong purpose", edit: func(_ *gateFixture, req *CallRequest) { req.Purpose = "payroll:write" }, want: DenyPurpose},
		{name: "wrong population", edit: func(_ *gateFixture, req *CallRequest) { req.User.Population = "employees" }, want: DenyPopulation},
		{name: "consent missing", edit: func(f *gateFixture, _ *CallRequest) { f.grants.grants[0].ConsentRequired = true }, want: DenyConsent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newGateFixture(t)
			req := fixture.request()
			tc.edit(&fixture, &req)
			if _, err := fixture.gate.Authorize(context.Background(), req); deniedCode(t, err) != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if len(fixture.pdp.calls) != 0 {
				t.Fatal("PDP was called after grant denial")
			}
		})
	}
}

func TestTodo_AGENT2_005_Property(t *testing.T) {
	f := newGateFixture(t)
	result, err := f.gate.Execute(context.Background(), f.request(), func(context.Context, CallDecision) (Result, error) {
		return Result{Subjects: []ResultSubject{{Subject: f.subject.Ref, Fields: map[authz.FieldID]any{
			authz.FieldWorkerNumber: "W-1", authz.FieldBaseSalary: 100000, authz.FieldTaxID: "must-not-cross",
		}}}}, nil
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !reflect.DeepEqual(result.Subjects[0].Fields, map[authz.FieldID]any{authz.FieldWorkerNumber: "W-1"}) {
		t.Fatalf("filtered result = %+v", result)
	}
	for field := range result.Subjects[0].Fields {
		if field != authz.FieldWorkerNumber {
			t.Fatalf("result contains field outside user-visible set: %s", field)
		}
	}
}

func TestTodo_AGENT2_005_Mutation(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*gateFixture, *CallRequest)
	}{
		{name: "remove role check", edit: func(f *gateFixture, _ *CallRequest) { f.grants.grants[0].Roles = []string{"employee"} }},
		{name: "remove organization check", edit: func(f *gateFixture, _ *CallRequest) { f.grants.grants[0].OrganizationScopes = []string{"org-other"} }},
		{name: "remove purpose check", edit: func(f *gateFixture, req *CallRequest) { req.Purpose = "payroll:write" }},
		{name: "remove subject check", edit: func(f *gateFixture, req *CallRequest) { req.Subjects[0].Ref.Tenant = "tenant-other" }},
		{name: "remove field check", edit: func(f *gateFixture, req *CallRequest) {
			req.Fields[0] = authz.FieldBaseSalary
			f.pdp.decision.Subjects[0].Fields = map[authz.FieldID]authz.Effect{authz.FieldBaseSalary: authz.EffectDenied}
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			f := newGateFixture(t)
			req := f.request()
			mutation.edit(&f, &req)
			if _, err := f.gate.Authorize(context.Background(), req); err == nil {
				t.Fatal("mutated authorization unexpectedly succeeded")
			}
		})
	}
}

func TestTodo_AGENT2_005_ConsentLifecycle(t *testing.T) {
	f := newGateFixture(t)
	f.grants.grants[0].ConsentRequired = true
	checks := 0
	gate, err := New(Config{Skills: f.gate.skills, Grants: f.grants, PDP: f.pdp, Now: func() time.Time { return f.now }, Consent: ConsentCheckerFunc(func(_ context.Context, req ConsentRequest) error {
		checks++
		if req.Purpose != testPurpose || len(req.Subjects) != 1 {
			t.Fatalf("consent request = %+v", req)
		}
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Authorize(context.Background(), f.request()); err != nil {
		t.Fatalf("active consent denied: %v", err)
	}
	if checks != 1 {
		t.Fatalf("consent checks = %d, want one per call", checks)
	}
}

func TestTodo_AGENT2_005_Discovery(t *testing.T) {
	f := newGateFixture(t)
	available, err := f.gate.Discover(context.Background(), DiscoveryRequest{User: f.user, Purpose: testPurpose, At: f.now})
	if err != nil || len(available) != 1 || available[0].Definition.Key() != f.key {
		t.Fatalf("discovery = %+v, err = %v", available, err)
	}
	if len(f.pdp.calls) != 1 {
		t.Fatalf("discovery PDP calls = %d, want one per underlying capability", len(f.pdp.calls))
	}
	f.grants.grants[0].Roles = []string{"employee"}
	available, err = f.gate.Discover(context.Background(), DiscoveryRequest{User: f.user, Purpose: testPurpose, At: f.now})
	if err != nil || len(available) != 0 {
		t.Fatalf("discovery after role change = %+v, err = %v", available, err)
	}
}

func TestTodo_AGENT2_005_DefaultPDP(t *testing.T) {
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	workerID := "00000000-0000-4000-8000-000000000002"
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: testTenant, Subject: workerID, SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: testOrg,
		Roles: []string{"worker_self"}, Purposes: []string{authz.PurposeSelfService}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-self", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential-self",
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := values.EntityRef{Tenant: testTenant, Kind: values.Kind("worker"), Id: workerID}
	decision, err := (AuthorizationPDP{}).Authorize(context.Background(), CapabilityRequest{
		Principal: principal, Roles: []string{"worker_self"}, Capability: capability.Record{Definition: testCapability()},
		Purpose: authz.PurposeSelfService, Subjects: []Subject{{Ref: ref, Organization: authz.OrgUnitRef{Tenant: testTenant, ID: testOrg}}},
		Fields: []authz.FieldID{authz.FieldWorkerNumber}, EffectiveAt: at,
	})
	if err != nil || !decision.Allowed || decision.Subjects[0].Fields[authz.FieldWorkerNumber] != authz.EffectAllow {
		t.Fatalf("default PDP = %+v, err = %v", decision, err)
	}
	_, err = (AuthorizationPDP{}).Authorize(context.Background(), CapabilityRequest{
		Principal: principal, Roles: []string{"worker_self"}, Capability: capability.Record{Definition: testCapability()},
		Purpose: authz.PurposeSelfService, Subjects: []Subject{{Ref: ref, Organization: authz.OrgUnitRef{Tenant: testTenant, ID: testOrg}}},
		Fields: []authz.FieldID{authz.FieldBaseSalary}, EffectiveAt: at,
	})
	if deniedCode(t, err) != DenyField {
		t.Fatalf("default PDP capability field expansion = %v", err)
	}
}

func TestTodo_AGENT2_005_FailClosedInputs(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty config error = %v", err)
	}
	if ConsentCheckerFunc(nil).Check(context.Background(), ConsentRequest{}) == nil {
		t.Fatal("nil consent checker unexpectedly allowed")
	}
	var denied *DeniedError
	err := &DeniedError{Code: DenyRole, Detail: "role"}
	if !errors.Is(err, ErrDenied) || err.Error() == "" || !errors.As(err, &denied) || denied.Code != DenyRole {
		t.Fatalf("typed denial = %v", err)
	}
	f := newGateFixture(t)
	request := f.request()
	request.Skill.Digest = "tampered"
	request.Actor.RunID = ""
	if _, err := f.gate.Authorize(context.Background(), request); err == nil {
		t.Fatal("invalid actor or pin unexpectedly allowed")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.gate.Authorize(cancelled, f.request()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled authorization = %v", err)
	}
}

func TestTodo_AGENT2_005_ResultBoundary(t *testing.T) {
	f := newGateFixture(t)
	decision, err := f.gate.Authorize(context.Background(), f.request())
	if err != nil {
		t.Fatal(err)
	}
	unexpected := Result{Subjects: []ResultSubject{{Subject: values.EntityRef{Tenant: testTenant, Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000003"}, Fields: map[authz.FieldID]any{authz.FieldWorkerNumber: "other"}}}}
	if _, err := FilterResult(decision, unexpected); deniedCode(t, err) != DenySubject {
		t.Fatalf("unexpected result subject = %v", err)
	}
	if effectRank(authz.EffectRedacted) >= effectRank(authz.EffectAllow) || effectRank(authz.EffectWithheld) >= effectRank(authz.EffectDenied) {
		t.Fatal("field effect order is not restrictive")
	}
}

func TestTodo_AGENT2_005_PDPFailureIsTyped(t *testing.T) {
	f := newGateFixture(t)
	f.pdp.err = errors.New("dependency unavailable")
	_, err := f.gate.Authorize(context.Background(), f.request())
	if deniedCode(t, err) != DenyCapability || !errors.Is(err, ErrDenied) {
		t.Fatalf("PDP failure = %v", err)
	}
}
