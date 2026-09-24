package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

var rev028Clock = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func TestTodo_REV_028_01(t *testing.T) {
	policy, ok := operator.PolicyFor(operator.KindLedgerCorrection)
	if !ok || policy.Material == false || policy.DualControl || !policy.SimulationRequired || len(policy.Roles) != 1 || policy.Roles[0] != jit.RoleIntegrityRepair {
		t.Fatalf("ledger correction policy = %+v (known=%t), want material, simulated, single-operator integrity repair", policy, ok)
	}
	base := ledgerCorrectionInput{TenantID: uuid.New(), StreamKey: "worker:1", TargetSequence: 1, ExpectedHead: 1,
		Operator: "operator:ana", Reason: "fix fact", Authority: "authority:hr", SchemaRef: "hcmnext.people.v1.Compensation@1",
		Payload: "125000", OccurredAt: rev028Clock, EffectiveAt: rev028Clock}
	if err := validateLedgerCorrectionInput(base); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	base.Payload, base.ArtifactRef = "x", "artifact:1"
	if err := validateLedgerCorrectionInput(base); err == nil {
		t.Fatal("request with both payload forms was accepted")
	}
}

func TestTodo_REV_028_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := migopConn(t, db)
	tenant := migopInsertTenant(t, db, "rev028-ledger-correction")
	const stream = "worker:rev028"
	const schemaRef = "hcmnext.people.v1.Compensation@1"
	const authorityRef = "authority:rev028"
	ctx := context.Background()
	if err := migopTx(conn, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1, $2, 'hcmnext.people.v1.Compensation', 1, 'hcmnext.people.v1.Compensation', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, schemaRef); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from) VALUES ($1, $2, 'INTERNAL', 'people', timestamptz '2026-01-01T00:00:00Z')`, tenant, authorityRef); err != nil {
			return err
		}
		if err := ledger.EnsureStream(ctx, tx, tenant, stream, "WORKER", stream); err != nil {
			return err
		}
		_, err := ledger.New(ledger.WithClock(func() time.Time { return rev028Clock })).Append(ctx, tx, ledger.AppendRequest{
			Tenant: tenant, StreamKey: stream, ExpectedHead: 0, AssertionClass: ledger.DomainFact,
			Authority: authorityRef, SourceRef: "test:rev028-origin", SchemaRef: schemaRef, Payload: []byte("120000"),
			OccurredAt: rev028Clock, EffectiveAt: rev028Clock, CorrelationID: uuid.New(), IdempotencyKey: "origin",
		})
		return err
	}); err != nil {
		t.Fatalf("seed ledger origin: %v", err)
	}

	args := []string{"-database-url", "postgres://test", "-tenant", tenant.String(), "-stream", stream,
		"-target-sequence", "1", "-expected-head", "1", "-operator", "operator:ana", "-reason", "wrong compensation amount",
		"-authority", authorityRef, "-schema-ref", schemaRef, "-payload", "125000", "-occurred-at", rev028Clock.Format(time.RFC3339Nano),
		"-effective-at", rev028Clock.AddDate(0, 1, 0).Format(time.RFC3339Nano), "-source-ref", "test:rev028", "-idempotency-key", "rev028-key"}
	call := func() (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := runLedgerCorrect(args, &stdout, &stderr, func() time.Time { return rev028Clock }, func(context.Context, string) (dbport.Beginner, func(), error) {
			return migopConn(t, db), func() {}, nil
		})
		return code, stdout.String(), stderr.String()
	}
	broadScope, err := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIntegrityRepair), TicketRef: "INC-028-BROAD", Justification: "broad grant must not authorize a correction",
		Capabilities: []string{string(operator.KindLedgerCorrection)}, Purpose: "ledger correction"})
	if err != nil {
		t.Fatal(err)
	}
	if err := truststore.New(conn).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-rev028-broad",
		Revision: 1, State: "ACTIVE", Requester: "operator:ana", Approver: "operator:lead", Scope: broadScope,
		NotBefore: rev028Clock.Add(-time.Minute), ExpiresAt: rev028Clock.Add(time.Hour)}); err != nil {
		t.Fatalf("record broad JIT grant: %v", err)
	}
	code, _, denied := call()
	if code == 0 || !strings.Contains(denied, operator.CodeAuthorityRequired) {
		t.Fatalf("with broad but unscoped JIT grant: exit %d, error %q", code, denied)
	}
	if n := rev028EventCount(t, conn, tenant); n != 1 {
		t.Fatalf("ledger rows after denied action = %d, want 1", n)
	}

	scope, err := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIntegrityRepair), TicketRef: "INC-028", Justification: "correct compensation",
		Capabilities: []string{string(operator.KindLedgerCorrection)},
		Fields:       []string{ledgerCorrectionTargetField(stream, 1)}, Purpose: "ledger correction"})
	if err != nil {
		t.Fatal(err)
	}
	if err := truststore.New(conn).PutJITGrant(ctx, tenant, truststore.JITGrantRecord{TenantID: tenant, RowID: uuid.New(), GrantID: "jit-rev028",
		Revision: 1, State: "ACTIVE", Requester: "operator:ana", Approver: "operator:lead", Scope: scope,
		NotBefore: rev028Clock.Add(-time.Minute), ExpiresAt: rev028Clock.Add(time.Hour)}); err != nil {
		t.Fatalf("record JIT grant: %v", err)
	}
	code, out, stderr := call()
	if code != 0 || !strings.Contains(out, "outcome: APPLIED") || !strings.Contains(out, "correction=worker:rev028@2 effective=worker:rev028@2") {
		t.Fatalf("governed correction: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if n := rev028EventCount(t, conn, tenant); n != 2 {
		t.Fatalf("ledger rows after correction = %d, want 2", n)
	}
	if err := migopTx(conn, tenant, func(tx dbport.Tx) error {
		current, path, err := lineage.EffectiveCurrent(ctx, tx, tenant, ledger.EventRef{StreamKey: stream, Sequence: 1})
		if err != nil {
			return err
		}
		if current.Ref.Sequence != 2 || len(path) != 1 || path[0].Ref.Sequence != 2 || current.AssertionClass != ledger.Correction {
			t.Fatalf("effective current = %+v path=%+v, want correction sequence 2", current, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("read effective current: %v", err)
	}

	if _, err := truststore.New(conn).UpdateJITGrant(ctx, tenant, "jit-rev028", 1, "REVOKED", true); err != nil {
		t.Fatalf("revoke correction grant before replay: %v", err)
	}
	code, out, stderr = call()
	if code != 0 || !strings.Contains(out, "outcome: DUPLICATE") {
		t.Fatalf("idempotent replay: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if n := rev028EventCount(t, conn, tenant); n != 2 {
		t.Fatalf("ledger rows after replay = %d, want 2", n)
	}

	otherTenant := migopInsertTenant(t, db, "rev028-other-tenant")
	otherScope, err := json.Marshal(truststore.JITGrantScope{Role: string(jit.RoleIntegrityRepair), TicketRef: "INC-028-OTHER", Justification: "tenant isolation test",
		Capabilities: []string{string(operator.KindLedgerCorrection)}, Fields: []string{ledgerCorrectionTargetField(stream, 1)}, Purpose: "ledger correction"})
	if err != nil {
		t.Fatal(err)
	}
	if err := truststore.New(conn).PutJITGrant(ctx, otherTenant, truststore.JITGrantRecord{TenantID: otherTenant, RowID: uuid.New(), GrantID: "jit-rev028-other",
		Revision: 1, State: "ACTIVE", Requester: "operator:ana", Approver: "operator:lead", Scope: otherScope,
		NotBefore: rev028Clock.Add(-time.Minute), ExpiresAt: rev028Clock.Add(time.Hour)}); err != nil {
		t.Fatalf("record other-tenant JIT grant: %v", err)
	}
	otherArgs := append([]string(nil), args...)
	for i := 0; i < len(otherArgs)-1; i++ {
		if otherArgs[i] == "-tenant" {
			otherArgs[i+1] = otherTenant.String()
			break
		}
	}
	var foreignOut, foreignErr bytes.Buffer
	if code := runLedgerCorrect(otherArgs, &foreignOut, &foreignErr, func() time.Time { return rev028Clock }, func(context.Context, string) (dbport.Beginner, func(), error) {
		return conn, func() {}, nil
	}); code == 0 || !strings.Contains(foreignErr.String(), "LEDGER_EVENT_NOT_FOUND") {
		t.Fatalf("cross-tenant target: exit %d, stderr %q; want a missing target refusal", code, foreignErr.String())
	}
	if n := rev028EventCount(t, conn, otherTenant); n != 0 {
		t.Fatalf("other tenant ledger rows after cross-tenant attempt = %d, want 0", n)
	}
}

func rev028EventCount(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID) int {
	t.Helper()
	var count int
	if err := migopTx(conn, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1`, tenant).Scan(&count)
	}); err != nil {
		t.Fatalf("count ledger events: %v", err)
	}
	return count
}
