package agentsystem

import (
	"context"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/schemaflux"
	"reflect"
	"testing"
)

func TestAgentUXR7_L1_UsedDocumentsIntegration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',now())`, tenant, tenantKey, tenantKey)
	mapper := func(key values.TenantId) uuid.UUID {
		if key == tenantKey {
			return tenant
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	store, err := agentrunstore.New(conn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, reply string
		want        []string
	}{{"used", `{"text":"The policy provides ten days.","citations":["document:leave/version:2#allowance"]}`, []string{"agent-document-usage:v1", "document:leave/version:2#allowance"}}, {"none", `{"text":"Hello.","citations":[]}`, []string{"agent-document-usage:v1"}}} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixtureWith(t, fixtureStores{Tasks: store})
			f.defaultOwner(t)
			f.provider.ReplyFunc(func(_ int, _ schemaflux.CompletionRequest) (string, error) { return test.reply, nil })
			task := f.start(t, "r7-documents-"+test.name, planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
			task, err = f.runner.Step(context.Background(), task.ID, ModeOnBehalfOf)
			if err != nil || task.State != agentrun.StateCompleted {
				t.Fatalf("sealed answer state=%s error=%v", task.State, err)
			}
			fresh, err := agentrunstore.New(db.NewConn(t), mapper)
			if err != nil {
				t.Fatal(err)
			}
			scoped, err := fresh.ForTenant(context.Background(), tenantKey)
			if err != nil {
				t.Fatal(err)
			}
			got, err := scoped.Get(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range got.Ledger.Entries {
				if entry.Kind == "STEP_RESULT" {
					found = true
					if !reflect.DeepEqual(entry.SourceIDs, test.want) {
						t.Fatalf("durable answer citations=%v want %v", entry.SourceIDs, test.want)
					}
				}
			}
			if !found {
				t.Fatal("sealed answer result absent after reconnect")
			}
		})
	}
}
