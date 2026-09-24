package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	transportwebhook "github.com/monstercameron/human-capital-management-suite/internal/transport/webhook"
)

func TestTodo_INTG_018_ServedHTTPPostgres(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const payrollSecret = "test-payroll-webhook-secret-0123456789abcdef"
	const iamSecret = "test-iam-webhook-secret-0123456789abcdef"
	tenant := uuid.NewString()
	providerTenant := pgstore.TenantID(tenant).String()
	now := time.Now().UTC().Truncate(time.Second)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: tenant, CellID: "cell-intg-018-served",
		MaxDeadline: 30 * time.Second, Migrate: false, Workspace: true, OTelExporter: OTelExporterNone,
		LegalEvidenceIssuerKeys:  base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)),
		PayrollWebhookEndpointID: "payroll-endpoint", PayrollWebhookSecret: payrollSecret,
		IAMWebhookEndpointID: "iam-endpoint", IAMWebhookSecret: iamSecret,
	}

	// An unconfigured deployment leaves both provider paths absent.
	unconfigured := cfg
	unconfigured.PayrollWebhookEndpointID, unconfigured.PayrollWebhookSecret = "", ""
	unconfigured.IAMWebhookEndpointID, unconfigured.IAMWebhookSecret = "", ""
	plain, err := ComposeServe(context.Background(), ServeInput{Config: unconfigured, Pool: pool, Logger: &recordingLogger{}, Identity: "intg-018-unconfigured"})
	if err != nil {
		t.Fatalf("compose without providers: %v", err)
	}
	if component, ok := plain.Graph().Component(ComponentProviderWebhookReceivers); ok {
		t.Fatalf("unconfigured provider receiver was composed: %+v", component)
	}
	if err := plain.Start(context.Background()); err != nil {
		t.Fatalf("start unconfigured served app: %v", err)
	}
	for _, path := range []string{transportwebhook.PathForProvider(transportwebhook.PayrollProvider), transportwebhook.PathForProvider(transportwebhook.IAMProvider)} {
		resp := postWebhook(t, plain.HTTPAddr(), path, []byte(`{}`), nil)
		if resp != http.StatusNotFound {
			t.Fatalf("unconfigured %s status=%d, want 404", path, resp)
		}
	}
	stopComposed(t, plain)

	serve := func(identity string) *App {
		app, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Logger: &recordingLogger{}, Identity: identity, Options: Options{Now: func() time.Time { return now }}})
		if err != nil {
			t.Fatalf("compose configured serve: %v", err)
		}
		if err := app.Start(context.Background()); err != nil {
			t.Fatalf("start configured served app: %v", err)
		}
		return app
	}
	first := serve("intg-018-first")
	if component, ok := first.Graph().Component(ComponentProviderWebhookReceivers); !ok || component.Impl == "" {
		t.Fatalf("provider receiver composition missing: %+v, present=%v", component, ok)
	}
	payload := []byte(`{"event_id":"evt-payroll-1","change_ref":"payroll:change-1","correlation_key":"corr-1","provider_ref":"PSIM-1","outcome":"APPLIED","reason":"","effective_date":"2026-12-01","base_pay":{"amount":"160000.00","currency":"USD"},"occurred_at":"2026-09-24T12:00:00Z"}`)
	accepted := signedWebhook(payrollSecret, "evt-payroll-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, providerTenant, now, payload)
	if status := postWebhook(t, first.HTTPAddr(), transportwebhook.PathForProvider(transportwebhook.PayrollProvider), payload, accepted); status != http.StatusAccepted {
		t.Fatalf("signed payroll status=%d, want 202", status)
	}
	forged := signedWebhook("wrong-secret-0123456789abcdef", "evt-payroll-forged", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, tenant, now, payload)
	if status := postWebhook(t, first.HTTPAddr(), transportwebhook.PathForProvider(transportwebhook.PayrollProvider), payload, forged); status != http.StatusUnauthorized {
		t.Fatalf("forged payroll status=%d, want 401", status)
	}
	foreign := signedWebhook(payrollSecret, "evt-payroll-foreign", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, uuid.NewString(), now, payload)
	if status := postWebhook(t, first.HTTPAddr(), transportwebhook.PathForProvider(transportwebhook.PayrollProvider), payload, foreign); status != http.StatusUnauthorized {
		t.Fatalf("foreign tenant status=%d, want 401", status)
	}
	stopComposed(t, first)

	// A fresh serving composition sees durable receipt/idempotency state, acknowledges
	// the identical delivery, and can replay its exact signed bytes with approval.
	second := serve("intg-018-restarted")
	if status := postWebhook(t, second.HTTPAddr(), transportwebhook.PathForProvider(transportwebhook.PayrollProvider), payload, accepted); status != http.StatusAccepted {
		t.Fatalf("duplicate after restart status=%d, want 202", status)
	}
	composedReceivers, err := composeProviderWebhookReceivers(cfg, pool, func() time.Time { return now })
	if err != nil {
		t.Fatalf("compose replay receiver: %v", err)
	}
	receiver, ok := composedReceivers.payroll.(transportwebhook.Receiver)
	if !ok {
		t.Fatalf("payroll receiver type=%T", composedReceivers.payroll)
	}
	replayed, err := receiver.Replay(context.Background(), "evt-payroll-1", webhook.ReplayApproval{Approved: true, Actor: "operator-1", Purpose: "served integration verification"})
	if err != nil || !bytes.Equal(replayed.Payload, payload) {
		t.Fatalf("replay payload=%q err=%v", replayed.Payload, err)
	}
	if _, err := receiver.Replay(context.Background(), "evt-payroll-1", webhook.ReplayApproval{}); err == nil {
		t.Fatal("replay without approval succeeded")
	}

	iamPayload := []byte(`{"event_id":"evt-iam-1","change_ref":"iam:change-1","correlation_key":"corr-iam-1","provider_ref":"ISIM-1","outcome":"GRANTED","reason":"","job_code":"SAL-DIR","grade":"M4","effective_date":"2026-12-01","occurred_at":"2026-09-24T12:00:00Z"}`)
	iamHeaders := signedWebhook(iamSecret, "evt-iam-1", providerreceipt.IAMEventGranted, providerreceipt.IAMSchema, providerTenant, now, iamPayload)
	if status := postWebhook(t, second.HTTPAddr(), transportwebhook.PathForProvider(transportwebhook.IAMProvider), iamPayload, iamHeaders); status != http.StatusAccepted {
		t.Fatalf("signed IAM status=%d, want 202", status)
	}
	if got := countTenant(t, pool, tenant, `SELECT count(*) FROM integration_webhook_receipt`); got != 2 {
		t.Fatalf("receipt rows=%d, want payroll+IAM only", got)
	}
	if got := countTenant(t, pool, tenant, `SELECT count(*) FROM integration_webhook_outbox`); got != 2 {
		t.Fatalf("outbox rows=%d, want one per accepted event", got)
	}
	stopComposed(t, second)
}

func signedWebhook(secret string, id, event, schema, tenant string, at time.Time, payload []byte) http.Header {
	h := make(http.Header)
	h.Set("Webhook-Id", id)
	h.Set("Webhook-Event", event)
	h.Set("Webhook-Schema", schema)
	h.Set("Webhook-Timestamp", fmt.Sprint(at.UnixNano()))
	h.Set("Webhook-Tenant", tenant)
	h.Set("Webhook-Signature", webhook.Sign([]byte(secret), webhook.Request{EventID: id, EventType: event, Schema: schema, Timestamp: at, Payload: payload}))
	return h
}

func postWebhook(t *testing.T, addr, path string, body []byte, headers http.Header) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func stopComposed(t *testing.T, app *App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("stop composed app: %v", err)
	}
}

func countTenant(t *testing.T, pool *pgxadapter.Pool, tenant string, query string) int {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, pgstore.TenantID(tenant)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return count
}
