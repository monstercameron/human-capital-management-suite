package formdraftstore

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/forms/drafts"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func testTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func TestTodo_FORM_005_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := testTenant(t, db, "form-draft-recovery")
	otherTenant := testTenant(t, db, "form-draft-other")
	now := time.Date(2026, 9, 24, 12, 0, 0, 123456000, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	conn := appConn(t, db)
	first, err := New(conn, key, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	answers := []byte(`{"bank_account":"sensitive-value"}`)
	req := drafts.SaveRequest{
		ID: "draft-recovery-1", TenantID: tenant.String(), PrincipalID: "principal:worker-1",
		FormID: "form.direct-deposit", FormVersion: "v3", Answers: answers,
		ExpiresAt: now.Add(time.Hour),
	}
	saved, err := first.Save(req)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Revision != 1 || saved.AnswerDigest == [32]byte{} {
		t.Fatalf("saved draft = %+v, want revision 1 and digest", saved)
	}

	// A fresh repository value models process recovery: only the configured key
	// and PostgreSQL row survive between the save and the resume.
	recovered, err := New(conn, key, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := recovered.Resume(drafts.ResumeRequest{ID: req.ID, TenantID: req.TenantID,
		PrincipalID: req.PrincipalID, FormID: req.FormID, FormVersion: req.FormVersion, Revision: saved.Revision})
	if err != nil {
		t.Fatalf("Resume after repository recreation: %v", err)
	}
	if resumed.Outcome != drafts.Current || !bytes.Equal(resumed.Answers, answers) || resumed.Draft.AnswerDigest != saved.AnswerDigest {
		t.Fatalf("recovered draft = %+v, want intact answers and the original digest", resumed)
	}

	// The app role sees only ciphertext for the tenant-scoped row.
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	var sealed []byte
	if err := tx.QueryRow(context.Background(), `SELECT sealed_answers FROM form_draft WHERE tenant_id=$1 AND draft_id=$2`, tenant, req.ID).Scan(&sealed); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("read ciphertext: %v", err)
	}
	if bytes.Contains(sealed, answers) {
		_ = tx.Rollback(context.Background())
		t.Fatal("stored ciphertext contains plaintext answers")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}

	wrongPrincipal := drafts.ResumeRequest{ID: req.ID, TenantID: req.TenantID, PrincipalID: "principal:other",
		FormID: req.FormID, FormVersion: req.FormVersion, Revision: saved.Revision}
	if _, err := recovered.Resume(wrongPrincipal); !errors.Is(err, drafts.ErrDenied) {
		t.Fatalf("wrong-principal Resume error = %v, want ErrDenied", err)
	}
	wrongTenant := wrongPrincipal
	wrongTenant.TenantID = otherTenant.String()
	wrongTenant.PrincipalID = req.PrincipalID
	if _, err := recovered.Resume(wrongTenant); !errors.Is(err, drafts.ErrNotFound) {
		t.Fatalf("cross-tenant Resume error = %v, want non-disclosing ErrNotFound", err)
	}
	wrongKey, err := New(conn, []byte("abcdef0123456789abcdef0123456789"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongKey.Resume(drafts.ResumeRequest{ID: req.ID, TenantID: req.TenantID,
		PrincipalID: req.PrincipalID, FormID: req.FormID, FormVersion: req.FormVersion, Revision: saved.Revision}); !errors.Is(err, drafts.ErrDenied) {
		t.Fatalf("wrong-key Resume error = %v, want ErrDenied", err)
	}

	updated, err := recovered.Save(drafts.SaveRequest{ID: req.ID, TenantID: req.TenantID, PrincipalID: req.PrincipalID,
		FormID: req.FormID, FormVersion: req.FormVersion, ExpectedRevision: 1,
		Answers: []byte(`{"bank_account":"updated"}`), ExpiresAt: now.Add(2 * time.Hour)})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("CAS update = %+v, %v; want revision 2", updated, err)
	}
	stale := req
	stale.ExpectedRevision = 1
	if _, err := recovered.Save(stale); !errors.Is(err, drafts.ErrConflict) {
		t.Fatalf("stale Save error = %v, want ErrConflict", err)
	}

	result, err := recovered.Submit(drafts.ResumeRequest{ID: req.ID, TenantID: req.TenantID,
		PrincipalID: req.PrincipalID, FormID: req.FormID, FormVersion: req.FormVersion, Revision: updated.Revision},
		func(version string, got []byte) error {
			if version != req.FormVersion || !bytes.Equal(got, []byte(`{"bank_account":"updated"}`)) {
				return errors.New("unexpected validated form content")
			}
			return nil
		})
	if err != nil || result.Submission.DraftRevision != updated.Revision || !result.Effects.IsZero() {
		t.Fatalf("Submit = %+v, %v; want typed revision 2 with zero external effects", result, err)
	}

	tamperTx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tamperTx, tenant); err != nil {
		_ = tamperTx.Rollback(context.Background())
		t.Fatal(err)
	}
	var tampered []byte
	if err := tamperTx.QueryRow(context.Background(), `SELECT sealed_answers FROM form_draft WHERE tenant_id=$1 AND draft_id=$2`, tenant, req.ID).Scan(&tampered); err != nil {
		_ = tamperTx.Rollback(context.Background())
		t.Fatal(err)
	}
	tampered[len(tampered)-1] ^= 0x80
	if _, err := tamperTx.Exec(context.Background(), `UPDATE form_draft SET sealed_answers=$3 WHERE tenant_id=$1 AND draft_id=$2`, tenant, req.ID, tampered); err != nil {
		_ = tamperTx.Rollback(context.Background())
		t.Fatalf("tamper ciphertext: %v", err)
	}
	if err := tamperTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.Resume(drafts.ResumeRequest{ID: req.ID, TenantID: req.TenantID, PrincipalID: req.PrincipalID,
		FormID: req.FormID, FormVersion: req.FormVersion, Revision: updated.Revision}); !errors.Is(err, drafts.ErrDenied) {
		t.Fatalf("tampered ciphertext Resume error = %v, want ErrDenied", err)
	}
}
