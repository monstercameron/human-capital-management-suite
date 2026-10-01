package agentmodelpolicystore

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// AuthorityPoolConfig names an independently credentialed policy publisher on
// the isolated agent database. Its login must differ from every serving login.
type AuthorityPoolConfig struct{ DSN, AgentDSN, CoreDSN, ChatDSN, DocumentDSN string }
type AuthorityPool struct{ pool *pgxadapter.Pool }

func NewAuthorityPool(ctx context.Context, cfg AuthorityPoolConfig) (*AuthorityPool, error) {
	if ctx == nil {
		return nil, ErrAuthority
	}
	issuer, err := policyDatabaseURL(cfg.DSN)
	if err != nil {
		return nil, ErrAuthority
	}
	agent, err := policyDatabaseURL(cfg.AgentDSN)
	if err != nil {
		return nil, ErrAuthority
	}
	core, err := policyDatabaseURL(cfg.CoreDSN)
	if err != nil {
		return nil, ErrAuthority
	}
	if policyDatabaseIdentity(issuer) != policyDatabaseIdentity(agent) || issuer.User.Username() == agent.User.Username() || issuer.User.Username() == core.User.Username() || policyDatabaseIdentity(issuer) == policyDatabaseIdentity(core) {
		return nil, ErrAuthority
	}
	for _, other := range []string{cfg.ChatDSN, cfg.DocumentDSN} {
		if strings.TrimSpace(other) == "" {
			continue
		}
		u, err := policyDatabaseURL(other)
		if err != nil || policyDatabaseIdentity(issuer) == policyDatabaseIdentity(u) || issuer.User.Username() == u.User.Username() {
			return nil, ErrAuthority
		}
	}
	query := issuer.Query()
	query.Set("pool_max_conns", "2")
	query.Set("pool_min_conns", "0")
	issuer.RawQuery = query.Encode()
	pool, err := pgxadapter.NewPool(ctx, issuer.String(), map[string]string{"role": AuthorityRole})
	if err != nil {
		return nil, fmt.Errorf("agentmodelpolicystore: open authority connection: %w", err)
	}
	var superuser, bypass, createRole, createDB bool
	err = pool.QueryRow(ctx, `SELECT rolsuper,rolbypassrls,rolcreaterole,rolcreatedb FROM pg_roles WHERE rolname=session_user`).Scan(&superuser, &bypass, &createRole, &createDB)
	if err != nil || superuser || bypass || createRole || createDB {
		pool.Close()
		return nil, ErrAuthority
	}
	return &AuthorityPool{pool: pool}, nil
}
func (p *AuthorityPool) Begin(ctx context.Context) (dbport.Tx, error) {
	if p == nil || p.pool == nil {
		return nil, ErrAuthority
	}
	return p.pool.Begin(ctx)
}
func (p *AuthorityPool) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

func policyDatabaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.User == nil || strings.TrimSpace(u.User.Username()) == "" || strings.Trim(u.Path, "/") == "" {
		return nil, ErrAuthority
	}
	return u, nil
}
func policyDatabaseIdentity(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "::1" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port) + "/" + strings.Trim(u.Path, "/")
}
