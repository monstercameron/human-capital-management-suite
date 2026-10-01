package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTP_023_LocalPolicyIndependentReviewerSecurity(t *testing.T) {
	for _, tc := range []struct{ tenant, reviewer string }{{"ironridge-demo", "ir-008-curtis-bell"}, {"harborcare-demo", "hc-004-darius-bennett"}} {
		pack, _ := demoworkforce.PackFor(tc.tenant)
		reviewer, err := localDevPolicyHelperReviewer(pack, pgstore.TenantID(tc.tenant))
		owner, steward, ownerErr := localDevPolicyHelperOwners(pack, pgstore.TenantID(tc.tenant))
		if err != nil || ownerErr != nil || reviewer != tc.reviewer || reviewer == owner || reviewer == steward {
			t.Fatalf("independent reviewer=%s owner=%s steward=%s err=%v/%v", reviewer, owner, steward, err, ownerErr)
		}
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	db := &localDraftBootstrapDB{}
	directory := &localDraftBootstrapDirectory{}
	authorizer := &personaCreateAuthorizerFake{allowedTenant: values.TenantId("ironridge-demo"), err: ErrPersonaDraftDenied}
	b := &LocalDevPolicyHelperReviewerBootstrap{ReviewDB: db, Roles: &positionReadRoleStore{}, Directory: directory, TenantUUID: tenantKeyMapper[values.TenantId](pgstore.TenantID), Authorizer: authorizer, Now: func() time.Time { return now }}
	for _, subject := range []string{"ir-008-curtis-bell", "ir-003-loretta-haynes"} {
		ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "ironridge-demo", subject, now))
		if _, err := b.Provision(ctx, ServeProfileLocalDev); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
			t.Fatalf("nonowner granted reviewer permission err=%v", err)
		}
	}
	ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "ironridge-demo", "ir-001-walt-brennan", now))
	if _, err := b.Provision(ctx, ServeProfileLocalDev); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
		t.Fatalf("owner without current admin permission accepted err=%v", err)
	}
	if authorizer.calls != 1 || db.begins != 0 || directory.reads != 0 {
		t.Fatalf("early unauthorized side effect auth=%d db=%d directory=%d", authorizer.calls, db.begins, directory.reads)
	}
	authorizer.err = nil
	if _, err := b.Provision(ctx, ServeProfileLocalDev); !errors.Is(err, ErrLocalDevPersonaChatBootstrap) {
		t.Fatalf("unresolved reviewer current directory accepted err=%v", err)
	}
	if db.begins != 0 || directory.reads != 1 {
		t.Fatalf("missing current reviewer reached writer db=%d directory=%d", db.begins, directory.reads)
	}
}
