package agentstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENTP_006_ReviewAuthorityPoolIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("apply isolated agent migrations: %v", err)
	}
	agentLogin, reviewLogin, coreLogin := roleName("agent_login"), roleName("review_login"), roleName("core_login")
	for _, name := range []string{agentLogin, reviewLogin, coreLogin} {
		if _, err := db.SQL.ExecContext(ctx, "CREATE ROLE "+name+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD 'test-password'"); err != nil {
			t.Fatalf("create isolated test login: %v", err)
		}
		name := name
		t.Cleanup(func() {
			if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+name); err != nil {
				t.Errorf("drop isolated test login: %v", err)
			}
		})
	}
	if _, err := db.SQL.ExecContext(ctx, "GRANT "+AppRole+" TO "+agentLogin); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, "GRANT "+PersonaReviewAuthorityRole+" TO "+reviewLogin); err != nil {
		t.Fatal(err)
	}
	password := "test-password"
	agentDSN := testDSN(t, db.URL, db.Schema, agentLogin, password, "postgres")
	reviewDSN := testDSN(t, db.URL, db.Schema, reviewLogin, password, "postgres")
	coreDSN := testDSN(t, db.URL, "", coreLogin, password, "core")
	store, err := NewPersonaReviewAuthorityStore(ctx, PersonaReviewAuthorityConfig{
		DSN: reviewDSN, AgentDSN: agentDSN, CoreDSN: coreDSN, MaxConns: 1,
	})
	if err != nil {
		t.Fatalf("open restricted review authority pool: %v", err)
	}
	t.Cleanup(store.Close)
	tx, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var currentUser, sessionUser string
	if err := tx.QueryRow(ctx, `SELECT current_user, session_user`).Scan(&currentUser, &sessionUser); err != nil {
		t.Fatal(err)
	}
	if currentUser != PersonaReviewAuthorityRole || sessionUser != reviewLogin {
		t.Fatalf("pool identity current_user=%q session_user=%q", currentUser, sessionUser)
	}
}

func TestTodo_AGENTP_006_ReviewAuthorityPoolSecurity(t *testing.T) {
	core := "postgres://core:pw@db.example:5432/core"
	agent := "postgres://agent:pw@db.example:5432/agent"
	t.Run("requires its own credential", func(t *testing.T) {
		_, err := NewPersonaReviewAuthorityStore(context.Background(), PersonaReviewAuthorityConfig{
			DSN: agent, AgentDSN: agent, CoreDSN: core,
		})
		if err != ErrSharedCredential {
			t.Fatalf("shared credential error = %v", err)
		}
	})
	t.Run("requires the isolated agent database", func(t *testing.T) {
		_, err := NewPersonaReviewAuthorityStore(context.Background(), PersonaReviewAuthorityConfig{
			DSN: "postgres://review:pw@db.example:5432/other", AgentDSN: agent, CoreDSN: core,
		})
		if err == nil {
			t.Fatal("review authority accepted a different database")
		}
	})
	if !sameDatabase(agent, "postgres://other:pw@db.example:5432/agent") {
		t.Fatal("database identity check did not recognize shared agent database")
	}
}
