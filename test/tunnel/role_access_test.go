package tunnel_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func bootstrapTestRoleAccess(t *testing.T, db dbport.Beginner, tenant string) roleaccess.Store {
	t.Helper()
	definitions := productui.FlattenFeatureDefinitions()
	features := make([]roleaccess.FeatureDefinition, 0, len(definitions))
	for _, item := range definitions {
		features = append(features, roleaccess.FeatureDefinition{
			PageID: string(item.Page), FeatureID: string(item.Feature.ID),
			View: item.Feature.View, Create: item.Feature.Create,
			Update: item.Feature.Update, Delete: item.Feature.Delete,
		})
	}
	store := roleaccessstore.New(db, func(id values.TenantId) uuid.UUID { return pgstore.TenantID(string(id)) }, features...)
	if err := store.Bootstrap(context.Background(), values.TenantId(tenant), "system:test-bootstrap"); err != nil {
		t.Fatalf("bootstrap role access: %v", err)
	}
	return store
}
