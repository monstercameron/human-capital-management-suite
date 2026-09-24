package hipaa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/privacymeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type certMutationBeginner struct {
	dbport.Beginner
	onCertRowsClosed func() error
}

func (b certMutationBeginner) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return certMutationTx{Tx: tx, onCertRowsClosed: b.onCertRowsClosed}, nil
}

type certMutationTx struct {
	dbport.Tx
	onCertRowsClosed func() error
}

func (t certMutationTx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	rows, err := t.Tx.Query(ctx, sql, args...)
	if err != nil || t.onCertRowsClosed == nil || !strings.Contains(sql, "FROM data_copy_inventory i") {
		return rows, err
	}
	return &certMutationRows{Rows: rows, mutate: t.onCertRowsClosed}, nil
}

type certMutationRows struct {
	dbport.Rows
	mutate func() error
	once   sync.Once
	err    error
}

func (r *certMutationRows) Close() {
	r.Rows.Close()
	r.once.Do(func() { r.err = r.mutate() })
}

func fixtureCopyInventory(at time.Time) CopyInventory {
	return hipaaInventoryWith(at, []CopyProcessor{
		{Subprocessor: "claims-clearinghouse", DataCategory: "MEDICAL", Fields: []string{"leave_status", "restriction_code"}},
		{Subprocessor: "records-vault", DataCategory: "MEDICAL", Fields: []string{"leave_status"}},
	})
}

func hipaaInventoryWith(at time.Time, processors []CopyProcessor) CopyInventory {
	return CopyInventory{TenantID: "tenant-test", Version: "records-copy-v1", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Watermark: "records-copy:2026-09-01T12:00:00Z", AsOf: at, CertifiedAt: at, CertifiedCopyCount: len(processors), Complete: true, Processors: processors}
}

func TestTodo_REV_099_02_Security(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	program := rev09902Program()
	valid := fixtureCopyInventory(at)
	if _, err := approveMedicalFlow(program, valid, "claims-clearinghouse", []string{"leave_status"}, at, 24*time.Hour); err != nil {
		t.Fatalf("approved minimum medical flow: %v", err)
	}
	valid.Processors = append(valid.Processors, CopyProcessor{Subprocessor: "payroll-vendor", DataCategory: "PII", Fields: []string{"name"}})
	valid.CertifiedCopyCount++
	if _, err := approveMedicalFlow(program, valid, "claims-clearinghouse", []string{"leave_status"}, at, 24*time.Hour); err != nil {
		t.Fatalf("nonmedical copy incorrectly established HIPAA applicability: %v", err)
	}
	valid = fixtureCopyInventory(at)
	cases := []struct {
		name      string
		program   HIPAABusinessAssociateProgram
		inventory CopyInventory
		fields    []string
		want      error
	}{
		{"unresolved coverage", func() HIPAABusinessAssociateProgram {
			p := program
			p.Applicability = ApplicabilityUnresolved
			return p
		}(), valid, []string{"leave_status"}, ErrCoverageUnclear},
		{"extra requested field", program, valid, []string{"diagnosis"}, ErrScopeExceedsMinimumNecessary},
		{"wildcard request", program, valid, []string{"*"}, ErrScopeExceedsMinimumNecessary},
		{"processor absent from BAA", program, hipaaInventoryWith(at, []CopyProcessor{{Subprocessor: "unlisted", DataCategory: "MEDICAL", Fields: []string{"leave_status"}}}), []string{"leave_status"}, ErrCopyInventoryMismatch},
		{"processor fields exceed BAA", program, hipaaInventoryWith(at, []CopyProcessor{{Subprocessor: "records-vault", DataCategory: "MEDICAL", Fields: []string{"restriction_code"}}}), []string{"leave_status"}, ErrScopeExceedsMinimumNecessary},
		{"stale copy census", program, fixtureCopyInventory(at.Add(-48 * time.Hour)), []string{"leave_status"}, ErrCopyInventoryMismatch},
		{"partial copy census", program, CopyInventory{Version: valid.Version, AsOf: at, Processors: valid.Processors}, []string{"leave_status"}, ErrCopyInventoryMismatch},
		{"copy census without digest binding", program, func() CopyInventory { i := hipaaInventoryWith(at, valid.Processors); i.Digest = ""; return i }(), []string{"leave_status"}, ErrCopyInventoryMismatch},
		{"copy census with malformed digest", program, func() CopyInventory {
			i := hipaaInventoryWith(at, valid.Processors)
			i.Digest = "sha256:copyset"
			return i
		}(), []string{"leave_status"}, ErrCopyInventoryMismatch},
		{"copy census certificate omits a processor", program, func() CopyInventory {
			i := hipaaInventoryWith(at, valid.Processors)
			i.CertifiedCopyCount--
			return i
		}(), []string{"leave_status"}, ErrCopyInventoryMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := approveMedicalFlow(tc.program, tc.inventory, "claims-clearinghouse", tc.fields, at, 24*time.Hour)
			if !errors.Is(err, tc.want) {
				t.Fatalf("approval error=%v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}
	missingCopy := program
	missingCopy.Agreements = append([]BusinessAssociateAgreement(nil), program.Agreements...)
	missingCopy.Agreements[0].CopyDigest = ""
	if err := missingCopy.Validate(); !errors.Is(err, ErrProgramInvalid) {
		t.Fatalf("BAA without current-copy digest validated: %v", err)
	}
	malformedCopy := program
	malformedCopy.Agreements = append([]BusinessAssociateAgreement(nil), program.Agreements...)
	malformedCopy.Agreements[0].CopyDigest = "sha256:claims"
	if err := malformedCopy.Validate(); !errors.Is(err, ErrProgramInvalid) {
		t.Fatalf("BAA with malformed current-copy digest validated: %v", err)
	}
	futureReview := program
	futureReview.Agreements = append([]BusinessAssociateAgreement(nil), program.Agreements...)
	futureReview.Agreements[0].ReviewedAt = at.Add(time.Hour)
	if _, err := approveMedicalFlow(futureReview, valid, "claims-clearinghouse", []string{"leave_status"}, at, 24*time.Hour); !errors.Is(err, ErrBAAExpired) {
		t.Fatalf("future-dated BAA copy review was accepted: %v", err)
	}
}

func TestTodo_REV_099_02_Integration(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	program := rev09902Program()
	inventory := rev09902StoredCopyInventory(t, at, false)
	approval, err := ApproveCertifiedMedicalFlow(program, inventory.Snapshot().TenantID, inventory, "records-vault", []string{"leave_status"}, at, 24*time.Hour)
	if err != nil {
		t.Fatalf("program + copy census + minimum scope flow: %v", err)
	}
	if approval.Reference != "BAA-2026-0008" {
		t.Fatalf("approval BAA=%s, want BAA-2026-0008", approval.Reference)
	}
	view := inventory.Snapshot()
	if view.TenantID == "" || view.CertifiedCopyCount != 2 || len(view.Processors) != 2 || view.Digest == "" {
		t.Fatalf("certified adapter lost tenant/copy evidence: %+v", view)
	}
	view.Processors[0].Fields[0] = "forged_field"
	if _, err := ApproveCertifiedMedicalFlow(program, inventory.Snapshot().TenantID, inventory, "records-vault", []string{"leave_status"}, at, 24*time.Hour); err != nil {
		t.Fatalf("detached snapshot mutation changed sealed certification: %v", err)
	}
	if _, err := ApproveCertifiedMedicalFlow(program, "tenant-other", inventory, "records-vault", []string{"leave_status"}, at, 24*time.Hour); !errors.Is(err, ErrCopyInventoryMismatch) {
		t.Fatalf("cross-tenant certified inventory approved: %v", err)
	}
}

func TestTodo_REV_099_02_TenantBinding(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	inventory := fixtureCopyInventory(at)
	if _, err := ApproveCertifiedMedicalFlow(rev09902Program(), "tenant-other", CertifiedCopyInventory{inventory}, "claims-clearinghouse", []string{"leave_status"}, at, 24*time.Hour); !errors.Is(err, ErrCopyInventoryMismatch) {
		t.Fatalf("cross-tenant HIPAA approval = %v, want ErrCopyInventoryMismatch", err)
	}
}

func rev09902StoredCopyInventory(t *testing.T, at time.Time, concurrentMutation bool) CertifiedCopyInventory {
	t.Helper()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	if _, err := db.Conn.Exec(ctx, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local','HIPAA test tenant','ACTIVE',$3)`, tenant, "hipaa-"+uuid.NewString(), at.Add(-time.Hour)); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set app role: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant tx: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	asset := "asset:hipaa:" + uuid.NewString()
	inventoryID := uuid.New()
	watermark := "catalog:hipaa-1"
	digest := sha256.Sum256([]byte(watermark))
	copySet := privacymeta.DataCopyInventory{TenantID: tenant, InventoryID: inventoryID, CanonicalAssetKey: asset, AsOf: at, SourceWatermark: watermark, ExpectedSources: json.RawMessage(`[]`), SourceWatermarks: json.RawMessage(`{}`), Completeness: "COMPLETE", ContentDigest: hex.EncodeToString(digest[:]), CreatedAt: at}
	// A copy census needs a nonempty, covered source; insert two processors under one source.
	copySet.ExpectedSources = json.RawMessage(`["catalog"]`)
	copySet.SourceWatermarks = json.RawMessage(`{"catalog":"wm-1"}`)
	copyDigest := sha256.Sum256([]byte("copy-set"))
	copySet.ContentDigest = hex.EncodeToString(copyDigest[:])
	if err := privacymeta.InsertDataCopyInventory(ctx, tx, copySet); err != nil {
		t.Fatalf("insert copy census: %v", err)
	}
	var vaultCopyID uuid.UUID
	for i, processor := range []struct{ ref, fields string }{{"claims-clearinghouse", `{"fields":["leave_status","restriction_code"]}`}, {"records-vault", `{"fields":["leave_status"]}`}} {
		verified := at
		copyID := uuid.New()
		if processor.ref == "records-vault" {
			vaultCopyID = copyID
		}
		copy := privacymeta.DataCopy{TenantID: tenant, CopyID: copyID, InventoryID: inventoryID, CanonicalAssetKey: asset, CopyType: "PROVIDER", StoreRef: fmt.Sprintf("provider-store-%d", i), DiscoverySource: "catalog", SubjectRef: "employee:opaque", DataCategory: "MEDICAL", ProcessorRef: processor.ref, Region: "us-east", FieldScope: json.RawMessage(processor.fields), EncryptionKeyRef: "kms://medical-key", RetentionScheduleKey: "schedule:medical", HoldState: "NONE", DeletionCapability: "DELETE", RestorePolicy: "REAPPLY_TOMBSTONES", LastVerifiedAt: &verified, CreatedAt: at}
		if err := privacymeta.InsertDataCopy(ctx, tx, copy); err != nil {
			t.Fatalf("insert copy %s: %v", processor.ref, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit copy census: %v", err)
	}
	if concurrentMutation {
		var mutationErr error
		performMutation := func() error {
			mutationConn := db.NewConn(t)
			defer mutationConn.Close(context.Background())
			if _, err := mutationConn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
				return err
			}
			mutationTx, err := mutationConn.Begin(ctx)
			if err != nil {
				return err
			}
			defer mutationTx.Rollback(ctx)
			if err := tenancy.WithTenant(ctx, mutationTx, tenant); err != nil {
				return err
			}
			changed, err := mutationTx.Exec(ctx, `UPDATE data_copy SET field_scope=$1 WHERE tenant_id=$2 AND inventory_id=$3 AND copy_id=$4`, json.RawMessage(`{"fields":["diagnosis"]}`), tenant, inventoryID, vaultCopyID)
			if err != nil {
				return err
			}
			if changed != 1 {
				return fmt.Errorf("concurrent update changed %d rows, want 1", changed)
			}
			return mutationTx.Commit(ctx)
		}
		mutator := func() error { mutationErr = performMutation(); return mutationErr }
		inventory, err := CertifyTenantCopyInventory(ctx, certMutationBeginner{Beginner: conn, onCertRowsClosed: mutator}, tenant, inventoryID, at, 24*time.Hour)
		if err != nil {
			t.Fatalf("certify with concurrent mutation: %v", err)
		}
		if mutationErr != nil {
			t.Fatalf("concurrent copy mutation: %v", mutationErr)
		}
		return inventory
	}
	return loadStoredCopyInventory(t, at, conn, tenant, inventoryID)
}

func TestTodo_REV_099_02_SecurityConcurrentCopyUpdate(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	inventory := rev09902StoredCopyInventory(t, at, true)
	view := inventory.Snapshot()
	for _, copy := range view.Processors {
		if copy.Subprocessor == "records-vault" && (len(copy.Fields) != 1 || copy.Fields[0] != "leave_status") {
			t.Fatalf("certified scope mixed a concurrent post-certificate update: %+v", copy)
		}
	}
	if _, err := ApproveCertifiedMedicalFlow(rev09902Program(), view.TenantID, inventory, "records-vault", []string{"leave_status"}, at, 24*time.Hour); err != nil {
		t.Fatalf("approval using consistent pre-update certificate snapshot: %v", err)
	}
}

func loadStoredCopyInventory(t *testing.T, at time.Time, conn *pgxadapter.Conn, tenant, inventoryID uuid.UUID) CertifiedCopyInventory {
	t.Helper()
	ctx := context.Background()
	inventory, err := CertifyTenantCopyInventory(ctx, conn, tenant, inventoryID, at, 24*time.Hour)
	if err != nil {
		t.Fatalf("certify and adapt persisted copy census: %v", err)
	}
	if _, err := CertifyTenantCopyInventory(ctx, conn, uuid.New(), inventoryID, at, 24*time.Hour); !errors.Is(err, ErrCopyInventoryMismatch) {
		t.Fatalf("foreign tenant inventory read/certify = %v, want ErrCopyInventoryMismatch", err)
	}
	return inventory
}

func TestTodo_REV_099_02_Mutation(t *testing.T) {
	base := StandardBreachMatrixExtension("breach-matrix-v3")
	if err := base.Validate(); err != nil {
		t.Fatalf("baseline HIPAA clocks: %v", err)
	}
	mutations := []struct {
		name   string
		mutate func(*BreachMatrixExtension)
	}{
		{"remove annual small breach clock", func(m *BreachMatrixExtension) { m.Rules = m.Rules[:len(m.Rules)-2] }},
		{"extend individual deadline", func(m *BreachMatrixExtension) { m.Rules[0].DeadlineDays = 61 }},
		{"remove media threshold", func(m *BreachMatrixExtension) { m.Rules[1].Threshold = 501 }},
		{"detach from PRIV-009 version", func(m *BreachMatrixExtension) { m.PRIV009MatrixVersion = "" }},
		{"remove law-enforcement delay rule", func(m *BreachMatrixExtension) { m.Exception = "" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			m := StandardBreachMatrixExtension("breach-matrix-v3")
			mutation.mutate(&m)
			if err := m.Validate(); !errors.Is(err, ErrBreachMatrixInvalid) {
				t.Fatalf("mutated matrix validated: %v", err)
			}
		})
	}
}
