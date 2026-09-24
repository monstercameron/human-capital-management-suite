package configregistry_test

import (
	"testing"
	"time"

	dataconfigregistry "github.com/monstercameron/human-capital-management-suite/internal/data/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

func TestTodo_REV_031_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "rev031-atomic-activation")
	store := dataconfigregistry.New(appConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String(), CellID: "cell-local"}
	prepared, err := platformconfig.Prepare(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindRule, ID: "atomic-rule", Revision: 1,
		Body: []byte(`{"enabled":true}`), SchemaRef: "rule/v1", Scope: scope,
		PublisherPrincipal: "operator", PublishedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	obj := prepared.Object()
	if _, found, err := store.GetObject(obj.Ref()); err != nil || found {
		t.Fatalf("prepare wrote object before commit: found=%t err=%v", found, err)
	}
	evidence := platformconfig.ActivationEvidence{ActivatedBy: "operator", Authority: "config-control", ActivatedAt: time.Date(2026, 9, 23, 12, 1, 0, 0, time.UTC)}
	receipt, err := platformconfig.CommitActivation(store, prepared, evidence)
	if err != nil {
		t.Fatal(err)
	}
	active, err := platformconfig.Resolve(store, scope, obj.Kind, obj.ID)
	if err != nil || active.Digest() != obj.Digest() || receipt.ObjectDigest != obj.Digest() {
		t.Fatalf("committed object=%+v receipt=%+v err=%v", active, receipt, err)
	}
	history, err := store.ListActivations(scope, obj.Kind, obj.ID)
	if err != nil || len(history) != 1 || history[0].ObjectDigest != obj.Digest() {
		t.Fatalf("durable activation history=%+v err=%v", history, err)
	}
}
