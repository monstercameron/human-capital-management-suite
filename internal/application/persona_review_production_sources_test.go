package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaReviewSourceDB struct{ tx *personaReviewSourceTx }

func (d personaReviewSourceDB) Begin(context.Context) (dbport.Tx, error) { return d.tx, nil }

type personaReviewSourceTx struct {
	settingTenant string
	query         string
	args          []any
	current       bool
	queryErr      error
	rolledBack    bool
}

func (tx *personaReviewSourceTx) Exec(_ context.Context, query string, args ...any) (int64, error) {
	if !strings.Contains(query, "set_config") || len(args) != 2 || args[0] != "app.tenant_id" {
		return 0, errors.New("unexpected tenant setup")
	}
	tx.settingTenant = args[1].(string)
	return 1, nil
}

func (tx *personaReviewSourceTx) QueryRow(_ context.Context, query string, args ...any) dbport.Row {
	tx.query, tx.args = query, args
	return personaReviewSourceRow{tx: tx}
}

func (*personaReviewSourceTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (*personaReviewSourceTx) Commit(context.Context) error { return nil }

func (tx *personaReviewSourceTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type personaReviewSourceRow struct{ tx *personaReviewSourceTx }

func (row personaReviewSourceRow) Scan(dest ...any) error {
	if row.tx.queryErr != nil {
		return row.tx.queryErr
	}
	if len(dest) != 1 {
		return errors.New("unexpected scan shape")
	}
	current, ok := dest[0].(*bool)
	if !ok {
		return errors.New("unexpected scan target")
	}
	*current = row.tx.current
	return nil
}

func TestTodo_AGENTP_006_CurrentReviewGrantAuthorityTenantScopesGrantRead(t *testing.T) {
	tenantID := uuid.MustParse("8c2e481c-5b07-4c13-a1db-409eb6557001")
	tx := &personaReviewSourceTx{current: true}
	authority, err := NewCurrentPersonaReviewGrantAuthority(personaReviewSourceDB{tx: tx}, func(tenant values.TenantId) uuid.UUID {
		if tenant != "tenant-a" {
			t.Fatalf("tenant mapper received %q", tenant)
		}
		return tenantID
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.AuthorizePersonaReview(context.Background(), "tenant-a", "user:reviewer"); err != nil {
		t.Fatalf("current grant authorization: %v", err)
	}
	if tx.settingTenant != tenantID.String() || !strings.Contains(tx.query, "persona_review_grant") || !strings.Contains(tx.query, "revoked_at IS NULL") || !strings.Contains(tx.query, "expires_at > clock_timestamp()") {
		t.Fatalf("grant read did not use the tenant boundary and current-grant predicates: tenant=%q query=%q", tx.settingTenant, tx.query)
	}
	if len(tx.args) != 2 || tx.args[0] != tenantID || tx.args[1] != "user:reviewer" || !tx.rolledBack {
		t.Fatalf("grant query args=%v transaction rolled back=%t", tx.args, tx.rolledBack)
	}
}

func TestTodo_AGENTP_006_CurrentReviewGrantAuthorityDeniesMissingOrUnavailableGrant(t *testing.T) {
	tenantID := uuid.MustParse("8c2e481c-5b07-4c13-a1db-409eb6557001")
	for _, tc := range []struct {
		name     string
		current  bool
		queryErr error
		wantErr  bool
	}{
		{name: "current grant", current: true},
		{name: "no current grant", wantErr: true},
		{name: "database failure", queryErr: errors.New("database unavailable"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &personaReviewSourceTx{current: tc.current, queryErr: tc.queryErr}
			authority, err := NewCurrentPersonaReviewGrantAuthority(personaReviewSourceDB{tx: tx}, func(values.TenantId) uuid.UUID { return tenantID })
			if err != nil {
				t.Fatal(err)
			}
			err = authority.AuthorizePersonaReview(context.Background(), "tenant-a", "user:reviewer")
			if (err != nil) != tc.wantErr {
				t.Fatalf("authorization error = %v, wantErr=%t", err, tc.wantErr)
			}
			if !tx.rolledBack {
				t.Fatal("grant lookup transaction was not closed")
			}
		})
	}
}

func TestTodo_AGENTP_006_ReviewSourcesFailClosedWithoutDistinctStores(t *testing.T) {
	mapper := func(values.TenantId) uuid.UUID { return uuid.MustParse("8c2e481c-5b07-4c13-a1db-409eb6557001") }
	if _, err := NewPersonaAdminReviewSources(nil, nil, mapper); !errors.Is(err, ErrPersonaReviewUnavailable) {
		t.Fatalf("missing stores error = %v", err)
	}
	if _, err := NewPersonaAdminReviewSources(nil, nil, nil); !errors.Is(err, ErrPersonaReviewUnavailable) {
		t.Fatalf("missing stores and tenant mapper error = %v", err)
	}
}
