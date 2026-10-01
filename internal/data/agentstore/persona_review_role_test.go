package agentstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENTP_006_ReviewIssuerRoleIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("apply isolated agent migrations: %v", err)
	}

	var canLogin, bypassRLS, canCreateDB, canCreateRole bool
	err := db.SQL.QueryRowContext(ctx, `SELECT rolcanlogin, rolbypassrls, rolcreatedb, rolcreaterole
		FROM pg_roles WHERE rolname='hcmnext_persona_review_authority'`).Scan(&canLogin, &bypassRLS, &canCreateDB, &canCreateRole)
	if err != nil {
		t.Fatalf("read review authority role: %v", err)
	}
	if canLogin || bypassRLS || canCreateDB || canCreateRole {
		t.Fatalf("review authority role is overprivileged: login=%v bypass_rls=%v createdb=%v createrole=%v", canLogin, bypassRLS, canCreateDB, canCreateRole)
	}

	var appCanInsertDecision, appCanInsertGrant, issuerCanInsertDecision, issuerCanInsertGrant, issuerCanRewrite, issuerCanRevoke bool
	err = db.SQL.QueryRowContext(ctx, `SELECT
		has_table_privilege('hcmnext_agent_app','persona_review_decision','INSERT'),
		has_table_privilege('hcmnext_persona_review_authority','persona_review_decision','INSERT'),
		has_table_privilege('hcmnext_agent_app','persona_review_grant','INSERT'),
		has_table_privilege('hcmnext_persona_review_authority','persona_review_grant','INSERT'),
		has_column_privilege('hcmnext_persona_review_authority','persona_review_decision','review_digest','UPDATE'),
		has_column_privilege('hcmnext_persona_review_authority','persona_review_decision','revoked_at','UPDATE')`).Scan(
		&appCanInsertDecision, &issuerCanInsertDecision, &appCanInsertGrant, &issuerCanInsertGrant, &issuerCanRewrite, &issuerCanRevoke)
	if err != nil {
		t.Fatalf("read review evidence grants: %v", err)
	}
	if appCanInsertDecision || appCanInsertGrant || !issuerCanInsertDecision || !issuerCanInsertGrant || issuerCanRewrite || !issuerCanRevoke {
		t.Fatalf("unexpected review evidence privileges: app_decision_insert=%v app_grant_insert=%v issuer_decision_insert=%v issuer_grant_insert=%v issuer_rewrite=%v issuer_revoke=%v", appCanInsertDecision, appCanInsertGrant, issuerCanInsertDecision, issuerCanInsertGrant, issuerCanRewrite, issuerCanRevoke)
	}
}
