package platformidempotencystore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	messageops "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func fixture(t *testing.T) (*pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, "platform-idem-"+tenant.String(), "Platform Idempotency")
	return db, tenant
}

func newAppStore(t *testing.T, db *pgtest.DB) *PostgresStore {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return New(conn)
}

func request(tenant uuid.UUID, now time.Time) idempotency.Request {
	return idempotency.Request{
		Identity: idempotency.Identity{Tenant: tenant.String(), Capability: "webhook.receive", EffectScope: "connection:payroll", Key: "event-42"},
		Layer:    "webhook", Canonical: []byte(`{"event":"42","payload":"same"}`), Now: now,
		Retention: idempotency.RetentionPolicy{ExpiresAt: now.Add(time.Hour), Mode: idempotency.RejectReuse, Tombstone: true},
	}
}

func TestTodo_REV_060_01(t *testing.T) {
	db, tenant := fixture(t)
	store := newAppStore(t, db)
	registry := idempotency.NewRegistryWithStore(store)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	req := request(tenant, now)
	first, err := registry.Reserve(req)
	if err != nil || first.Decision != idempotency.Reserved || first.Record.State != idempotency.InProgress {
		t.Fatalf("first reserve = %+v, %v", first, err)
	}
	completed, err := registry.Complete(req.Identity, first.Record.RequestDigest, "receipt-42", "effect-42", now.Add(time.Minute))
	if err != nil || completed.State != idempotency.Completed {
		t.Fatalf("complete = %+v, %v", completed, err)
	}
	replay, err := registry.Reserve(req)
	if err != nil || replay.Decision != idempotency.Replay || replay.Record.ResultRef != "receipt-42" || replay.Record.EffectRef != "effect-42" {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	if replay.Record.ReplayCount != 1 {
		t.Fatalf("replay count = %d, want 1", replay.Record.ReplayCount)
	}
}

func TestTodo_REV_060_01_Golden(t *testing.T) {
	db, tenant := fixture(t)
	registry := idempotency.NewRegistryWithStore(newAppStore(t, db))
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	first, err := registry.Reserve(request(tenant, now))
	if err != nil {
		t.Fatal(err)
	}
	if first.Record.RequestDigest != "691f825cd7c7fbbef21151b61b600eb732e992a091ae7509df0ce1f24306e469" {
		t.Fatalf("request digest = %q", first.Record.RequestDigest)
	}
	if first.Record.Identity.Capability != "webhook.receive" || first.Record.Identity.EffectScope != "connection:payroll" {
		t.Fatalf("golden identity = %+v", first.Record.Identity)
	}
}

func TestTodo_REV_060_01_Integration(t *testing.T) {
	db, tenant := fixture(t)
	otherTenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, otherTenant, "platform-idem-"+otherTenant.String(), "Other Tenant")
	registry := idempotency.NewRegistryWithStore(newAppStore(t, db))
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	base := request(tenant, now)
	for name, changed := range map[string]idempotency.Request{
		"different capability":   func() idempotency.Request { r := base; r.Identity.Capability = "communications.notify"; return r }(),
		"different effect scope": func() idempotency.Request { r := base; r.Identity.EffectScope = "intent:msg-42"; return r }(),
		"different tenant":       func() idempotency.Request { r := base; r.Identity.Tenant = otherTenant.String(); return r }(),
	} {
		got, err := registry.Reserve(changed)
		if err != nil || got.Decision != idempotency.Reserved {
			t.Errorf("%s reservation = %+v, %v; want isolated Reserved identity", name, got, err)
		}
	}
	if _, err := registry.Reserve(base); err != nil {
		t.Fatal(err)
	}
	foreignLookup := base.Identity
	foreignLookup.Tenant = otherTenant.String()
	if got, err := registry.Lookup(foreignLookup); err != nil || got.Identity.Tenant != otherTenant.String() {
		t.Fatalf("tenant-bound lookup = %+v, %v", got, err)
	}
	// With no selected tenant, hcmnext_app sees no rows even in a broad query.
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM platform_idempotency_record`).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("unscoped row visibility = (%d, %v), want zero", visible, err)
	}
}

type restartAttemptStore struct{ claims, observations int }

func (s *restartAttemptStore) Claim(_ context.Context, _ delivery.Envelope, n int) (delivery.Claim, error) {
	s.claims++
	return delivery.Claim{AttemptID: "attempt-1", Attempt: n}, nil
}
func (s *restartAttemptStore) Observe(_ context.Context, _ delivery.Envelope, _ delivery.Claim, _ delivery.Observation) error {
	s.observations++
	return nil
}

type restartTransport struct{ calls int }

func (s *restartTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	s.calls++
	return delivery.ProviderResult{ProviderRef: "provider-1"}, nil
}

func TestTodo_REV_060_01_IntegrationAdapters(t *testing.T) {
	db, tenant := fixture(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	endpoint := webhook.Endpoint{ID: "ep-1", TenantID: tenant.String(), ConnectionID: "conn-1", Secret: []byte("secret"), AllowedSchemas: []string{"promotion.v1"}, MaxPayloadBytes: 1024, ReplayWindow: time.Hour}
	request := webhook.Request{EndpointID: endpoint.ID, TenantID: endpoint.TenantID, EventID: "event-1", EventType: "promotion.updated", Schema: "promotion.v1", Timestamp: now, Payload: []byte(`{"worker":"w-1"}`)}
	request.Signature = webhook.Sign(endpoint.Secret, request)
	firstWebhook := webhook.NewStoreWithRegistry(idempotency.NewRegistryWithStore(newAppStore(t, db)))
	if err := firstWebhook.RegisterEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	firstReceipt, err := firstWebhook.Receive(request, now)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate both receiver and registry after the receipt's in-process maps are gone.
	restartedWebhook := webhook.NewStoreWithRegistry(idempotency.NewRegistryWithStore(newAppStore(t, db)))
	if err := restartedWebhook.RegisterEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	replayedReceipt, err := restartedWebhook.Receive(request, now.Add(30*time.Second))
	if err != nil || replayedReceipt.ID != firstReceipt.ID {
		t.Fatalf("webhook restart receipt = %+v, %v; first=%+v", replayedReceipt, err, firstReceipt)
	}

	store1, transport1 := &restartAttemptStore{}, &restartTransport{}
	envelope := delivery.Envelope{TenantID: tenant.String(), IntentID: uuid.NewString(), RecipientRef: "recipient-1", EndpointRef: uuid.NewString(), TemplateRef: "template-1", ParametersRef: "params-1", Purpose: "APPROVAL_REQUIRED", Classification: "INTERNAL", Channel: delivery.ChannelEmail, IdempotencyKey: "intent:endpoint", CorrelationID: "corr-1", ContentDigest: "sha256:abc", ExpiresAt: now.Add(time.Hour)}
	runner1 := delivery.Runner{Store: store1, Transport: transport1, Idempotency: idempotency.NewRegistryWithStore(newAppStore(t, db)), Clock: func() time.Time { return now }}
	if got, err := runner1.Deliver(context.Background(), envelope); err != nil || got.State != delivery.StateSubmitted {
		t.Fatalf("first worker dispatch = %+v, %v", got, err)
	}
	store2, transport2 := &restartAttemptStore{}, &restartTransport{}
	runner2 := delivery.Runner{Store: store2, Transport: transport2, Idempotency: idempotency.NewRegistryWithStore(newAppStore(t, db)), Clock: func() time.Time { return now.Add(time.Minute) }}
	if got, err := runner2.Deliver(context.Background(), envelope); err != nil || got.State != delivery.StateAlreadyObserved || got.ProviderRef != "provider-1" || transport2.calls != 0 || store2.claims != 1 {
		t.Fatalf("worker restart replay = %+v, %v; provider=%d claims=%d", got, err, transport2.calls, store2.claims)
	}
}

type restartMessageProvider struct{ calls int }

func (p *restartMessageProvider) Send(_ context.Context, _ messageops.Delivery) (messageops.ProviderResult, error) {
	p.calls++
	return messageops.ProviderResult{Reference: "provider-message-1", Accepted: true}, nil
}

func TestTodo_REV_060_01_IntegrationDispatcher(t *testing.T) {
	db, tenant := fixture(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	in := messageops.Intent{
		TenantID: tenant.String(), IntentID: uuid.NewString(), RecipientRef: "recipient-1", Purpose: "APPROVAL_REQUIRED",
		Subject: "Approval required", Body: "Open the secure task", Classification: "INTERNAL", IdempotencyKey: "message-1",
		CanonicalRequest: []byte(`{"purpose":"APPROVAL_REQUIRED","recipient":"recipient-1"}`), Committed: true, CreatedAt: now,
	}
	firstProvider := &restartMessageProvider{}
	first, err := messageops.NewDispatcherWithRegistry(firstProvider, messageops.Policy{}, idempotency.NewRegistryWithStore(newAppStore(t, db)))
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := first.Dispatch(context.Background(), in)
	if err != nil || firstResult.Attempt.State != messageops.AcceptedByProvider || firstProvider.calls != 1 {
		t.Fatalf("initial dispatcher result=%+v err=%v provider calls=%d", firstResult, err, firstProvider.calls)
	}

	// Reconstruct the dispatcher and registry after the old process-local
	// attempt map has disappeared. The durable lifecycle returns the outcome
	// reference and prevents the second provider call.
	secondProvider := &restartMessageProvider{}
	second, err := messageops.NewDispatcherWithRegistry(secondProvider, messageops.Policy{}, idempotency.NewRegistryWithStore(newAppStore(t, db)))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := second.Dispatch(context.Background(), in)
	if err != nil || recovered.Attempt.State != messageops.AcceptedByProvider || recovered.Attempt.ID != firstResult.Attempt.ID || recovered.Attempt.ProviderRef != firstResult.Attempt.ProviderRef {
		t.Fatalf("dispatcher restart result=%+v err=%v original=%+v", recovered, err, firstResult)
	}
	if firstProvider.calls != 1 || secondProvider.calls != 0 {
		t.Fatalf("provider calls across restart=(%d,%d), want (1,0)", firstProvider.calls, secondProvider.calls)
	}
}

func TestTodo_REV_060_01_Fault(t *testing.T) {
	db, tenant := fixture(t)
	registry := idempotency.NewRegistryWithStore(newAppStore(t, db))
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	req := request(tenant, now)
	req.Retention.ExpiresAt = now
	if _, err := registry.Reserve(req); !errors.Is(err, idempotency.ErrRetentionInvalid) {
		t.Fatalf("nonpositive retention = %v, want ErrRetentionInvalid", err)
	}
	if _, err := registry.Lookup(request(tenant, now).Identity); !errors.Is(err, idempotency.ErrNotFound) {
		t.Fatalf("invalid reserve persisted row: lookup error = %v", err)
	}
	valid := request(tenant, now)
	if _, err := registry.Reserve(valid); err != nil {
		t.Fatal(err)
	}
	changed := valid
	changed.Canonical = []byte(`{"event":"42","payload":"changed"}`)
	if _, err := registry.Reserve(changed); !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("changed canonical request = %v, want ErrConflict", err)
	}
}

func TestTodo_REV_060_01_Recovery(t *testing.T) {
	db, tenant := fixture(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	req := request(tenant, now)
	firstStore := newAppStore(t, db)
	firstProcess := idempotency.NewRegistryWithStore(firstStore)
	first, err := firstProcess.Reserve(req)
	if err != nil || first.Decision != idempotency.Reserved {
		t.Fatalf("initial reserve = %+v, %v", first, err)
	}
	// End the original process's registry and prove a separately launched test
	// process can only observe the persisted IN_PROGRESS row.
	firstProcess = nil
	firstStore = nil
	runRecoveryProcess(t, db, tenant, "in-flight")
	restartedProcess := idempotency.NewRegistryWithStore(newAppStore(t, db))
	if _, err := restartedProcess.Complete(req.Identity, first.Record.RequestDigest, "receipt-42", "effect-42", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	runRecoveryProcess(t, db, tenant, "replay")
	recovered := idempotency.NewRegistryWithStore(newAppStore(t, db))
	if count, err := recovered.ExpireTenant(tenant.String(), now.Add(2*time.Hour)); err != nil || count != 1 {
		t.Fatalf("expire tenant = (%d, %v), want one compacted row", count, err)
	}
	if _, err := recovered.Reserve(req); !errors.Is(err, idempotency.ErrExpired) {
		t.Fatalf("tombstone was reusable after restart: %v", err)
	}
}

func TestTodo_REV_060_01_RecoveryProcess(t *testing.T) {
	if os.Getenv("HCM_REV06001_PROCESS_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	conn, err := pgxadapter.Connect(ctx, os.Getenv("HCM_REV06001_DATABASE_URL"), map[string]string{"search_path": os.Getenv("HCM_REV06001_SCHEMA")})
	if err != nil {
		t.Fatalf("child connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("child assume app role: %v", err)
	}
	tenant, err := uuid.Parse(os.Getenv("HCM_REV06001_TENANT"))
	if err != nil {
		t.Fatal(err)
	}
	registry := idempotency.NewRegistryWithStore(New(conn))
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	got, err := registry.Reserve(request(tenant, now))
	if err != nil {
		t.Fatalf("child reserve: %v", err)
	}
	switch os.Getenv("HCM_REV06001_EXPECT") {
	case "in-flight":
		if got.Decision != idempotency.InFlight || got.Record.ExecutionRef == "" {
			t.Fatalf("child after process restart = %+v; want same in-flight execution", got)
		}
	case "replay":
		if got.Decision != idempotency.Replay || got.Record.ResultRef != "receipt-42" || got.Record.EffectRef != "effect-42" {
			t.Fatalf("child replay after process restart = %+v; want original completed result", got)
		}
	default:
		t.Fatalf("unknown helper expectation %q", os.Getenv("HCM_REV06001_EXPECT"))
	}
}

func runRecoveryProcess(t *testing.T, db *pgtest.DB, tenant uuid.UUID, expected string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTodo_REV_060_01_RecoveryProcess$")
	cmd.Env = append(os.Environ(),
		"HCM_REV06001_PROCESS_HELPER=1",
		"HCM_REV06001_DATABASE_URL="+db.URL,
		"HCM_REV06001_SCHEMA="+db.Schema,
		"HCM_REV06001_TENANT="+tenant.String(),
		"HCM_REV06001_EXPECT="+expected)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restart helper (%s) = %v, output %s", expected, err, string(output))
	}
}
