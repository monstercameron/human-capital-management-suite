package application

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

func siemFixture(t *testing.T) (uuid.UUID, time.Time, *subscription.CredentialRing) {
	t.Helper()
	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	provider, err := subscription.NewHMACProvider("siem-key", []byte("test-only-secret"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := subscription.NewCredentialRing("endpoint:customer-siem", func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	credential := subscription.SigningCredential{
		Destination: "endpoint:customer-siem", Profile: "hmac-test", Version: "v1", KeyRef: "siem-key",
		NotBefore: at.Add(-time.Hour), NotAfter: at.Add(time.Hour), Provider: provider,
	}
	if err := ring.Add(credential); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", at); err != nil {
		t.Fatal(err)
	}
	return tenant, at, ring
}

func TestTodo_REV_099_05(t *testing.T) {
	tenant, at, ring := siemFixture(t)
	record, err := ExportTrustedSIEMAlert(tenant, 4, testTrustedAlert(tenant), ring, at)
	if err != nil {
		t.Fatal(err)
	}
	if record.Payload.Tenant != tenant.String() || record.Payload.Sequence != 4 || record.Payload.Kind != subscription.EventSecurityAlert || record.Envelope.Tenant != tenant.String() {
		t.Fatalf("export lost tenant or sequence binding: %+v", record)
	}
	if record.Signature.MessageDigest != record.Envelope.Digest() {
		t.Fatalf("signature digest=%q envelope=%q", record.Signature.MessageDigest, record.Envelope.Digest())
	}
}

func TestTodo_REV_099_05_Golden(t *testing.T) {
	payload := SIEMAlertPayload{
		SchemaVersion: 1, Tenant: "tenant-1", Sequence: 2, Kind: subscription.EventSecurityAlert,
		OccurredAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), RuleID: "security.break-glass-use",
		RuleVersion: 1, SignalKind: "break_glass_use", Route: "incident_review",
		WindowStart: time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
		ObservedCount: 1, EvidenceDigest: "evidence-digest", AlertDigest: "alert-digest",
	}
	got, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"tenant":"tenant-1","sequence":2,"kind":"SECURITY_ALERT","occurred_at":"2026-09-24T12:00:00Z","rule_id":"security.break-glass-use","rule_version":1,"signal_kind":"break_glass_use","route":"incident_review","window_start":"2026-09-24T11:00:00Z","window_end":"2026-09-24T12:00:00Z","observed_count":1,"evidence_digest":"evidence-digest","alert_digest":"alert-digest"}`
	if string(got) != want {
		t.Fatalf("payload JSON changed\n got: %s\nwant: %s", got, want)
	}
}

func TestTodo_REV_099_05_Security(t *testing.T) {
	tenant, at, ring := siemFixture(t)
	if _, err := ExportTrustedSIEMAlert(uuid.New(), 1, testTrustedAlert(tenant), ring, at); !errors.Is(err, ErrSIEMExportInvalid) {
		t.Fatalf("cross-tenant export error=%v", err)
	}
	forged := testTrustedAlert(tenant)
	forged.alert.Digest = "forged"
	if _, err := ExportTrustedSIEMAlert(tenant, 1, forged, ring, at); !errors.Is(err, ErrSIEMExportInvalid) {
		t.Fatalf("tampered alert error=%v", err)
	}
	if _, err := ExportTrustedSIEMAlert(tenant, 1, testTrustedAlert(tenant), nil, at); !errors.Is(err, ErrSIEMExportInvalid) {
		t.Fatalf("missing signer error=%v", err)
	}
}

func TestTodo_REV_099_05_Integration(t *testing.T) {
	tenant, at, ring := siemFixture(t)
	record, err := ExportTrustedSIEMAlert(tenant, 9, testTrustedAlert(tenant), ring, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySIEMAlertRecord(tenant, record, ring, at); err != nil {
		t.Fatalf("export did not round-trip through subscription signing: %v", err)
	}
	schema, err := SIEMAlertEventSchema()
	if err != nil || schema.Verify() != nil || schema.Kind != subscription.EventSecurityAlert {
		t.Fatalf("schema=%+v error=%v", schema, err)
	}
	auditSchema, err := SIEMAuditEventSchema()
	if err != nil || auditSchema.Verify() != nil || auditSchema.Kind != subscription.EventSecurityAudit {
		t.Fatalf("audit schema=%+v error=%v", auditSchema, err)
	}
}

func TestTodo_REV_099_05_Mutation(t *testing.T) {
	tenant, at, ring := siemFixture(t)
	record, err := ExportTrustedSIEMAlert(tenant, 3, testTrustedAlert(tenant), ring, at)
	if err != nil {
		t.Fatal(err)
	}
	record.Payload.ObservedCount++
	if err := VerifySIEMAlertRecord(tenant, record, ring, at); !errors.Is(err, ErrSIEMExportInvalid) {
		t.Fatalf("mutated payload accepted: %v", err)
	}
}
