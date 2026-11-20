package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// A person admitted to another company, with the same subject id as a member
// here, learns nothing and writes nothing: the conversation is looked up in
// the caller's own tenant.
func TestIssue3815_OtherTenantLearnsAndWritesNothing_Security(t *testing.T) {
	acks, store := issue3815Fixture(t)
	ctx := context.Background()
	if _, err := acks.Require(ctx, issue3795Person(t, "owner", trust.SubjectKindHuman), "room", "policy"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, subject := range []string{"person", "owner", "manager"} {
		foreign, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-b"), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := acks.Views(ctx, foreign, "room", []string{"policy"}); !errors.Is(err, ErrChatAckNotFound) {
			t.Errorf("%s of another company reads = %v; want not found", subject, err)
		}
		if _, err := acks.Acknowledge(ctx, foreign, "room", "policy"); !errors.Is(err, ErrChatAckNotFound) {
			t.Errorf("%s of another company acknowledges = %v; want not found", subject, err)
		}
		if _, err := acks.Require(ctx, foreign, "room", "policy"); !errors.Is(err, ErrChatAckNotFound) {
			t.Errorf("%s of another company requires = %v; want not found", subject, err)
		}
	}
	if len(store.acks["policy"]) != 0 || len(store.reqs) != 1 {
		t.Fatalf("a refused call wrote: reqs %v acks %v", store.reqs, store.acks)
	}
}
