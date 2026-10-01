package agentmodelpolicystore

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENT_021_PolicyPublisherCredentialIsolation(t *testing.T) {
	valid := AuthorityPoolConfig{DSN: "postgres://issuer:password@localhost:5432/agents", AgentDSN: "postgres://agent:password@127.0.0.1:5432/agents", CoreDSN: "postgres://core:password@localhost:5432/core"}
	for _, tc := range []struct {
		name   string
		change func(*AuthorityPoolConfig)
	}{{"missing DSN", func(c *AuthorityPoolConfig) { c.DSN = "" }}, {"non postgres", func(c *AuthorityPoolConfig) { c.DSN = "http://issuer:password@localhost/agents" }}, {"serving login", func(c *AuthorityPoolConfig) { c.DSN = c.AgentDSN }}, {"core database", func(c *AuthorityPoolConfig) { c.CoreDSN = "postgres://core:password@localhost:5432/agents" }}, {"core login", func(c *AuthorityPoolConfig) { c.CoreDSN = "postgres://issuer:different@localhost:5432/core" }}, {"other agent database", func(c *AuthorityPoolConfig) { c.AgentDSN = "postgres://agent:password@localhost:5432/other" }}, {"chat login", func(c *AuthorityPoolConfig) { c.ChatDSN = "postgres://issuer:different@localhost:5432/chat" }}, {"document database", func(c *AuthorityPoolConfig) { c.DocumentDSN = "postgres://document:password@localhost:5432/agents" }}} {
		cfg := valid
		tc.change(&cfg)
		if _, err := NewAuthorityPool(context.Background(), cfg); !errors.Is(err, ErrAuthority) {
			t.Fatalf("%s=%v", tc.name, err)
		}
	}
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	login := "policy_test_" + uuid.NewString()[:8]
	password := uuid.NewString()
	if _, err := db.SQL.Exec(`CREATE ROLE ` + login + ` LOGIN PASSWORD '` + password + `' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`GRANT ` + AuthorityRole + ` TO ` + login); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.Exec(`DROP ROLE ` + login); err != nil {
			t.Error(err)
		}
	})
	issuer, _ := url.Parse(db.URL)
	issuer.User = url.UserPassword(login, password)
	query := issuer.Query()
	query.Set("search_path", db.Schema)
	issuer.RawQuery = query.Encode()
	agent := *issuer
	agent.User = url.UserPassword("test_agent_serving", "other")
	core := *issuer
	core.Path = "/core"
	core.User = url.UserPassword("test_core_serving", "other")
	cfg := AuthorityPoolConfig{DSN: issuer.String(), AgentDSN: agent.String(), CoreDSN: core.String()}
	pool, err := NewAuthorityPool(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var current, session string
	if err := tx.QueryRow(ctx, `SELECT current_user,session_user`).Scan(&current, &session); err != nil {
		t.Fatal(err)
	}
	if current != AuthorityRole || session != login {
		t.Fatalf("publisher credential role=%s login=%s", current, session)
	}
	_ = tx.Rollback(ctx)
	pool.Close()
	admin := *issuer
	parsedAdmin, _ := url.Parse(db.URL)
	admin.User = parsedAdmin.User
	cfg.DSN = admin.String()
	if _, err := NewAuthorityPool(ctx, cfg); !errors.Is(err, ErrAuthority) {
		t.Fatalf("superuser login=%v", err)
	}
}
