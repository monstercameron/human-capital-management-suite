package admin_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func startEvidenceExportServer(t *testing.T, export admin.LedgerEvidenceExport) (*grpc.ClientConn, func()) {
	t.Helper()
	cfg := transport.Config{Verifier: fakeVerifier{}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	admin.Register(srv, admin.Dependencies{})
	admin.RegisterLedgerEvidence(srv, export)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("dial: %v", err)
	}
	return conn, func() { _ = conn.Close(); srv.Stop(); _ = lis.Close() }
}

func evidenceExportRequest(from, to time.Time, tenant string) *adminv1.ExportLedgerEvidenceRequest {
	return &adminv1.ExportLedgerEvidenceRequest{
		Scope: &commonv1.ScopeContext{TenantId: tenant},
		From:  timestamppb.New(from), To: timestamppb.New(to),
	}
}

func TestTodo_REV_028_02(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	called := false
	conn, cleanup := startEvidenceExportServer(t, func(_ context.Context, tenant string, gotFrom, gotTo time.Time) (map[string][]byte, error) {
		called = true
		if tenant != fixtureTenant || !gotFrom.Equal(from) || !gotTo.Equal(to) {
			t.Fatalf("export scope = %q [%s,%s), want %q [%s,%s)", tenant, gotFrom, gotTo, fixtureTenant, from, to)
		}
		return map[string][]byte{"manifest.json": []byte("signed package bytes")}, nil
	})
	defer cleanup()
	response, err := adminv1.NewLedgerEvidenceServiceClient(conn).ExportLedgerEvidence(withToken(context.Background(), fixtureOperatorToken), evidenceExportRequest(from, to, fixtureTenant))
	if err != nil {
		t.Fatalf("ExportLedgerEvidence: %v", err)
	}
	if !called {
		t.Fatal("the configured exporter port was not called")
	}
	if len(response.GetFiles()) != 1 || response.GetFiles()[0].GetPath() != "manifest.json" || string(response.GetFiles()[0].GetContent()) != "signed package bytes" {
		t.Fatalf("returned files = %+v, want the complete package path and bytes", response.GetFiles())
	}
	if response.GetEvidenceRef().GetEvidenceId() == "" || response.GetEvidenceRef().GetEvidenceKind() != "admin.export_ledger_evidence" {
		t.Fatalf("evidence_ref = %+v, want the authenticated export reference", response.GetEvidenceRef())
	}
}

func TestTodo_REV_028_02_Integration(t *testing.T) {
	db, dir, from, to := seedTransportLedgerEvidence(t)
	conn, cleanup := startEvidenceExportServer(t, application.NewLedgerEvidenceExport(db.Conn))
	defer cleanup()
	response, err := adminv1.NewLedgerEvidenceServiceClient(conn).ExportLedgerEvidence(withToken(context.Background(), fixtureOperatorToken), evidenceExportRequest(from, to, fixtureTenant))
	if err != nil {
		t.Fatalf("ExportLedgerEvidence round-trip: %v", err)
	}
	received := make(map[string][]byte, len(response.GetFiles()))
	for _, file := range response.GetFiles() {
		if _, exists := received[file.GetPath()]; exists {
			t.Fatalf("duplicate package path %q", file.GetPath())
		}
		received[file.GetPath()] = file.GetContent()
	}
	if len(received) == 0 {
		t.Fatal("transport returned no evidence files")
	}
	report := ledgerport.VerifyEvidence(received, dir)
	if err := report.Err(); err != nil {
		t.Fatalf("offline VerifyEvidence rejected transported output: %v", err)
	}
	if report.EventsVerified != 1 {
		t.Fatalf("offline verifier checked %d events, want the seeded ledger event", report.EventsVerified)
	}
}

func TestTodo_REV_028_02_Security(t *testing.T) {
	calls := 0
	conn, cleanup := startEvidenceExportServer(t, func(context.Context, string, time.Time, time.Time) (map[string][]byte, error) {
		calls++
		return map[string][]byte{"manifest.json": []byte("secret")}, nil
	})
	defer cleanup()
	client := adminv1.NewLedgerEvidenceServiceClient(conn)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	t.Run("another tenant is refused before export", func(t *testing.T) {
		_, err := client.ExportLedgerEvidence(withToken(context.Background(), fixtureOperatorToken), evidenceExportRequest(from, to, "another-tenant"))
		if err == nil || !strings.Contains(err.Error(), "InvalidArgument") {
			t.Fatalf("cross-tenant call error = %v, want rejection by trusted-scope validation", err)
		}
		if calls != 0 {
			t.Fatalf("exporter called %d times after denied tenant request", calls)
		}
	})
	t.Run("non-operator is refused before export", func(t *testing.T) {
		_, err := client.ExportLedgerEvidence(withToken(context.Background(), fixtureOrdinaryToken), evidenceExportRequest(from, to, fixtureTenant))
		if err == nil || !strings.Contains(err.Error(), "PermissionDenied") {
			t.Fatalf("non-operator call error = %v, want PermissionDenied", err)
		}
		if calls != 0 {
			t.Fatalf("exporter called %d times after denied role request", calls)
		}
	})
}

func seedTransportLedgerEvidence(t *testing.T) (*pgtest.DB, checkpoint.KeyDirectory, time.Time, time.Time) {
	t.Helper()
	db := pgtest.New(t)
	tenant := pgstore.TenantID(fixtureTenant)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	releaseVersion, err := migrations.TargetVersion()
	if err != nil {
		t.Fatalf("target migration version: %v", err)
	}
	releaseDigest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("migration artifact digest: %v", err)
	}
	releaseID := uuid.New()
	releaseName := fmt.Sprintf("p1a-%05d-%s", releaseVersion, releaseDigest[:12])
	db.Exec(t, `
		INSERT INTO schema_release (
			release_id, release_version, artifact_digest, digest_algorithm,
			source_digest, tool_version, compatibility_class, owner,
			reversible, trusted_time_source)
		VALUES ($1, $2, $3, 'sha256', $3, 'rev028-02-test',
			'BACKWARD_COMPATIBLE', 'test', true, 'TEST_CLOCK')`,
		releaseID, releaseName, releaseDigest)
	db.Exec(t, `
		INSERT INTO migration_journal (
			journal_id, release_id, migration_version, migration_name, direction,
			checksum, checksum_algorithm, tool_version, applied_by, status,
			started_at, finished_at, trusted_time_source)
		VALUES ($1, $2, $3, 'latest_migration', 'UP', $4, 'sha256',
			'rev028-02-test', 'test', 'APPLIED', $5, $6, 'TEST_CLOCK')`,
		uuid.New(), releaseID, releaseVersion, releaseDigest, from.Add(-time.Hour), from.Add(-time.Minute))
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-test', 'Evidence Export Test', 'ACTIVE', $3)`, tenant, fixtureTenant, from.Add(-time.Hour))
	const schemaRef = "hcmnext.test.v1.Record@1"
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.test.v1.Record', 1,
			'hcmnext.test.v1.Record', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, schemaRef)
	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, 'authority:rev028-02', 'INTERNAL', 'workforce', $2)`, tenant, from.Add(-time.Hour))
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("chain registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)
	streamKey := "worker:rev028-02"
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin ledger seed: %v", err)
	}
	if err := ledger.EnsureStream(ctx, tx, tenant, streamKey, "WORKER", "worker:test"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure stream: %v", err)
	}
	recordAt := from
	appender := ledger.New(ledger.WithClock(func() time.Time { return recordAt }))
	receipt, err := appender.Append(ctx, tx, ledger.AppendRequest{
		Tenant: tenant, StreamKey: streamKey, ExpectedHead: 0,
		AssertionClass: ledger.TransactionFact, Authority: "authority:rev028-02",
		SourceRef: "test:rev028-02", SchemaRef: schemaRef, Payload: []byte("auditable recorded fact"),
		OccurredAt: from, EffectiveAt: from, CorrelationID: uuid.New(), IdempotencyKey: "rev028-02-event-1",
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("append ledger event: %v", err)
	}
	if _, err := hashchain.NewAppender(digester).Append(ctx, tx, receipt); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("link ledger event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ledger seed: %v", err)
	}
	seed := [ed25519.SeedSize]byte{0x72, 0x65, 0x76, 0x30, 0x32, 0x38, 0x2d, 0x30, 0x32, 0x2d, 0x74, 0x65, 0x73, 0x74}
	signer, err := checkpoint.NewEd25519Signer("rev028-02-fixture", ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	keyDir := checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: signer.KeyID(), PublicKey: signer.PublicKey(), NotBefore: from.Add(-time.Hour),
	})
	service, err := checkpoint.NewService(signer, keyDir)
	if err != nil {
		t.Fatalf("new checkpoint service: %v", err)
	}
	tx, err = db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin checkpoint seed: %v", err)
	}
	if _, err := service.Create(ctx, tx, checkpoint.CreateRequest{
		Tenant: tenant, Schema: checkpoint.SchemaRelease{Version: releaseVersion, Digest: releaseDigest}, At: to,
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("create signed checkpoint: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit checkpoint: %v", err)
	}
	return db, keyDir, from, to
}
