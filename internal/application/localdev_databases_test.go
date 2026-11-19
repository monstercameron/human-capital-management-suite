package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// The built-in local-dev defaults name one login for the core, chat and agent
// databases, and both stores refuse the core credential. This pins the
// refusal the profile has to work around.
func TestLocalDevDefaultsShareTheCoreCredential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := chatstore.New(ctx, chatstore.Config{DSN: LocalDevChatDatabaseURL, CoreDSN: LocalDevDatabaseURL}); !errors.Is(err, chatstore.ErrCoreCredential) {
		t.Fatalf("chat store with the built-in defaults: err = %v, want ErrCoreCredential", err)
	}
	if _, err := agentstore.New(ctx, agentstore.Config{DSN: LocalDevAgentDatabaseURL, CoreDSN: LocalDevDatabaseURL}); !errors.Is(err, agentstore.ErrSharedCredential) {
		t.Fatalf("agent store with the built-in defaults: err = %v, want ErrSharedCredential", err)
	}
}

func TestLocalDevStoreDSN(t *testing.T) {
	const key = "a-development-key-of-enough-length"
	dsn, password, database, err := localDevStoreDSN(LocalDevChatDatabaseURL, key, localDevChatRole)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := u.User.Password()
	if u.User.Username() != localDevChatRole || got != password || len(password) != 32 {
		t.Fatalf("dsn user = %q password = %q (derived %q)", u.User.Username(), got, password)
	}
	if database != "hcm_next_chat" || u.Host != "127.0.0.1:5432" || u.Query().Get("sslmode") != "disable" {
		t.Fatalf("database %q host %q query %q: the rewrite changed more than the login", database, u.Host, u.RawQuery)
	}
	_, again, _, _ := localDevStoreDSN(LocalDevChatDatabaseURL, key, localDevChatRole)
	_, agent, _, _ := localDevStoreDSN(LocalDevAgentDatabaseURL, key, localDevAgentRole)
	_, otherKey, _, _ := localDevStoreDSN(LocalDevChatDatabaseURL, key+"x", localDevChatRole)
	if again != password || agent == password || otherKey == password {
		t.Fatalf("password must be stable per role and key: again=%v agent-differs=%v key-differs=%v", again == password, agent != password, otherKey != password)
	}
	for name, tc := range map[string]struct{ url, key string }{
		"no key":      {LocalDevChatDatabaseURL, " "},
		"not a URL":   {"host=db dbname=chat", key},
		"no database": {"postgres://postgres:postgres@127.0.0.1:5432/", key},
	} {
		if _, _, _, err := localDevStoreDSN(tc.url, tc.key, localDevChatRole); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Every profile but local-dev, and every explicit store URL, is left exactly
// as configured: the stores' own credential checks stay in force there.
func TestIsolateLocalDevDatabasesLeavesOtherConfigurationsAlone(t *testing.T) {
	explicit := "postgres://chat:secret@127.0.0.1:5432/my_chat?sslmode=disable"
	for name, cfg := range map[string]ServeConfig{
		"standard profile": {Profile: ServeProfileStandard, ChatEnabled: true, ChatDatabaseURL: LocalDevChatDatabaseURL, AgentDatabaseURL: LocalDevAgentDatabaseURL, DevHMACKey: LocalDevHMACKey},
		"explicit URLs":    {Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: explicit, AgentDatabaseURL: explicit + "&x=1", DevHMACKey: LocalDevHMACKey},
		"chat disabled":    {Profile: ServeProfileLocalDev, ChatDatabaseURL: LocalDevChatDatabaseURL, DevHMACKey: LocalDevHMACKey},
	} {
		// A closed pool fails every statement, so any provisioning attempt
		// surfaces as an error here.
		pool := closedPool(t)
		got, err := isolateLocalDevDatabases(context.Background(), cfg, pool)
		if err != nil || !sameStoreURLs(got, cfg) {
			t.Errorf("%s: err = %v, changed = %v", name, err, !sameStoreURLs(got, cfg))
		}
	}
	cfg := ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: LocalDevChatDatabaseURL, DevHMACKey: LocalDevHMACKey}
	if got, err := isolateLocalDevDatabases(context.Background(), cfg, nil); err != nil || !sameStoreURLs(got, cfg) {
		t.Errorf("no pool: err = %v, changed = %v", err, !sameStoreURLs(got, cfg))
	}
	if _, err := isolateLocalDevDatabases(context.Background(), cfg, closedPool(t)); !errors.Is(err, errLocalDevRoleProvisioning) {
		t.Errorf("a core login that cannot provision: err = %v, want errLocalDevRoleProvisioning", err)
	}
}

// Against a real server: the database is created when missing, the role is
// created and can log in, a second start re-keys rather than fails, and the
// chat store no longer refuses the returned credential.
func TestProvisionLocalDevStore_Integration(t *testing.T) {
	pgtest.New(t) // starts the shared server
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := pgxadapter.NewPool(ctx, pgtest.ServerURL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	role, database := "hcmnext_localdev_probe_"+suffix, "hcmnext_localdev_probe_db_"+suffix
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _ = admin.Exec(cleanup, `DROP DATABASE IF EXISTS `+quoteLocalDevIdentifier(database)+` WITH (FORCE)`)
		_, _ = admin.Exec(cleanup, `DROP ROLE IF EXISTS `+quoteLocalDevIdentifier(role))
	})
	base, err := url.Parse(pgtest.ServerURL())
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	storeURL := base.String()

	dsn, err := provisionLocalDevStore(ctx, admin, storeURL, LocalDevHMACKey, role)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	again, err := provisionLocalDevStore(ctx, admin, storeURL, LocalDevHMACKey, role)
	if err != nil || again != dsn {
		t.Fatalf("second start: dsn changed = %v, err = %v", again != dsn, err)
	}
	store, err := pgxadapter.NewPool(ctx, dsn, nil)
	if err != nil {
		t.Fatalf("log in with the provisioned role: %v", err)
	}
	defer store.Close()
	var user, current string
	var super bool
	if err := store.QueryRow(ctx, `SELECT current_user, current_database(), (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)`).Scan(&user, &current, &super); err != nil {
		t.Fatal(err)
	}
	if user != role || current != database || !super {
		t.Fatalf("connected as %q to %q (superuser %v), want %q on %q", user, current, super, role, database)
	}
	// The credential check passes; the store may still refuse an unmigrated
	// schema, which is not what this test is about.
	chat, err := chatstore.New(ctx, chatstore.Config{DSN: dsn, CoreDSN: pgtest.ServerURL()})
	if errors.Is(err, chatstore.ErrCoreCredential) || errors.Is(err, chatstore.ErrCoreDatabase) {
		t.Fatalf("chat store still refuses the provisioned credential: %v", err)
	}
	if chat != nil {
		chat.Close()
	}
	if !strings.Contains(dsn, role+":") {
		t.Fatalf("dsn does not name the role: %q", dsn)
	}
}

func sameStoreURLs(a, b ServeConfig) bool {
	return a.ChatDatabaseURL == b.ChatDatabaseURL && a.AgentDatabaseURL == b.AgentDatabaseURL
}

func closedPool(t *testing.T) *pgxadapter.Pool {
	t.Helper()
	pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), pgtest.ServerURL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	return pool
}
