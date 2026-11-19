package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

const (
	// localDevChatRole and localDevAgentRole are the login roles the built-in
	// local-development profile connects to its chat and agent databases as.
	// Both stores refuse the core credential, and the profile's own defaults
	// name one PostgreSQL login for every database, so without a role of
	// their own the documented `serve -profile=local-dev` cannot start.
	localDevChatRole  = "hcmnext_chat_dev"
	localDevAgentRole = "hcmnext_agents_dev"
)

// errLocalDevRoleProvisioning marks a core credential that cannot create the
// development roles. The message names the way out: explicit store URLs.
var errLocalDevRoleProvisioning = errors.New("application: the local-dev profile could not give its chat and agent databases their own login roles; " +
	"set -" + FieldChatDatabaseURL + " and -" + FieldAgentDatabaseURL + " to databases with their own credentials")

// isolateLocalDevDatabases gives the built-in local-development chat and agent
// databases a login role each and returns the configuration naming them. It
// acts only on the exact built-in loopback URLs of the local-dev profile; an
// explicit URL, and every other profile, is returned unchanged and keeps the
// stores' own credential checks.
//
// The roles are superusers on purpose: the stores switch to roles their
// migrations create, and the profile's core login is already the loopback
// superuser, so a narrower role would add ceremony without adding a boundary.
// Their passwords derive from the development key and are reset on every
// start, so nothing is written down.
func isolateLocalDevDatabases(ctx context.Context, cfg ServeConfig, pool *pgxadapter.Pool) (ServeConfig, error) {
	if cfg.Profile != ServeProfileLocalDev || pool == nil {
		return cfg, nil
	}
	if cfg.ChatEnabled && cfg.ChatDatabaseURL == LocalDevChatDatabaseURL {
		dsn, err := provisionLocalDevStore(ctx, pool, cfg.ChatDatabaseURL, cfg.DevHMACKey, localDevChatRole)
		if err != nil {
			return cfg, err
		}
		cfg.ChatDatabaseURL = dsn
	}
	if cfg.AgentDatabaseURL == LocalDevAgentDatabaseURL {
		dsn, err := provisionLocalDevStore(ctx, pool, cfg.AgentDatabaseURL, cfg.DevHMACKey, localDevAgentRole)
		if err != nil {
			return cfg, err
		}
		// Nothing but this process migrates the agent database, and the
		// profile turns -migrate off, so a first start would otherwise meet an
		// empty schema.
		if err := agentstore.MigrateDSN(ctx, dsn); err != nil {
			return cfg, fmt.Errorf("application: migrate the local development agent database: %w", err)
		}
		cfg.AgentDatabaseURL = dsn
	}
	return cfg, nil
}

// provisionLocalDevStore creates the store's database when it is missing,
// creates or re-keys its login role, and returns storeURL rewritten to that
// role.
func provisionLocalDevStore(ctx context.Context, pool *pgxadapter.Pool, storeURL, devKey, role string) (string, error) {
	dsn, password, database, err := localDevStoreDSN(storeURL, devKey, role)
	if err != nil {
		return "", err
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, database).Scan(&exists); err != nil {
		return "", fmt.Errorf("%w: %v", errLocalDevRoleProvisioning, err)
	}
	if !exists {
		// CREATE DATABASE cannot run in a transaction or take a parameter;
		// the name comes from the profile's own constant.
		if _, err := pool.Exec(ctx, `CREATE DATABASE `+quoteLocalDevIdentifier(database)); err != nil {
			return "", fmt.Errorf("%w: %v", errLocalDevRoleProvisioning, err)
		}
	}
	statement := fmt.Sprintf(`DO $do$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[1]s') THEN
			CREATE ROLE %[1]s LOGIN SUPERUSER PASSWORD '%[2]s';
		ELSE
			ALTER ROLE %[1]s LOGIN SUPERUSER PASSWORD '%[2]s';
		END IF;
	END $do$`, role, password)
	if _, err := pool.Exec(ctx, statement); err != nil {
		return "", fmt.Errorf("%w: %v", errLocalDevRoleProvisioning, err)
	}
	return dsn, nil
}

// localDevStoreDSN rewrites storeURL to the store's development role. The
// password is an HMAC of the role under the development key: stable across
// restarts, different per role, and never the core login's.
func localDevStoreDSN(storeURL, devKey, role string) (dsn, password, database string, err error) {
	if strings.TrimSpace(devKey) == "" {
		return "", "", "", errors.New("application: the local-dev profile needs the development key to derive its store credentials")
	}
	u, parseErr := url.Parse(strings.TrimSpace(storeURL))
	if parseErr != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", "", "", errors.New("application: the local-dev store URL is not a postgres URL")
	}
	database = strings.TrimPrefix(u.Path, "/")
	if database == "" {
		return "", "", "", errors.New("application: the local-dev store URL names no database")
	}
	mac := hmac.New(sha256.New, []byte(devKey))
	_, _ = mac.Write([]byte("hcmnext-local-dev-store-role:" + role))
	password = hex.EncodeToString(mac.Sum(nil))[:32]
	u.User = url.UserPassword(role, password)
	return u.String(), password, database, nil
}

func quoteLocalDevIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
