package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestNewPersonaPublicationService_FailsClosedWithoutDurableAuthorities(t *testing.T) {
	if _, err := NewPersonaPublicationService(nil); !errors.Is(err, ErrPersonaPublicationUnavailable) {
		t.Fatalf("error=%v want unavailable", err)
	}
}

func TestPersonaPublicationService_RejectsMalformedRequestBeforeStoreAccess(t *testing.T) {
	service, err := NewPersonaPublicationService(&agentpersonastore.Store{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		tenant values.TenantId
		event  agentpersonastore.LifecycleEvent
	}{
		{name: "nil context", tenant: "tenant-a", event: agentpersonastore.LifecycleEvent{TenantID: "tenant-a", PersonaID: "persona-a", PersonaVersion: 1}},
		{name: "foreign tenant", ctx: context.Background(), tenant: "tenant-a", event: agentpersonastore.LifecycleEvent{TenantID: "tenant-b", PersonaID: "persona-a", PersonaVersion: 1}},
		{name: "missing version", ctx: context.Background(), tenant: "tenant-a", event: agentpersonastore.LifecycleEvent{TenantID: "tenant-a", PersonaID: "persona-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := service.Publish(tc.ctx, tc.tenant, tc.event, agentpersonastore.PublicationEvidence{}); !errors.Is(err, ErrPersonaPublicationInvalid) {
				t.Fatalf("error=%v want invalid", err)
			}
		})
	}
}
