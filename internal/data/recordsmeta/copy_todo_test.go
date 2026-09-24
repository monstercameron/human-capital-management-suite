package recordsmeta_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
)

func TestTodo_DATA_018(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "data018")
	conn := appConn(t, db)
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,$2,1,$2,'PROTOBUF','EVIDENCE_MANIFEST')`, tenant, recordsmeta.RecordsCopySchemaRef)
	declaration := newDeclaration(tenant)
	hold := newHold(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(context.Background(), tx, declaration); err != nil {
			return err
		}
		if _, err := recordsmeta.RegisterCopy(context.Background(), tx, recordsmeta.CopyLink{TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL", StoreRef: "ledger:" + uuid.NewString()}); err != nil {
			return err
		}
		if err := recordsmeta.InsertLegalHold(context.Background(), tx, hold); err != nil {
			return err
		}
		_, err := recordsmeta.PropagateHold(context.Background(), tx, tenant, declaration.DeclarationID, hold.HoldID, fixedInstant)
		return err
	})
	var copies []recordsmeta.CopyLink
	var listErr error
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		copies, listErr = recordsmeta.ListCopies(context.Background(), tx, tenant, declaration.DeclarationID)
		return listErr
	})
	if listErr != nil || len(copies) != 1 || copies[0].HoldState != "HELD" || copies[0].DispositionState != "HELD" {
		t.Fatalf("copies = %+v, err=%v", copies, listErr)
	}
	var outboxRows int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE tenant_id=$1 AND effect_identity LIKE 'records.hold.propagated:%'`, tenant).Scan(&outboxRows); err != nil || outboxRows != 1 {
		t.Fatalf("hold outbox rows = %d, err=%v", outboxRows, err)
	}
}

func TestTodo_DATA_018_Golden(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "data018-golden")
	conn := appConn(t, db)
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,$2,1,$2,'PROTOBUF','EVIDENCE_MANIFEST')`, tenant, recordsmeta.RecordsCopySchemaRef)
	declaration := newDeclaration(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(context.Background(), tx, declaration); err != nil {
			return err
		}
		_, err := recordsmeta.RegisterCopy(context.Background(), tx, recordsmeta.CopyLink{TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "EXPORT", StoreRef: "archive:fixed-export-1"})
		return err
	})
	var copies []recordsmeta.CopyLink
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		copies, err = recordsmeta.ListCopies(context.Background(), tx, tenant, declaration.DeclarationID)
		return err
	})
	if len(copies) != 1 || copies[0].CopyType != "EXPORT" || copies[0].StoreRef != "archive:fixed-export-1" || copies[0].DeclarationID != declaration.DeclarationID {
		t.Fatalf("copy round trip = %+v", copies)
	}
}

func TestTodo_DATA_018_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "data018-isolation")
	other := insertTenant(t, db, "data018-other")
	conn := appConn(t, db)
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,$2,1,$2,'PROTOBUF','EVIDENCE_MANIFEST')`, tenant, recordsmeta.RecordsCopySchemaRef)
	declaration := newDeclaration(tenant)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(context.Background(), tx, declaration); err != nil {
			return err
		}
		_, err := recordsmeta.RegisterCopy(context.Background(), tx, recordsmeta.CopyLink{TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "BACKUP", StoreRef: "backup:tenant-a"})
		return err
	})
	var copies []recordsmeta.CopyLink
	inTenantTx(t, conn, other, func(tx dbport.Tx) error {
		var err error
		copies, err = recordsmeta.ListCopies(context.Background(), tx, other, declaration.DeclarationID)
		return err
	})
	if len(copies) != 0 {
		t.Fatalf("foreign tenant observed copies: %+v", copies)
	}
}
