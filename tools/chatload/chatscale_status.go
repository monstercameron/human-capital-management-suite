package chatload

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Status reports only this harness's disposable databases on the test server.
func Status(ctx context.Context, c Config) ([]string, error) {
	c.Database = TestPrefix + "status"
	c.Posts = 5
	if e := c.Validate(); e != nil {
		return nil, e
	}
	conn, e := pgx.Connect(ctx, c.DSN)
	if e != nil {
		return nil, e
	}
	defer conn.Close(ctx)
	rows, e := conn.Query(ctx, `SELECT datname,coalesce(state,'background'),coalesce(wait_event_type,''),coalesce(wait_event,''),coalesce(extract(epoch from(clock_timestamp()-query_start))::float8,0),coalesce(left(query,200),'') FROM pg_stat_activity WHERE left(datname,length($1))=$1 ORDER BY datname,pid`, TestPrefix)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []string
	databases := map[string]bool{}
	for rows.Next() {
		var db, state, kind, event, query string
		var age float64
		if e = rows.Scan(&db, &state, &kind, &event, &age, &query); e != nil {
			return nil, e
		}
		out = append(out, fmt.Sprintf("%s %s %s/%s %.1fs %s", db, state, kind, event, age, query))
		databases[db] = true
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	for name := range databases {
		c.Database = name
		if c.Validate() != nil {
			continue
		}
		target, e := pgx.ConnectConfig(ctx, databaseConfig(c))
		if e != nil {
			continue
		}
		var completed, bytes int64
		e = target.QueryRow(ctx, "SELECT completed,pg_database_size(current_database()) FROM chatscale_progress WHERE id").Scan(&completed, &bytes)
		_ = target.Close(ctx)
		if e == nil {
			out = append(out, fmt.Sprintf("%s committed_generated_posts=%d database_bytes=%d", name, completed, bytes))
		}
	}
	return out, nil
}

// CancelMeasurement cancels SQL statements in exactly one named test database.
// It does not terminate any backend or server process. The interrupted harness
// performs its usual deferred cleanup; matching progress proves ownership.
func CancelMeasurement(ctx context.Context, c Config) (int, error) {
	if e := c.Validate(); e != nil {
		return 0, e
	}
	target, e := pgx.ConnectConfig(ctx, databaseConfig(c))
	if e != nil {
		return 0, e
	}
	targetPID := target.PgConn().PID()
	var seed string
	var tenants, channels int
	e = target.QueryRow(ctx, "SELECT seed,tenants,channels FROM chatscale_progress WHERE id").Scan(&seed, &tenants, &channels)
	_ = target.Close(ctx)
	if e != nil {
		return 0, e
	}
	if seed != fmt.Sprint(c.Seed) || tenants != c.Tenants || channels != c.Channels {
		return 0, fmt.Errorf("cancel database ownership marker mismatch")
	}
	admin, e := pgx.Connect(ctx, c.DSN)
	if e != nil {
		return 0, e
	}
	defer admin.Close(ctx)
	var n int
	e = admin.QueryRow(ctx, `SELECT count(*) FROM (SELECT pg_cancel_backend(pid) ok FROM pg_stat_activity WHERE datname=$1 AND state='active' AND pid<>pg_backend_pid() AND pid<>$2) cancelled WHERE ok`, c.Database, targetPID).Scan(&n)
	return n, e
}
