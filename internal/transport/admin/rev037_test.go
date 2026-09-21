package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

type rev037Fixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	digester *hashchain.Digester
	appender *hashchain.Appender
	head     int64
}

func newRev037Fixture(t *testing.T) *rev037Fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()

	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build chain-link registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)
	f := &rev037Fixture{db: db, tenant: tenant, digester: digester, appender: hashchain.NewAppender(digester)}

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant)
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.people.v1.CompensationBase', 1,
			'hcmnext.people.v1.CompensationBase', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, "hcmnext.people.v1.CompensationBase@1")
	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, 'authority:hr', 'INTERNAL', 'workforce', timestamptz '2020-01-01T00:00:00Z')`,
		tenant)

	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.EnsureStream(ctx, tx, tenant, "worker:1", "WORKER", "worker:1"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ensure stream: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit stream: %v", err)
	}

	at := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	eff := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	appender := ledger.New(ledger.WithClock(func() time.Time { return at }))
	for i, payload := range []string{"120000", "130000"} {
		var receipt ledger.AppendReceipt
		err := f.inTx(func(tx dbport.Tx) error {
			var appendErr error
			receipt, appendErr = appender.Append(ctx, tx, ledger.AppendRequest{
				Tenant:         tenant,
				StreamKey:      "worker:1",
				ExpectedHead:   f.head,
				AssertionClass: ledger.DomainFact,
				Authority:      "authority:hr",
				SourceRef:      "test:fixture",
				SchemaRef:      "hcmnext.people.v1.CompensationBase@1",
				Payload:        []byte(payload),
				OccurredAt:     at,
				EffectiveAt:    eff,
				CorrelationID:  uuid.New(),
				IdempotencyKey: uuid.NewString(),
			})
			if appendErr != nil {
				return appendErr
			}
			_, appendErr = f.appender.Append(ctx, tx, receipt)
			return appendErr
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		f.head = receipt.Sequence
	}
	return f
}

func (f *rev037Fixture) inTx(fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func (f *rev037Fixture) count(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	row := f.db.QueryRow(context.Background(), "SELECT count(*) FROM "+table)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func rev037Deps(f *rev037Fixture) admin.Dependencies {
	return admin.Dependencies{
		ListLedgerStream: func(ctx context.Context, tenant uuid.UUID, stream string) (explorer.StreamListingView, error) {
			return explorer.StreamListing(ctx, f.db.Conn, tenant, stream, nil)
		},
		VerifyLedgerChain: func(ctx context.Context, tenant uuid.UUID, stream string) (explorer.ChainView, error) {
			return explorer.VerifyChain(ctx, f.db.Conn, f.digester, tenant, stream)
		},
	}
}
func TestTodo_REV_037_01(t *testing.T) {
	f := newRev037Fixture(t)
	conn, cleanup := startTestServer(t, rev037Deps(f))
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)
	tenantID := f.tenant.String()

	list, err := client.ListLedgerEvents(opCtx, &adminv1.ListLedgerEventsRequest{TenantId: tenantID, StreamKey: "worker:1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.GetEvents()) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(list.GetEvents()))
	}
	if list.GetEvents()[0].GetSequence() != 1 || list.GetEvents()[1].GetSequence() != 2 {
		t.Fatalf("events not in sequence order: %d, %d", list.GetEvents()[0].GetSequence(), list.GetEvents()[1].GetSequence())
	}
	if string(list.GetEvents()[1].GetPayload()) != "130000" {
		t.Fatalf("events[1].payload = %s, want 130000", list.GetEvents()[1].GetPayload())
	}
	if list.GetDigest() == "" {
		t.Fatal("listing digest is empty")
	}
	if list.GetEvents()[0].GetPayloadWithheld() || list.GetEvents()[0].GetSubjectWithheld() {
		t.Fatal("operator listing withholds payload the library discloses")
	}

	direct, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, "worker:1", nil)
	if err != nil {
		t.Fatalf("direct listing: %v", err)
	}
	if direct.Digest != list.GetDigest() {
		t.Fatalf("rpc digest %q != library digest %q", list.GetDigest(), direct.Digest)
	}

	chain, err := client.GetChainVerification(opCtx, &adminv1.GetChainVerificationRequest{TenantId: tenantID, StreamKey: "worker:1"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !chain.GetVerified() || chain.GetEmpty() {
		t.Fatalf("chain not verified: %+v", chain)
	}
	if chain.GetHead().GetSequence() != 2 {
		t.Fatalf("head sequence = %d, want 2", chain.GetHead().GetSequence())
	}
	if chain.GetHead().GetChainHash() == "" {
		t.Fatal("chain head hash is empty")
	}

	sim, err := client.SimulateAuthorization(opCtx, &adminv1.SimulateAuthorizationRequest{
		SubjectTenantId: tenantID,
		SubjectKind:     "worker",
		SubjectId:       "worker:1",
		Purpose:         "operator_diagnostics",
		Fields:          []string{"compensation.base"},
		PolicyVersion:   "p1a-bootstrap",
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if sim.GetDigest() == "" || sim.GetExplanation() == "" {
		t.Fatalf("simulation missing digest/explanation: %+v", sim)
	}
	for _, effect := range append([]string{sim.GetTenantEffect(), sim.GetScopeEffect()}, sim.GetFieldRulings()["compensation.base"]) {
		switch effect {
		case "ALLOW", "DENIED", "REDACTED", "WITHHELD", "EFFECT_UNSPECIFIED":
		default:
			t.Fatalf("unknown effect %q", effect)
		}
	}
	sim2, err := client.SimulateAuthorization(opCtx, &adminv1.SimulateAuthorizationRequest{
		SubjectTenantId: tenantID,
		SubjectKind:     "worker",
		SubjectId:       "worker:1",
		Purpose:         "operator_diagnostics",
		Fields:          []string{"compensation.base"},
		PolicyVersion:   "other-label",
	})
	if err != nil {
		t.Fatalf("simulate again: %v", err)
	}
	if sim2.GetPolicyVersionMatch() {
		t.Fatal("simulation against an unknown policy label reports a match")
	}
}

func TestTodo_REV_037_01_Integration(t *testing.T) {
	f := newRev037Fixture(t)
	conn, cleanup := startTestServer(t, rev037Deps(f))
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)
	tenantID := f.tenant.String()

	eventsBefore := f.count(t, "ledger_event")
	linksBefore := f.count(t, "ledger_hash_chain_link")

	list, err := client.ListLedgerEvents(opCtx, &adminv1.ListLedgerEventsRequest{TenantId: tenantID, StreamKey: "worker:1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := client.GetChainVerification(opCtx, &adminv1.GetChainVerificationRequest{TenantId: tenantID, StreamKey: "worker:1"}); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := client.SimulateAuthorization(opCtx, &adminv1.SimulateAuthorizationRequest{
		SubjectTenantId: tenantID, SubjectKind: "worker", SubjectId: "worker:1",
	}); err != nil {
		t.Fatalf("simulate: %v", err)
	}

	if got := f.count(t, "ledger_event"); got != eventsBefore {
		t.Fatalf("ledger_event rows %d -> %d: reads wrote", eventsBefore, got)
	}
	if got := f.count(t, "ledger_hash_chain_link"); got != linksBefore {
		t.Fatalf("ledger_hash_chain_link rows %d -> %d: reads wrote", linksBefore, got)
	}

	again, err := client.ListLedgerEvents(opCtx, &adminv1.ListLedgerEventsRequest{TenantId: tenantID, StreamKey: "worker:1"})
	if err != nil {
		t.Fatalf("list again: %v", err)
	}
	if again.GetDigest() != list.GetDigest() {
		t.Fatal("repeated listing digest changed without any write")
	}
}

func TestTodo_REV_037_01_Security(t *testing.T) {
	f := newRev037Fixture(t)
	conn, cleanup := startTestServer(t, rev037Deps(f))
	defer cleanup()
	client := dialAdminClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ordinary := withToken(ctx, fixtureOrdinaryToken)
	tenantID := f.tenant.String()

	_, err := client.ListLedgerEvents(ordinary, &adminv1.ListLedgerEventsRequest{TenantId: tenantID, StreamKey: "worker:1"})
	assertOwnedCode(t, err, envelope.CodePermissionDenied)
	_, err = client.GetChainVerification(ordinary, &adminv1.GetChainVerificationRequest{TenantId: tenantID, StreamKey: "worker:1"})
	assertOwnedCode(t, err, envelope.CodePermissionDenied)
	_, err = client.SimulateAuthorization(ordinary, &adminv1.SimulateAuthorizationRequest{
		SubjectTenantId: tenantID, SubjectKind: "worker", SubjectId: "worker:1",
	})
	assertOwnedCode(t, err, envelope.CodePermissionDenied)

	opCtx := withToken(ctx, fixtureOperatorToken)
	_, err = client.ListLedgerEvents(opCtx, &adminv1.ListLedgerEventsRequest{TenantId: "not-a-uuid", StreamKey: "worker:1"})
	assertOwnedCode(t, err, envelope.CodeInvalidArgument)
}
