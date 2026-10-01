package agenteval

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT2_025_ComponentCatalogRequiresPhysicalIdentity(t *testing.T) {
	const tenant values.TenantId = "synthetic-catalog"
	tenantUUID := uuid.New()
	components := catalogAttestedComponents(tenantUUID)
	ids := make([]string, len(components))
	for i, component := range components {
		identity, err := component.Attestor.AttestSyntheticRuntimeComponent(context.Background(), tenant, tenantUUID)
		if err != nil {
			t.Fatal(err)
		}
		ids[i], err = SyntheticRuntimeComponentID(identity)
		if err != nil {
			t.Fatal(err)
		}
	}
	record := agentstore.SyntheticTenantProvisionRecord{
		TenantID: tenantUUID, SuiteTenantID: tenant.String(), GrantStoreID: ids[0], TaskStoreID: ids[1],
		BudgetLedgerID: ids[2], AuditStoreID: ids[3], ToolOwnerID: ids[4],
	}
	catalog, err := NewSyntheticRuntimeIdentityCatalog(components)
	if err != nil {
		t.Fatalf("NewSyntheticRuntimeIdentityCatalog() error = %v", err)
	}
	got, err := catalog.Resolve(context.Background(), tenant, tenantUUID, record)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Grant != components[0].Component || got.Task != components[1].Component || got.Budget != components[2].Component || got.Audit != components[3].Component || got.Owner != components[4].Component {
		t.Fatalf("resolved components do not match physical attestations: %+v", got)
	}

	tests := []struct {
		name   string
		mutate func([]SyntheticRuntimeComponent)
	}{
		{name: "tenant mismatch", mutate: func(c []SyntheticRuntimeComponent) {
			c[0].Attestor = catalogAttestor{identity: SyntheticRuntimePhysicalIdentity{Role: SyntheticGrantStoreRole, TenantID: uuid.New(), BackendID: "agent-db/grant-store/grant-a"}}
		}},
		{name: "wrong physical identity", mutate: func(c []SyntheticRuntimeComponent) {
			c[0].Attestor = catalogAttestor{identity: SyntheticRuntimePhysicalIdentity{Role: SyntheticGrantStoreRole, TenantID: tenantUUID, BackendID: "attacker-selected-backend"}}
		}},
		{name: "backend reused", mutate: func(c []SyntheticRuntimeComponent) {
			c[1].Attestor = catalogAttestor{identity: SyntheticRuntimePhysicalIdentity{Role: SyntheticTaskStoreRole, TenantID: tenantUUID, BackendID: "agent-db/grant-store/grant-a"}}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changed := append([]SyntheticRuntimeComponent(nil), components...)
			tc.mutate(changed)
			candidate, err := NewSyntheticRuntimeIdentityCatalog(changed)
			if err != nil {
				t.Fatalf("construct catalog: %v", err)
			}
			if _, err := candidate.Resolve(context.Background(), tenant, tenantUUID, record); !errors.Is(err, ErrSyntheticRuntimeIdentity) {
				t.Fatalf("Resolve() error = %v, want physical identity denial", err)
			}
		})
	}

	if _, err := NewSyntheticRuntimeIdentityCatalog([]SyntheticRuntimeComponent{{Component: new(int)}}); !errors.Is(err, ErrSyntheticRuntimeIdentity) {
		t.Fatalf("catalog accepted component without attestor: %v", err)
	}
	if _, err := catalog.Resolve(context.Background(), tenant, uuid.New(), record); !errors.Is(err, ErrSyntheticRuntimeIdentity) {
		t.Fatalf("Resolve() accepted a mismatched provision tenant: %v", err)
	}
}

func catalogAttestedComponents(tenant uuid.UUID) []SyntheticRuntimeComponent {
	definitions := []struct {
		role SyntheticRuntimeComponentRole
		id   string
	}{
		{SyntheticGrantStoreRole, "grant-a"}, {SyntheticTaskStoreRole, "task-a"},
		{SyntheticBudgetRole, "budget-a"}, {SyntheticAuditRole, "audit-a"}, {SyntheticToolOwnerRole, "owner-a"},
	}
	components := make([]SyntheticRuntimeComponent, 0, len(definitions))
	for _, definition := range definitions {
		components = append(components, SyntheticRuntimeComponent{
			Attestor: catalogAttestor{identity: SyntheticRuntimePhysicalIdentity{
				Role: definition.role, TenantID: tenant,
				BackendID: "agent-db/" + string(definition.role) + "/" + definition.id,
			}},
			Component: new(string),
		})
	}
	return components
}

type catalogAttestor struct {
	identity SyntheticRuntimePhysicalIdentity
}

func (a catalogAttestor) AttestSyntheticRuntimeComponent(context.Context, values.TenantId, uuid.UUID) (SyntheticRuntimePhysicalIdentity, error) {
	return a.identity, nil
}
