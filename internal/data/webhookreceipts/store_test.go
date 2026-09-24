package webhookreceipts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type storedReceipt struct {
	digest        string
	requestDigest string
	raw           []byte
	parsed        string
}

type memoryDB struct {
	records    map[string]storedReceipt
	outbox     map[string]bool
	commits    int
	failCommit bool
	failOutbox bool
}

func newMemoryDB() *memoryDB {
	return &memoryDB{records: make(map[string]storedReceipt), outbox: make(map[string]bool)}
}

func (d *memoryDB) Begin(context.Context) (dbport.Tx, error) {
	copyRows := make(map[string]storedReceipt, len(d.records))
	for key, row := range d.records {
		row.raw = append([]byte(nil), row.raw...)
		copyRows[key] = row
	}
	copyOutbox := make(map[string]bool, len(d.outbox))
	for key, value := range d.outbox {
		copyOutbox[key] = value
	}
	return &memoryTx{db: d, records: copyRows, outbox: copyOutbox}, nil
}

type memoryTx struct {
	db      *memoryDB
	records map[string]storedReceipt
	outbox  map[string]bool
	scoped  bool
	done    bool
}

func (tx *memoryTx) Exec(_ context.Context, query string, args ...any) (int64, error) {
	if strings.Contains(query, "set_config") {
		tx.scoped = true
		return 1, nil
	}
	if !tx.scoped {
		return 0, errors.New("tenant scope missing")
	}
	if strings.Contains(query, "INSERT INTO integration_webhook_outbox") {
		if tx.db.failOutbox {
			return 0, errors.New("outbox insert failed")
		}
		key := receiptKey(args[0], args[2], args[1])
		tx.outbox[key] = true
		return 1, nil
	}
	if !strings.Contains(query, "INSERT INTO integration_webhook_receipt") {
		return 0, fmt.Errorf("unexpected Exec: %s", query)
	}
	key := receiptKey(args[0], args[2], args[4])
	if _, exists := tx.records[key]; exists {
		return 0, nil
	}
	tx.records[key] = storedReceipt{digest: args[7].(string), requestDigest: args[8].(string), raw: append([]byte(nil), args[9].([]byte)...), parsed: args[10].(string)}
	return 1, nil
}

func (tx *memoryTx) QueryRow(_ context.Context, query string, args ...any) dbport.Row {
	if !tx.scoped {
		return memoryRow{err: errors.New("tenant scope missing")}
	}
	if strings.Contains(query, "SELECT request_digest") {
		row, ok := tx.records[receiptKey(args[0], args[1], args[2])]
		if !ok {
			return memoryRow{err: dbport.ErrNoRows}
		}
		return memoryRow{values: []any{row.requestDigest}}
	}
	if strings.Contains(query, "SELECT payload_bytes, parsed_receipt::text") {
		row, ok := tx.records[receiptKey(args[0], args[1], args[3])]
		if !ok {
			return memoryRow{err: dbport.ErrNoRows}
		}
		return memoryRow{values: []any{append([]byte(nil), row.raw...), row.parsed}}
	}
	return memoryRow{err: fmt.Errorf("unexpected QueryRow: %s", query)}
}

func (tx *memoryTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (tx *memoryTx) Commit(context.Context) error {
	if tx.db.failCommit {
		return errors.New("commit failed")
	}
	tx.db.records = tx.records
	tx.db.outbox = tx.outbox
	tx.db.commits++
	tx.done = true
	return nil
}

func (tx *memoryTx) Rollback(context.Context) error { tx.done = true; return nil }

type memoryRow struct {
	values []any
	err    error
}

func (r memoryRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan arity %d != %d", len(dest), len(r.values))
	}
	for i, value := range r.values {
		switch out := dest[i].(type) {
		case *string:
			*out = value.(string)
		case *[]byte:
			*out = append([]byte(nil), value.([]byte)...)
		default:
			return fmt.Errorf("unsupported scan destination %T", dest[i])
		}
	}
	return nil
}

type memoryRows struct{}

func (memoryRows) Next() bool        { return false }
func (memoryRows) Scan(...any) error { return nil }
func (memoryRows) Err() error        { return nil }
func (memoryRows) Close()            {}

func receiptKey(tenant, provider, event any) string {
	return fmt.Sprint(tenant, "|", provider, "|", event)
}

func sampleParsed() providerreceipt.Parsed {
	raw := []byte("{ \"event_id\":\"event-1\",\"outcome\":\"APPLIED\"}\n")
	return providerreceipt.Parsed{Provider: "payroll", EventID: "event-1", EventType: providerreceipt.PayrollEventApplied,
		Schema: providerreceipt.PayrollSchema, TenantID: "8f4d7d0e-e402-4eb3-8d22-770e716471b5", ChangeRef: "change-1",
		CorrelationKey: "corr-1", Outcome: providerreceipt.OutcomeApplied, Payload: raw, PayloadDigest: digest(raw), Details: map[string]string{"currency": "USD"}}
}

func TestStoreRecordAndReplaySurviveRecomposition(t *testing.T) {
	db := newMemoryDB()
	tenant := uuid.MustParse("8f4d7d0e-e402-4eb3-8d22-770e716471b5")
	scope := Scope{TenantID: tenant, Provider: "payroll", EndpointID: "endpoint-1"}
	first, err := New(db, scope)
	if err != nil {
		t.Fatal(err)
	}
	parsed := sampleParsed()
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	duplicate, err := first.Record(context.Background(), parsed, at)
	if err != nil || duplicate || db.commits != 1 || len(db.outbox) != 1 {
		t.Fatalf("first record duplicate=%v commits=%d outbox=%d err=%v", duplicate, db.commits, len(db.outbox), err)
	}
	duplicate, err = first.Record(context.Background(), parsed, at.Add(time.Second))
	if err != nil || !duplicate || db.commits != 2 || len(db.outbox) != 1 {
		t.Fatalf("identical record duplicate=%v commits=%d outbox=%d err=%v", duplicate, db.commits, len(db.outbox), err)
	}
	changed := parsed
	changed.Payload = []byte("{\"event_id\":\"event-1\",\"outcome\":\"APPLIED\"}")
	changed.PayloadDigest = digest(changed.Payload)
	if _, err := first.Record(context.Background(), changed, at); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed event bytes error=%v", err)
	}
	changedEnvelope := parsed
	changedEnvelope.EventType = providerreceipt.PayrollEventRejected
	if _, err := first.Record(context.Background(), changedEnvelope, at); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed event envelope error=%v", err)
	}

	// Recompose the store as a new process would while retaining only database state.
	restarted, err := New(db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Replay(context.Background(), parsed.EventID, webhook.ReplayApproval{}); !errors.Is(err, webhook.ErrReplayNotAuthorized) {
		t.Fatalf("unapproved replay error=%v", err)
	}
	replayed, err := restarted.Replay(context.Background(), parsed.EventID, webhook.ReplayApproval{Actor: "operator-1", Purpose: "incident recovery", Approved: true})
	if err != nil || string(replayed.Payload) != string(parsed.Payload) || replayed.PayloadDigest != parsed.PayloadDigest || replayed.Details["currency"] != "USD" {
		t.Fatalf("replay parsed=%+v err=%v", replayed, err)
	}
}

func TestStoreRefusalDoesNotCommitAndRequiresTenantMatch(t *testing.T) {
	db := newMemoryDB()
	scope := Scope{TenantID: uuid.MustParse(sampleParsed().TenantID), Provider: "payroll", EndpointID: "endpoint-1"}
	store, err := New(db, scope)
	if err != nil {
		t.Fatal(err)
	}
	parsed := sampleParsed()
	parsed.TenantID = uuid.NewString()
	if _, err := store.Record(context.Background(), parsed, time.Now().UTC()); !errors.Is(err, ErrInvalid) || len(db.records) != 0 {
		t.Fatalf("cross-tenant record err=%v rows=%d", err, len(db.records))
	}
	parsed = sampleParsed()
	db.failCommit = true
	if _, err := store.Record(context.Background(), parsed, time.Now().UTC()); err == nil || len(db.records) != 0 || len(db.outbox) != 0 {
		t.Fatalf("failed commit err=%v rows=%d outbox=%d", err, len(db.records), len(db.outbox))
	}
	db.failCommit = false
	db.failOutbox = true
	if _, err := store.Record(context.Background(), parsed, time.Now().UTC()); err == nil || len(db.records) != 0 || len(db.outbox) != 0 {
		t.Fatalf("failed outbox err=%v rows=%d outbox=%d", err, len(db.records), len(db.outbox))
	}
}

func TestNewRequiresCompleteTenantEndpointScope(t *testing.T) {
	for _, scope := range []Scope{{}, {TenantID: uuid.New(), Provider: "other", EndpointID: "ep"}, {TenantID: uuid.New(), Provider: "iam"}} {
		if _, err := New(newMemoryDB(), scope); !errors.Is(err, ErrInvalid) {
			t.Fatalf("New(%+v) error=%v", scope, err)
		}
	}
}
