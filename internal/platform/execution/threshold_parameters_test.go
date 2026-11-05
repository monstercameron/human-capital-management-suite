package execution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/configparamstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type stubThresholdSnapshots struct {
	snapshot config.Snapshot
	err      error
}

func (s stubThresholdSnapshots) LoadParameterSnapshot(context.Context, string) (config.Snapshot, error) {
	if s.err != nil {
		return config.Snapshot{}, s.err
	}
	return s.snapshot, nil
}

type stubThresholdRevisions struct {
	values map[string]config.ParameterValueResolution
	err    error
}

func (s stubThresholdRevisions) AppendParameterRevision(context.Context, string, config.Snapshot, config.ParameterValueChange) (config.ParameterValueRevision, error) {
	return config.ParameterValueRevision{}, s.err
}

func (s stubThresholdRevisions) ResolveParameterValue(_ context.Context, _ string, _ config.Snapshot, key string, _ config.ParameterScopePath, _ config.ParameterEnvironment, _ config.Consumer, _ workflow.ValueType) (config.ParameterValueResolution, error) {
	if s.err != nil {
		return config.ParameterValueResolution{}, s.err
	}
	if resolution, ok := s.values[key]; ok {
		return resolution, nil
	}
	return config.ParameterValueResolution{}, nil
}

func thresholdTestSnapshot(t *testing.T) (config.Snapshot, config.Consumer) {
	t.Helper()
	decimal := workflow.ValueType{Kind: workflow.KindDecimal}
	consumer := config.Consumer{Kind: config.ConsumerCapability, ID: ThresholdConsumerID}
	snapshot, err := config.NewSnapshotWithDefinitions("active", "defs-v1", nil, []config.ParameterDefinition{
		{Key: rules.ThresholdFinanceKey, Type: decimal, Classification: "TENANT", Owner: "rules", AllowedConsumers: []config.Consumer{consumer}},
		{Key: rules.ThresholdExecutiveKey, Type: decimal, Classification: "TENANT", Owner: "rules", AllowedConsumers: []config.Consumer{consumer}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, consumer
}

func TestConfigThresholdReader(t *testing.T) {
	snapshot, consumer := thresholdTestSnapshot(t)
	decimal := workflow.ValueType{Kind: workflow.KindDecimal}
	pair := map[string]config.ParameterValueResolution{
		rules.ThresholdFinanceKey:   {Value: "5.0000", ValueType: decimal, Found: true, Revision: 3, DefinitionVersion: "defs-v1"},
		rules.ThresholdExecutiveKey: {Value: "15.0000", ValueType: decimal, Found: true, Revision: 5, DefinitionVersion: "defs-v1"},
	}

	resolved, err := (&ConfigThresholdReader{
		Snapshots: stubThresholdSnapshots{snapshot: snapshot}, Revisions: stubThresholdRevisions{values: pair},
		Environment: config.EnvironmentSandbox, Consumer: consumer,
	}).ResolveThresholds(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Found || resolved.FinanceText != "5.0000" || resolved.ExecutiveText != "15.0000" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if resolved.Revision != 3 || resolved.DefinitionVersion != "defs-v1" {
		t.Fatalf("revision cite = %+v", resolved)
	}

	t.Run("no published override resolves unpublished", func(t *testing.T) {
		resolved, err := (&ConfigThresholdReader{
			Snapshots: stubThresholdSnapshots{snapshot: snapshot}, Revisions: stubThresholdRevisions{},
			Environment: config.EnvironmentSandbox, Consumer: consumer,
		}).ResolveThresholds(context.Background(), "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Found {
			t.Fatalf("unpublished tenant resolved: %+v", resolved)
		}
	})

	t.Run("half-published override fails closed", func(t *testing.T) {
		half := map[string]config.ParameterValueResolution{
			rules.ThresholdFinanceKey: {Value: "5.0000", ValueType: decimal, Found: true, Revision: 3, DefinitionVersion: "defs-v1"},
		}
		if _, err := (&ConfigThresholdReader{
			Snapshots: stubThresholdSnapshots{snapshot: snapshot}, Revisions: stubThresholdRevisions{values: half},
			Environment: config.EnvironmentSandbox, Consumer: consumer,
		}).ResolveThresholds(context.Background(), "tenant-a"); err == nil {
			t.Fatal("half-published override resolved")
		}
	})

	t.Run("uncomposed reader and bad inputs fail closed", func(t *testing.T) {
		var nilReader *ConfigThresholdReader
		if _, err := nilReader.ResolveThresholds(context.Background(), "tenant-a"); err == nil {
			t.Fatal("nil reader resolved")
		}
		bare := &ConfigThresholdReader{}
		if _, err := bare.ResolveThresholds(context.Background(), "tenant-a"); err == nil {
			t.Fatal("reader without substrate resolved")
		}
		composed := &ConfigThresholdReader{
			Snapshots: stubThresholdSnapshots{snapshot: snapshot}, Revisions: stubThresholdRevisions{values: pair},
			Environment: config.EnvironmentSandbox, Consumer: consumer,
		}
		if _, err := composed.ResolveThresholds(context.Background(), "  "); err == nil {
			t.Fatal("empty tenant resolved")
		}
		composed.Environment = "MARS"
		if _, err := composed.ResolveThresholds(context.Background(), "tenant-a"); err == nil {
			t.Fatal("unknown environment resolved")
		}
	})
}

// TestConfigThresholdReaderEndToEnd proves REV-010-02 GREEN through the real
// substrate: two tenants publish different threshold revisions to durable
// storage, the production reader resolves each, and the rules engine
// evaluates the identical increase to different approval tiers citing each
// own resolved table version.
func TestConfigThresholdReaderEndToEnd(t *testing.T) {
	db := pgtest.New(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	for _, tenant := range []uuid.UUID{tenantA, tenantB} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Threshold Tenants','ACTIVE',$3)`, tenant, "threshold-"+tenant.String(), time.Now().UTC())
	}
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	store := configparamstore.New(db.Conn, func(key string) uuid.UUID {
		switch key {
		case tenantA.String():
			return tenantA
		case tenantB.String():
			return tenantB
		default:
			return uuid.Nil
		}
	})
	snapshot, consumer := thresholdTestSnapshot(t)
	decimal := workflow.ValueType{Kind: workflow.KindDecimal}
	publish := func(tenant uuid.UUID, finance, executive string) {
		t.Helper()
		path := config.ParameterScopePath{{Kind: config.ScopeTenant, ID: tenant.String()}}
		for _, kv := range [][2]string{{rules.ThresholdFinanceKey, finance}, {rules.ThresholdExecutiveKey, executive}} {
			change := config.ParameterValueChange{
				Key: kv[0], Scope: path[0], Environment: config.EnvironmentSandbox,
				Value: kv[1], ValueType: decimal, Author: "tenant-admin", Reason: "REV-010-02 fixture",
				RecordedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), Path: path,
			}
			if _, err := store.AppendParameterRevision(context.Background(), tenant.String(), snapshot, change); err != nil {
				t.Fatal(err)
			}
		}
	}
	publish(tenantA, "5.0000", "15.0000")
	publish(tenantB, "25.0000", "40.0000")

	reader := &ConfigThresholdReader{
		Snapshots: stubThresholdSnapshots{snapshot: snapshot}, Revisions: store,
		Environment: config.EnvironmentSandbox, Consumer: consumer,
	}
	resA, err := reader.ResolveThresholds(context.Background(), tenantA.String())
	if err != nil {
		t.Fatal(err)
	}
	resB, err := reader.ResolveThresholds(context.Background(), tenantB.String())
	if err != nil {
		t.Fatal(err)
	}
	if !resA.Found || !resB.Found {
		t.Fatalf("published tenants unresolved: %+v / %+v", resA, resB)
	}
	tableA, err := rules.PromotionApprovalThresholdTableForTenant(rules.TenantThresholdScope{Tenant: tenantA.String()}, resA)
	if err != nil {
		t.Fatal(err)
	}
	tableB, err := rules.PromotionApprovalThresholdTableForTenant(rules.TenantThresholdScope{Tenant: tenantB.String()}, resB)
	if err != nil {
		t.Fatal(err)
	}
	increase, err := values.NewDecimal("18.0000", rules.IncreasePercentScale, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	input := rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient,
	}
	decisionA, err := rules.EvaluatePromotionApproval(tableA, input)
	if err != nil {
		t.Fatal(err)
	}
	decisionB, err := rules.EvaluatePromotionApproval(tableB, input)
	if err != nil {
		t.Fatal(err)
	}
	if decisionA.Tier != rules.ApprovalTierExecutiveRequired {
		t.Fatalf("tenant-a 18 percent tier = %s, want EXECUTIVE", decisionA.Tier)
	}
	if decisionB.Tier != rules.ApprovalTierStandard {
		t.Fatalf("tenant-b 18 percent tier = %s, want STANDARD", decisionB.Tier)
	}
	if decisionA.TableVersion != "defs-v1" || decisionB.TableVersion != "defs-v1" {
		t.Fatalf("cited versions = %q/%q", decisionA.TableVersion, decisionB.TableVersion)
	}
	if decisionA.TableDigest == decisionB.TableDigest {
		t.Fatal("tenant tables digest identically")
	}
}
