package rulepayloadstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestPayloadPersistence(t *testing.T) {
	db := &memoryDB{rows: map[string]memoryRecord{}}
	store := New(db)
	tenantID := uuid.New()
	table := rules.PromotionApprovalThresholdTable()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: "rules.threshold", Version: "v1"}
	payload := rulepayload.Payload{Ref: ref, Kind: rulepayload.KindDecisionTable, Table: &table}

	if err := store.Publish(context.Background(), tenantID, payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(context.Background(), tenantID, payload); err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	encoded, err := rulepayload.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := rulepayload.Unmarshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Resolve(context.Background(), tenantID, ref, decoded.Digest)
	if err != nil || got.Digest != decoded.Digest || got.Table == nil {
		t.Fatalf("resolve = %+v, %v", got, err)
	}
	if _, err := store.Resolve(context.Background(), tenantID, ref, "sha256:wrong"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("wrong digest error = %v", err)
	}

	other := rules.PromotionApprovalThresholdTable()
	other.Rows[0].ID = "changed"
	if err := store.Publish(context.Background(), tenantID, rulepayload.Payload{Ref: ref, Kind: rulepayload.KindDecisionTable, Table: &other}); !errors.Is(err, ErrImmutable) {
		t.Fatalf("changed replay error = %v", err)
	}
	if db.tenantSettingCalls < 4 {
		t.Fatalf("tenant setting calls = %d", db.tenantSettingCalls)
	}
}

func TestTodo_WF_EXT_006_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "rulepayload-"+tenantID.String(), "rule payload integration")
	table := rules.PromotionApprovalThresholdTable()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	payload := rulepayload.Payload{Ref: ref, Kind: rulepayload.KindDecisionTable, Table: &table}
	store := New(db.Conn)
	if err := store.Publish(ctx, tenantID, payload); err != nil {
		t.Fatalf("publish: %v", err)
	}
	encoded, err := rulepayload.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := rulepayload.Unmarshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.Resolve(ctx, tenantID, ref, decoded.Digest)
	if err != nil || resolved.Digest != decoded.Digest {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	program := ir.Program{
		IRVersion: ir.IRVersion, DefinitionName: "integration.copy",
		Instructions: []ir.Instruction{{
			Op:          ir.OpProject,
			Sources:     []transformation.Path{{Schema: "workflow", Field: "source", Type: transformation.TypeString}},
			Destination: transformation.Path{Schema: "workflow", Field: "target", Type: transformation.TypeString},
		}},
		Limits: ir.Limits{MaxSteps: 1, MaxFanOut: 1},
	}
	transformRef := workflow.Reference{Kind: workflow.RefTransform, ID: "transform.integration.copy", Version: "1"}
	transformPayload := rulepayload.Payload{Ref: transformRef, Kind: rulepayload.KindTransform, Transform: &program}
	if err := store.Publish(ctx, tenantID, transformPayload); err != nil {
		t.Fatalf("publish transform: %v", err)
	}
	transformEncoded, err := rulepayload.Marshal(transformPayload)
	if err != nil {
		t.Fatal(err)
	}
	transformDecoded, err := rulepayload.Unmarshal(transformEncoded)
	if err != nil {
		t.Fatal(err)
	}
	transformResolved, err := store.Resolve(ctx, tenantID, transformRef, transformDecoded.Digest)
	if err != nil || transformResolved.Transform == nil || transformResolved.Digest != transformDecoded.Digest {
		t.Fatalf("resolve transform = %+v, %v", transformResolved, err)
	}
	otherTenant := uuid.New()
	if _, err := store.Resolve(ctx, otherTenant, ref, decoded.Digest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant resolve error = %v, want ErrNotFound", err)
	}
}

type memoryRecord struct{ digest, body string }
type memoryDB struct {
	mu                 sync.Mutex
	rows               map[string]memoryRecord
	tenantSettingCalls int
}
type memoryTx struct {
	db     *memoryDB
	tenant string
	writes map[string]memoryRecord
}
type memoryRow struct {
	record memoryRecord
	err    error
}

func (db *memoryDB) Begin(context.Context) (dbport.Tx, error) {
	return &memoryTx{db: db, writes: map[string]memoryRecord{}}, nil
}
func (tx *memoryTx) Exec(_ context.Context, query string, args ...any) (int64, error) {
	if strings.Contains(query, "set_config") {
		tx.tenant = args[1].(string)
		tx.db.mu.Lock()
		tx.db.tenantSettingCalls++
		tx.db.mu.Unlock()
		return 1, nil
	}
	if !strings.Contains(query, "INSERT INTO workflow_executable_payload") {
		return 0, nil
	}
	tenant := args[0].(uuid.UUID).String()
	if tenant != tx.tenant {
		return 0, errors.New("tenant context missing")
	}
	key := tenant + "/" + string(args[1].(workflow.ReferenceKind)) + "/" + args[2].(string) + "/" + args[3].(string)
	value := memoryRecord{digest: args[5].(string), body: args[6].(string)}
	tx.db.mu.Lock()
	_, exists := tx.db.rows[key]
	tx.db.mu.Unlock()
	if !exists {
		tx.writes[key] = value
	}
	return 1, nil
}
func (tx *memoryTx) QueryRow(_ context.Context, query string, args ...any) dbport.Row {
	if !strings.Contains(query, "FROM workflow_executable_payload") {
		return memoryRow{err: dbport.ErrNoRows}
	}
	tenant := args[0].(uuid.UUID).String()
	if tenant != tx.tenant {
		return memoryRow{err: dbport.ErrNoRows}
	}
	key := tenant + "/" + string(args[1].(workflow.ReferenceKind)) + "/" + args[2].(string) + "/" + args[3].(string)
	if row, ok := tx.writes[key]; ok {
		return memoryRow{record: row}
	}
	tx.db.mu.Lock()
	row, ok := tx.db.rows[key]
	tx.db.mu.Unlock()
	if !ok {
		return memoryRow{err: dbport.ErrNoRows}
	}
	return memoryRow{record: row}
}
func (*memoryTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (tx *memoryTx) Commit(context.Context) error {
	tx.db.mu.Lock()
	defer tx.db.mu.Unlock()
	for key, value := range tx.writes {
		if _, exists := tx.db.rows[key]; !exists {
			tx.db.rows[key] = value
		}
	}
	return nil
}
func (*memoryTx) Rollback(context.Context) error { return nil }
func (r memoryRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*string)) = r.record.digest
	*(dest[1].(*[]byte)) = []byte(r.record.body)
	return nil
}
