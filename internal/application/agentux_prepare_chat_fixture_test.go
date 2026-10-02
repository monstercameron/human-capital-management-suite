package application

import (
	"context"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUXPrepareChatFixture is the chat side of the local demo preparation: the
// real chat store and service for one tenant, and the administrator the
// preparation acts as, signed in.
func agentUXPrepareChatFixture(t *testing.T) (*chatstore.Store, *chatcore.Service, context.Context, chatcore.Principal) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	service := chatcore.NewService(store, func() time.Time { return now })
	service.SetAuthority(servedPersonaChatAuthority{})
	admin := chatcore.Principal{TenantID: "tenant-a", SubjectID: "ir-001-walt-brennan"}
	verified, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: admin.SubjectID, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentux-prepare", CredentialDigest: "agentux-prepare", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return raw, service, trust.WithPrincipal(context.Background(), verified), admin
}
