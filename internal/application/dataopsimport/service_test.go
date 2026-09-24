package dataopsimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dataopsstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_REV_030_01_Application(t *testing.T) {
	ctx := context.Background()
	if _, err := (*Service)(nil).Stage(ctx, values.TenantId("acme"), StageRequest{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil service = %v, want ErrStoreUnavailable", err)
	}
	service := New(nil, nil, nil)
	if _, err := service.Stage(ctx, values.TenantId("acme"), StageRequest{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil store = %v, want ErrStoreUnavailable", err)
	}
	if _, err := service.StageFromContext(ctx, StageRequest{}); !errors.Is(err, trust.ErrNoPrincipal) {
		t.Fatalf("missing principal = %v, want trust.ErrNoPrincipal", err)
	}

	// A real store is not needed to prove the request and mapper checks happen
	// before parsing or writing any payload.
	service = New(&dataopsstore.Store{}, nil, func() time.Time { return time.Unix(1, 0).UTC() })
	if _, err := service.Stage(ctx, values.TenantId("acme"), StageRequest{}); !errors.Is(err, ErrTenantMapper) {
		t.Fatalf("nil mapper = %v, want ErrTenantMapper", err)
	}
	service = New(&dataopsstore.Store{}, func(values.TenantId) uuid.UUID { return uuid.New() }, func() time.Time { return time.Unix(1, 0).UTC() })
	if _, err := service.Stage(ctx, values.TenantId("bad tenant"), StageRequest{SourceURI: "upload://x", IdempotencyKey: "k", Payload: []byte("a,b\n1,2\n")}); !errors.Is(err, ErrRequestInvalid) {
		t.Fatalf("invalid tenant = %v, want ErrRequestInvalid", err)
	}
}

// TestTodo_REV_030_01_Application_Integration proves the application service
// obtains the tenant from the authenticated trust context before the durable
// store is called. StageRequest has no tenant field that a caller can forge.
func TestTodo_REV_030_01_Application_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := values.TenantId("acme")
	tenantUUID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,'dataops-app','cell-local','DataOps app','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantUUID)
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	service := New(dataopsstore.New(conn), func(values.TenantId) uuid.UUID { return tenantUUID }, func() time.Time { return now })
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: "operator-1", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-1", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "sha256:" + strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	payload := []byte("worker_id,name\nworker-1,Ada\n")
	h := sha256.Sum256(payload)
	receipt, err := service.StageCSV(trust.WithPrincipal(context.Background(), p), "upload://trusted", "app-stage-1", payload)
	if err != nil {
		t.Fatalf("StageCSV: %v", err)
	}
	rec := receipt.GetStagedImport()
	if rec == nil || rec.GetTenantId() != tenantUUID.String() || rec.GetRowCount() != 1 || rec.GetSourceHash() != "sha256:"+hex.EncodeToString(h[:]) || rec.GetStatus() != dataopsv1.StagedImportStatus_STAGED_IMPORT_STATUS_STAGED {
		t.Fatalf("receipt = %+v, want authenticated tenant and staged status", rec)
	}
}
