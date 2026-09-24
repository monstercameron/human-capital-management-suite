package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/organization"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type parameterGraphStub struct {
	snapshot ParameterOrganizationSnapshot
	err      error
	tenant   string
}

func (s *parameterGraphStub) LoadParameterOrganizationGraph(_ context.Context, tenant string, _ time.Time) (ParameterOrganizationSnapshot, error) {
	s.tenant = tenant
	return s.snapshot, s.err
}

func parameterPrincipal(t *testing.T, tenant, subject, org string, roles ...string) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: org, Roles: roles, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-parameter-test",
		IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential-parameter-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_WF_DATA_036_ActiveDefinitionsComeFromActivatedCellRegistry(t *testing.T) {
	registry := configregistry.NewRegistry()
	tenantID := uuid.New()
	consumer := config.Consumer{Kind: config.ConsumerWorkflow, ID: "workflow.payroll"}
	catalog := parameterCatalogEnvelope{Version: 1, Definitions: []config.ParameterDefinition{{
		Key: "payroll.currency", Type: workflow.ValueType{Kind: workflow.KindString},
		Classification: "TENANT_OPERATIONAL", Owner: "payroll", Required: true,
		AllowedConsumers: []config.Consumer{consumer},
	}}}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	object, err := configregistry.Publish(registry, configregistry.ConfigurationObject{
		Kind: configregistry.KindReference, ID: TenantParameterCatalogID, Revision: 3, Body: body,
		SchemaRef: "hcmnext.tenant-parameters/v1", Scope: configregistry.Scope{TenantID: tenantID.String()},
		PublisherPrincipal: "principal:config-admin", PublishedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configregistry.Activate(registry, object.Ref(), configregistry.ActivationEvidence{ActivatedBy: "principal:config-admin", ActivatedAt: at}); err != nil {
		t.Fatal(err)
	}
	source := ActiveRegistryParameterDefinitions{Registry: registry, TenantUUID: func(key string) uuid.UUID {
		if key == "tenant-key" {
			return tenantID
		}
		return uuid.Nil
	}}
	snapshot, err := source.LoadParameterSnapshot(context.Background(), "tenant-key")
	if err != nil {
		t.Fatal(err)
	}
	definition, found := snapshot.ParameterDefinition("payroll.currency")
	if !found || snapshot.Version != "3" || definition.Type.Kind != workflow.KindString || definition.AllowedConsumers[0] != consumer {
		t.Fatalf("active catalog snapshot=%+v definition=%+v found=%v", snapshot, definition, found)
	}
	if _, err := (ActiveRegistryParameterDefinitions{Registry: registry, TenantUUID: source.TenantUUID}).LoadParameterSnapshot(context.Background(), "unmapped"); !errors.Is(err, ErrParameterCatalogUnavailable) {
		t.Fatalf("unmapped tenant error=%v", err)
	}
	if _, err := (ActiveRegistryParameterDefinitions{Registry: registry, TenantUUID: func(string) uuid.UUID { return uuid.New() }}).LoadParameterSnapshot(context.Background(), "other"); !errors.Is(err, ErrParameterCatalogUnavailable) {
		t.Fatalf("unactivated tenant catalog error=%v", err)
	}
}

func TestTodo_WF_DATA_037_TrustedOrganizationScopeAndWriteAuthorization(t *testing.T) {
	tenant, root, child := "tenant-key", uuid.New().String(), uuid.New().String()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	graph := &parameterGraphStub{snapshot: ParameterOrganizationSnapshot{Graph: organization.Snapshot{
		Tenant: "physical-tenant", Watermark: "org:v1", ResolverPolicyVersion: "v1",
		Units: []organization.OrganizationUnit{
			{ID: root, Tenant: "physical-tenant", Type: "BUSINESS_UNIT", EffectiveFrom: at.Add(-time.Hour)},
			{ID: child, Tenant: "physical-tenant", Type: "TEAM", EffectiveFrom: at.Add(-time.Hour)},
		},
		Edges: []organization.RelationshipEdge{{ID: "parent", Tenant: "physical-tenant", Source: root, Target: child, Type: organization.Hierarchy, EffectiveFrom: at.Add(-time.Hour)}},
	}}}
	clock := fixedParameterClock{at: at}
	authority := OrganizationParameterScopes{Graph: graph, Clock: clock}
	identity := config.ParameterIdentity{Tenant: tenant, Subject: "admin", OrgScope: child}
	ctx := trust.WithPrincipal(context.Background(), parameterPrincipal(t, tenant, identity.Subject, child, "hcm_admin"))
	path, err := authority.ResolveParameterPath(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	want := config.ParameterScopePath{{Kind: config.ScopeTenant, ID: tenant}, {Kind: config.ScopeOrganization, ID: root}, {Kind: config.ScopeOrganization, ID: child}}
	if len(path) != len(want) {
		t.Fatalf("derived path=%+v want %+v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("derived path=%+v want %+v", path, want)
		}
	}
	if graph.tenant != tenant {
		t.Fatalf("graph tenant=%q, want authenticated logical tenant", graph.tenant)
	}
	definition := config.ParameterDefinition{Key: "payroll.currency"}
	writePath, err := authority.AuthorizeParameterWrite(ctx, identity, definition, want[1], false)
	if err != nil || len(writePath) != 2 || writePath[1] != want[1] {
		t.Fatalf("authorized ancestor write path=%+v err=%v", writePath, err)
	}
	if _, err := authority.AuthorizeParameterWrite(ctx, identity, definition, config.ParameterScope{Kind: config.ScopeCompany, ID: "company-1"}, false); !errors.Is(err, config.ErrParameterScopePathInvalid) {
		t.Fatalf("unsupported company path error=%v", err)
	}
	workerIdentity := config.ParameterIdentity{Tenant: tenant, Subject: "worker", OrgScope: child}
	workerCtx := trust.WithPrincipal(context.Background(), parameterPrincipal(t, tenant, workerIdentity.Subject, child, "manager"))
	if _, err := authority.AuthorizeParameterWrite(workerCtx, workerIdentity, definition, want[2], true); !errors.Is(err, ErrParameterWriteDenied) {
		t.Fatalf("unprivileged lock write error=%v", err)
	}
	wrongIdentity := identity
	wrongIdentity.Tenant = "other-tenant"
	if _, err := authority.ResolveParameterPath(ctx, wrongIdentity); !errors.Is(err, config.ErrParameterIdentityUnavailable) {
		t.Fatalf("mismatched authenticated identity error=%v", err)
	}
}

func TestTodo_WF_DATA_037_LegalEntityScopesComeFromTrustedTenantBindings(t *testing.T) {
	tenant, root, child := "tenant-key", uuid.New().String(), uuid.New().String()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	legalRoot, legalChild := uuid.New().String(), uuid.New().String()
	graph := &parameterGraphStub{snapshot: ParameterOrganizationSnapshot{
		Graph: organization.Snapshot{
			Tenant: "physical-tenant", Watermark: "org:v1", ResolverPolicyVersion: "v1",
			Units: []organization.OrganizationUnit{
				{ID: root, Tenant: "physical-tenant", Type: "BUSINESS_UNIT", EffectiveFrom: at.Add(-time.Hour)},
				{ID: child, Tenant: "physical-tenant", Type: "TEAM", EffectiveFrom: at.Add(-time.Hour)},
			},
			Edges: []organization.RelationshipEdge{{ID: "parent", Tenant: "physical-tenant", Source: root, Target: child, Type: organization.Hierarchy, EffectiveFrom: at.Add(-time.Hour)}},
		},
		LegalEntityByOrganization: map[string]string{root: legalRoot, child: legalChild},
	}}
	identity := config.ParameterIdentity{Tenant: tenant, Subject: "admin", OrgScope: child}
	ctx := trust.WithPrincipal(context.Background(), parameterPrincipal(t, tenant, identity.Subject, child, "hcm_admin"))
	authority := OrganizationParameterScopes{Graph: graph, Clock: fixedParameterClock{at: at}}
	path, err := authority.ResolveParameterPath(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	want := config.ParameterScopePath{
		{Kind: config.ScopeTenant, ID: tenant},
		{Kind: config.ScopeLegalEntity, ID: legalRoot},
		{Kind: config.ScopeOrganization, ID: root},
		{Kind: config.ScopeLegalEntity, ID: legalChild},
		{Kind: config.ScopeOrganization, ID: child},
	}
	if len(path) != len(want) {
		t.Fatalf("derived path=%+v want %+v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Fatalf("derived path=%+v want %+v", path, want)
		}
	}
	written, err := authority.AuthorizeParameterWrite(ctx, identity, config.ParameterDefinition{Key: "payroll.currency"}, config.ParameterScope{Kind: config.ScopeLegalEntity, ID: legalRoot}, false)
	if err != nil || len(written) != 2 || written[1].ID != legalRoot {
		t.Fatalf("legal-entity write path=%+v err=%v", written, err)
	}
}

func TestTodo_WF_DATA_037_LegalEntityBindingsFailClosed(t *testing.T) {
	resolved := uuid.New().String()
	for name, links := range map[string][]parameterLegalEntityLink{
		"missing entity":      {{OrganizationID: "org", LegalEntityRef: resolved}},
		"cross tenant entity": {{OrganizationID: "org", LegalEntityRef: resolved, ResolvedLegalEntity: stringPointer(uuid.New().String())}},
		"ambiguous entity":    {{OrganizationID: "org", LegalEntityRef: resolved, ResolvedLegalEntity: stringPointer(resolved)}, {OrganizationID: "org", LegalEntityRef: uuid.New().String(), ResolvedLegalEntity: stringPointer(uuid.New().String())}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parameterLegalEntityBindings(links); !errors.Is(err, ErrParameterScopeUnavailable) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

type fixedParameterClock struct{ at time.Time }

func (c fixedParameterClock) Now(context.Context) (time.Time, error) { return c.at, nil }

func TestTodo_WF_DATA_037_CellEnvironmentDoesNotAcceptRequestValues(t *testing.T) {
	configured, err := NewCellParameterEnvironment("SANDBOX")
	if err != nil || configured.Environment != config.EnvironmentSandbox {
		t.Fatalf("deployment environment=%+v err=%v", configured, err)
	}
	if _, err := NewCellParameterEnvironment(""); err == nil {
		t.Fatal("empty deployment environment selected an implicit value namespace")
	}
	if _, err := NewCellParameterEnvironment("sandbox"); err == nil {
		t.Fatal("noncanonical deployment environment was accepted")
	}
	cell := CellParameterEnvironment{Environment: config.EnvironmentSandbox}
	got, err := cell.ResolveParameterEnvironment(context.Background())
	if err != nil || got != config.EnvironmentSandbox {
		t.Fatalf("cell environment=%q err=%v", got, err)
	}
	if _, err := (CellParameterEnvironment{}).ResolveParameterEnvironment(context.Background()); !errors.Is(err, config.ErrParameterValueInvalid) {
		t.Fatalf("unset environment error=%v", err)
	}
}
