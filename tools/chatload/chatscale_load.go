package chatload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const TestPrefix = "hcmnext_test_chatscale_"
const DiskLimit int64 = 6_000_000_000

type Config struct {
	schema      fs.FS
	DSN         string
	Root        string
	Database    string
	Resume      bool
	Seed        uint64
	Posts       int64
	Tenants     int
	Channels    int
	Batch       int
	Repeats     int
	Concurrency int
	Operations  int
}

func (c Config) Validate() error {
	if len(c.Database) > 63 {
		return errors.New("database name exceeds PostgreSQL identifier limit")
	}
	if !regexp.MustCompile(`^hcmnext_test_chatscale_[a-z0-9_]+$`).MatchString(c.Database) {
		return errors.New("database must start with hcmnext_test_chatscale_ and contain only lowercase letters, digits or underscores")
	}
	p, e := pgx.ParseConfig(c.DSN)
	if e != nil {
		return errors.New("invalid PostgreSQL configuration")
	}
	if (p.Host != "127.0.0.1" && p.Host != "localhost") || p.Port != 18540 {
		return errors.New("only shared test PostgreSQL at 127.0.0.1:18540 is permitted")
	}
	for _, fallback := range p.Fallbacks {
		if (fallback.Host != "127.0.0.1" && fallback.Host != "localhost") || fallback.Port != 18540 {
			return errors.New("fallback connection must use the shared test server")
		}
	}
	if c.Posts < 5 || c.Posts > 3000000 || c.Tenants < 1 || c.Channels < 300 || c.Channels%c.Tenants != 0 || c.Channels/c.Tenants < 300 || c.Batch < 1 || c.Repeats < 2 || c.Concurrency < 1 || c.Concurrency > 1000 || c.Operations < 8 {
		return errors.New("invalid size, shape or measurement bounds (maximum 3 million posts)")
	}
	return nil
}
func databaseConfig(c Config) *pgx.ConnConfig {
	p, _ := pgx.ParseConfig(c.DSN)
	p.Database = c.Database
	return p
}

// WithDatabase creates only a new test database and always drops exactly that
// database after the callback, even on failure. Existing databases are refused.
func WithDatabase(ctx context.Context, c Config, fn func(*pgx.Conn) error) (err error) {
	if err = c.Validate(); err != nil {
		return err
	}
	p, _ := pgx.ParseConfig(c.DSN)
	admin, e := pgx.ConnectConfig(ctx, p)
	if e != nil {
		return e
	}
	defer admin.Close(context.Background())
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{c.Database}.Sanitize())
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		e := dropDatabase(cleanup, admin, c.Database)
		err = errors.Join(err, e)
	}()
	conn, e := pgx.ConnectConfig(ctx, databaseConfig(c))
	if e != nil {
		return e
	}
	defer conn.Close(context.Background())
	return fn(conn)
}
func Migrate(ctx context.Context, c Config) error {
	if e := c.Validate(); e != nil {
		return e
	}
	db := stdlib.OpenDB(*databaseConfig(c))
	defer db.Close()
	if c.schema != nil {
		provider, e := goose.NewProvider(goose.DialectPostgres, db, c.schema)
		if e != nil {
			return e
		}
		_, e = provider.Up(ctx)
		return e
	}
	return migrate(ctx, db, filepath.Join(c.Root, "internal/data/chatstore/migrations"))
}
func migrate(ctx context.Context, db *sql.DB, dir string) error {
	provider, e := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(dir))
	if e != nil {
		return e
	}
	_, e = provider.Up(ctx)
	return e
}

type generatedPost struct {
	ID, Tenant, Channel, Author, Body, Parent, References string
	Sequence                                              int64
	Revision                                              int
	Removed                                               bool
	Created, Updated                                      time.Time
}

func tenantName(i int) string  { return fmt.Sprintf("t%03x", i) }
func channelName(i int) string { return fmt.Sprintf("c%06x", i) }
func postName(i int64) string  { return fmt.Sprintf("p%06x", i) }
func generate(c Config, i int64, sequences []int64) generatedPost {
	// Each row owns its PRNG state, making resumption independent of batch size.
	rng := rand.New(rand.NewPCG(c.Seed, uint64(i)))
	ch := 0
	if rng.IntN(100) < 65 {
		ch = rng.IntN(min(3, c.Channels))
	} else {
		ch = 3 + rng.IntN(c.Channels-3)
	}
	if (i+4)%5 == 0 {
		ch = rng.IntN(3)
	}
	seq := sequences[ch] + 1
	sequences[ch] = seq
	tenant := tenantName(ch / (c.Channels / c.Tenants))
	p := generatedPost{ID: postName(i), Tenant: tenant, Channel: channelName(ch), Author: fmt.Sprintf("a%02x", rng.IntN(100)), Sequence: seq, Revision: 1, References: "[]", Created: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second)}
	if seq == 1 {
		p.ID = "root-" + p.Channel
	}
	switch rng.IntN(3) {
	case 0:
		p.Body = "Payroll onboarding review ready."
	case 1:
		p.Body = "Please review payroll onboarding."
	default:
		p.Body = "Payroll onboarding: team feedback."
	}
	p.Updated = p.Created
	if seq > 1 && (i+4)%5 == 0 {
		p.Parent = "root-" + p.Channel
	}
	if i%10 == 0 {
		p.Revision = 2
		p.Updated = p.Created.Add(time.Hour)
	}
	p.Removed = i%50 == 0
	if i%7 == 0 {
		p.References = `[{"Kind":"PERSON_MENTION","TenantID":"` + tenant + `","ID":"reader"}]`
	}
	if i%13 == 0 {
		p.References = `[{"Kind":"MEDIA","ID":"fixture-attachment","Display":"Policy.pdf"}]`
	}
	return p
}

// Load resumes at the last committed COPY batch. The progress row, RNG seed and
// shape are committed atomically with all correlated fixture rows.
func Load(ctx context.Context, conn *pgx.Conn, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	var actual string
	if e := conn.QueryRow(ctx, "SELECT current_database()").Scan(&actual); e != nil {
		return e
	}
	if actual != c.Database {
		return errors.New("connection database does not match guarded test database")
	}
	_, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS chatscale_progress (id boolean PRIMARY KEY DEFAULT true CHECK(id),generator_version text NOT NULL DEFAULT 'chatscale-20261001-v3',seed text NOT NULL,tenants integer NOT NULL,channels integer NOT NULL,completed bigint NOT NULL,sequences bigint[] NOT NULL)`)
	if err != nil {
		return err
	}
	sequences := make([]int64, c.Channels)
	var completed int64
	var seed, version string
	var tenants, channels int
	err = conn.QueryRow(ctx, "SELECT generator_version,seed,tenants,channels,completed,sequences FROM chatscale_progress WHERE id").Scan(&version, &seed, &tenants, &channels, &completed, &sequences)
	if err == nil {
		if version != "chatscale-20261001-v3" || seed != fmt.Sprint(c.Seed) || tenants != c.Tenants || channels != c.Channels || completed > c.Posts-4 {
			return errors.New("resume shape or seed mismatch")
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		tx, e := conn.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		conv := make([][]any, 0, c.Channels)
		members := make([][]any, 0, c.Channels)
		roots := make([][]any, 0, c.Channels)
		for ch := 0; ch < c.Channels; ch++ {
			t := tenantName(ch / (c.Channels / c.Tenants))
			cid := channelName(ch)
			conv = append(conv, []any{cid, t, "PUBLIC_CHANNEL", cid, "reader"})
			members = append(members, []any{t, cid, "reader", t})
			if ch < 3 || ch == 299 {
				roots = append(roots, []any{"root-" + cid, t, cid, "a00", t, int64(1), "Discussion root", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)})
				sequences[ch] = 1
			}
		}
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{"chat_conversation"}, []string{"id", "tenant_id", "kind", "name", "owner_id"}, pgx.CopyFromRows(conv)); e != nil {
			return e
		}
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{"chat_membership"}, []string{"tenant_id", "conversation_id", "member_id", "home_tenant_id"}, pgx.CopyFromRows(members)); e != nil {
			return e
		}
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{"chat_post"}, []string{"id", "tenant_id", "conversation_id", "author_id", "author_home_tenant_id", "sequence", "body", "created_at"}, pgx.CopyFromRows(roots)); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO chatscale_progress(seed,tenants,channels,completed,sequences) VALUES($1,$2,$3,0,$4)", fmt.Sprint(c.Seed), c.Tenants, c.Channels, sequences)
		if e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	} else {
		return err
	}
	for completed < c.Posts-4 {
		end := min(completed+int64(c.Batch), c.Posts-4)
		tx, e := conn.Begin(ctx)
		if e != nil {
			return e
		}
		posts, revisions, reactions, outbox, receipts, audit, records := [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}
		for i := completed + 1; i <= end; i++ {
			p := generate(c, i, sequences)
			originalBody := p.Body
			if p.Revision > 1 {
				p.Body += " Edited."
			}
			if p.Removed {
				p.Body = ""
			}
			audit = append(audit, []any{p.Tenant, "e" + p.ID, i, p.Author, "post.created", "chat", p.ID, int64(0), "post.created", "fixture", p.Created, "fixture"})
			records = append(records, []any{p.Tenant, p.ID, p.Channel, "post", p.ID, int64(p.Revision), p.Created})
			posts = append(posts, []any{p.ID, p.Tenant, p.Channel, p.Author, p.Tenant, p.Sequence, p.Body, p.Parent, p.References, p.Revision, p.Removed, p.Created, p.Updated})
			revisions = append(revisions, []any{p.Tenant, p.ID, int64(1), p.Author, originalBody, p.Parent, p.References, false, p.Created})
			if p.Revision > 1 {
				revisions = append(revisions, []any{p.Tenant, p.ID, int64(2), p.Author, p.Body, p.Parent, p.References, p.Removed, p.Updated})
			}
			if i%4 == 0 {
				reactions = append(reactions, []any{p.Tenant, p.Tenant, p.ID, "reader", "thumbsup", p.Created})
			}
			payload := fmt.Sprintf(`{"ConversationID":%q,"TargetID":%q,"EventSequence":%d}`, p.Channel, p.ID, p.Sequence)
			outbox = append(outbox, []any{i, p.Tenant, p.ID, "post.created", payload, p.Created})
			if i%10 != 0 {
				receipts = append(receipts, []any{p.Tenant, i, p.Created})
			}
		}
		batches := []struct {
			table   string
			columns []string
			rows    [][]any
		}{{"chat_post", []string{"id", "tenant_id", "conversation_id", "author_id", "author_home_tenant_id", "sequence", "body", "parent_id", "references_json", "revision", "tombstoned", "created_at", "updated_at"}, posts}, {"chat_post_revision", []string{"tenant_id", "post_id", "revision", "author_id", "body", "parent_id", "references_json", "tombstoned", "created_at"}, revisions}, {"chat_reaction", []string{"tenant_id", "home_tenant_id", "post_id", "member_id", "emoji", "created_at"}, reactions}, {"chat_outbox", []string{"id", "tenant_id", "aggregate_id", "event_type", "payload", "created_at"}, outbox}, {"chat_outbox_receipt", []string{"tenant_id", "outbox_id", "published_at"}, receipts}, {"chat_audit_event", []string{"tenant_id", "event_id", "sequence", "actor_id", "action", "target_type", "target_id", "prior_revision", "reason", "policy_evidence", "at_time", "digest"}, audit}, {"chat_record_inventory", []string{"tenant_id", "record_id", "conversation_id", "kind", "source_id", "revision", "created_at"}, records}}
		for _, b := range batches {
			if _, e = tx.CopyFrom(ctx, pgx.Identifier{b.table}, b.columns, pgx.CopyFromRows(b.rows)); e != nil {
				tx.Rollback(ctx)
				return e
			}
		}
		_, e = tx.Exec(ctx, "UPDATE chatscale_progress SET completed=$1,sequences=$2", end, sequences)
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		completed = end
		var size int64
		if e = conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&size); e != nil {
			return e
		}
		if size > DiskLimit {
			return errors.New("6 GB disk limit exceeded")
		}
	}
	_, err = conn.Exec(ctx, `UPDATE chat_conversation c SET post_sequence=x.n,event_sequence=x.n FROM (SELECT tenant_id,conversation_id,max(sequence) n FROM chat_post GROUP BY tenant_id,conversation_id)x WHERE c.tenant_id=x.tenant_id AND c.id=x.conversation_id; SELECT setval(pg_get_serial_sequence('chat_outbox','id'),(SELECT max(id) FROM chat_outbox)); ANALYZE`)
	return err
}

func guardConnection(ctx context.Context, conn *pgx.Conn) error {
	var db string
	if e := conn.QueryRow(ctx, "SELECT current_database()").Scan(&db); e != nil {
		return e
	}
	if !strings.HasPrefix(db, TestPrefix) {
		return errors.New("measurement requires a guarded chatscale test database")
	}
	return nil
}

// WithResumedDatabase accepts only a database containing this generator's
// matching progress marker, then takes responsibility for dropping it. This
// supports recovery after abrupt termination, without adopting other fixtures.
func WithResumedDatabase(ctx context.Context, c Config, fn func(*pgx.Conn) error) (err error) {
	if err = c.Validate(); err != nil {
		return err
	}
	conn, e := pgx.ConnectConfig(ctx, databaseConfig(c))
	if e != nil {
		return e
	}
	defer conn.Close(context.Background())
	var seed, version string
	var tenants, channels int
	var completed int64
	if e = conn.QueryRow(ctx, "SELECT generator_version,seed,tenants,channels,completed FROM chatscale_progress WHERE id").Scan(&version, &seed, &tenants, &channels, &completed); e != nil {
		return e
	}
	if version != "chatscale-20261001-v3" || seed != fmt.Sprint(c.Seed) || tenants != c.Tenants || channels != c.Channels || completed > c.Posts-4 {
		return errors.New("resume database ownership marker mismatch")
	}
	p, _ := pgx.ParseConfig(c.DSN)
	admin, e := pgx.ConnectConfig(ctx, p)
	if e != nil {
		return e
	}
	defer admin.Close(context.Background())
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = conn.Close(cleanup)
		e := dropDatabase(cleanup, admin, c.Database)
		err = errors.Join(err, e)
	}()
	return fn(conn)
}

func dropDatabase(ctx context.Context, admin *pgx.Conn, name string) error {
	for attempt := 0; attempt < 20; attempt++ {
		_, e := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		if e == nil {
			return nil
		}
		var pgErr *pgconn.PgError
		if !errors.As(e, &pgErr) || pgErr.Code != "55006" {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("test database still has open connections after cleanup")
}
