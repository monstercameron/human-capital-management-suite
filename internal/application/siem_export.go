package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
)

var ErrSIEMExportInvalid = errors.New("application: invalid SIEM alert export")

const SIEMAlertSchemaVersion = 1

// SIEMAlertPayload is the stable JSON body for hcmnext.security.alert.v1.
// It contains a tenant-scoped alert decision and evidence digests only; the
// source signal payload, subject identifiers and signing key are not exported.
type SIEMAlertPayload struct {
	SchemaVersion  int                           `json:"schema_version"`
	Tenant         string                        `json:"tenant"`
	Sequence       uint64                        `json:"sequence"`
	Kind           subscription.EventKind        `json:"kind"`
	OccurredAt     time.Time                     `json:"occurred_at"`
	RuleID         string                        `json:"rule_id"`
	RuleVersion    int                           `json:"rule_version"`
	SignalKind     securityevidence.SequenceKind `json:"signal_kind"`
	Route          securityevidence.AlertRoute   `json:"route"`
	WindowStart    time.Time                     `json:"window_start"`
	WindowEnd      time.Time                     `json:"window_end"`
	ObservedCount  int                           `json:"observed_count"`
	EvidenceDigest string                        `json:"evidence_digest"`
	AlertDigest    string                        `json:"alert_digest"`
}

// SIEMAlertRecord binds the documented payload to the subscription delivery
// envelope and destination signature. The caller supplies the monotonically
// increasing feed sequence and signing time from its durable tenant feed.
type SIEMAlertRecord struct {
	Payload   SIEMAlertPayload
	Envelope  subscription.CanonicalEnvelope
	Signature subscription.SignedDelivery
}

// SIEMAlertEventSchema documents the exact fields in hcmnext.security.alert.v1.
func SIEMAlertEventSchema() (subscription.EventSchema, error) {
	return subscription.NewEventSchema(subscription.EventSecurityAlert, SIEMAlertSchemaVersion, []subscription.SchemaField{
		{Name: "alert_digest", Type: subscription.TypeString, Classification: subscription.ClassificationRestricted},
		{Name: "evidence_digest", Type: subscription.TypeString, Classification: subscription.ClassificationRestricted},
		{Name: "kind", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
		{Name: "occurred_at", Type: subscription.TypeInstant, Classification: subscription.ClassificationInternal},
		{Name: "observed_count", Type: subscription.TypeInteger, Classification: subscription.ClassificationInternal},
		{Name: "route", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
		{Name: "rule_id", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
		{Name: "rule_version", Type: subscription.TypeInteger, Classification: subscription.ClassificationInternal},
		{Name: "schema_version", Type: subscription.TypeInteger, Classification: subscription.ClassificationInternal},
		{Name: "sequence", Type: subscription.TypeInteger, Classification: subscription.ClassificationInternal},
		{Name: "signal_kind", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
		{Name: "tenant", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
		{Name: "window_end", Type: subscription.TypeInstant, Classification: subscription.ClassificationInternal},
		{Name: "window_start", Type: subscription.TypeInstant, Classification: subscription.ClassificationInternal},
	})
}

// SIEMAuditEventSchema documents the minimized DLP, access, and admin signal
// records served by the resumable feed as hcmnext.security.audit.v1.
func SIEMAuditEventSchema() (subscription.EventSchema, error) {
	return subscription.NewEventSchema(subscription.EventSecurityAudit, SIEMAlertSchemaVersion, []subscription.SchemaField{
		{Name: "evidence_digest", Type: subscription.TypeString, Classification: subscription.ClassificationRestricted},
		{Name: "event_type", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
		{Name: "occurred_at", Type: subscription.TypeInstant, Classification: subscription.ClassificationInternal},
		{Name: "schema_version", Type: subscription.TypeInteger, Classification: subscription.ClassificationInternal},
		{Name: "sequence", Type: subscription.TypeInteger, Classification: subscription.ClassificationInternal},
		{Name: "source_ref", Type: subscription.TypeString, Classification: subscription.ClassificationRestricted},
		{Name: "tenant", Type: subscription.TypeString, Classification: subscription.ClassificationInternal},
	})
}

// ExportTrustedSIEMAlert projects one detector-produced alert to the customer
// feed. expectedTenant must match the tenant bound by DetectOwnedAlerts;
// sequence is assigned by the durable feed owner, so this pure adapter does
// not invent a cursor or claim delivery completeness.
func ExportTrustedSIEMAlert(expectedTenant uuid.UUID, sequence uint64, trusted TrustedRoutedAlert, ring *subscription.CredentialRing, signedAt time.Time) (SIEMAlertRecord, error) {
	if expectedTenant == uuid.Nil || trusted.tenant == uuid.Nil || expectedTenant != trusted.tenant || sequence == 0 || ring == nil || signedAt.IsZero() {
		return SIEMAlertRecord{}, ErrSIEMExportInvalid
	}
	alert := trusted.alert
	if alert.Digest == "" || securityevidence.DigestOfAlert(alert) != alert.Digest ||
		alert.Revision == 0 || alert.RuleID == "" || alert.RuleVersion <= 0 || alert.ObservedCount <= 0 ||
		alert.EvidenceDigest == "" || alert.WindowStart.IsZero() || alert.WindowEnd.IsZero() || alert.WindowEnd.Before(alert.WindowStart) {
		return SIEMAlertRecord{}, ErrSIEMExportInvalid
	}
	payload := SIEMAlertPayload{
		SchemaVersion: SIEMAlertSchemaVersion, Tenant: expectedTenant.String(), Sequence: sequence,
		Kind: subscription.EventSecurityAlert, OccurredAt: alert.WindowEnd.UTC(),
		RuleID: alert.RuleID, RuleVersion: alert.RuleVersion, SignalKind: alert.Sequence,
		Route: alert.Route, WindowStart: alert.WindowStart.UTC(), WindowEnd: alert.WindowEnd.UTC(),
		ObservedCount: alert.ObservedCount, EvidenceDigest: alert.EvidenceDigest, AlertDigest: alert.Digest,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SIEMAlertRecord{}, fmt.Errorf("%w: encode payload", ErrSIEMExportInvalid)
	}
	bodyDigest := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(bodyDigest[:])
	envelope := subscription.CanonicalEnvelope{
		Tenant: expectedTenant.String(), Kind: subscription.EventSecurityAlert, SchemaVersion: SIEMAlertSchemaVersion,
		SubjectRefs: []string{"security-alert:" + alert.Digest}, EffectiveAt: alert.WindowEnd.UTC(), KnownAt: signedAt.UTC(),
		PayloadDigest: digest, ProvenanceRef: "securityevidence.alert/" + alert.Digest, Sequence: sequence,
	}
	if err := envelope.Validate(); err != nil {
		return SIEMAlertRecord{}, fmt.Errorf("%w: envelope: %v", ErrSIEMExportInvalid, err)
	}
	signature, err := ring.Sign(envelope.Digest(), signedAt)
	if err != nil {
		return SIEMAlertRecord{}, fmt.Errorf("%w: sign envelope: %v", ErrSIEMExportInvalid, err)
	}
	return SIEMAlertRecord{Payload: payload, Envelope: envelope, Signature: signature}, nil
}

// VerifySIEMAlertRecord checks the tenant, canonical payload digest, envelope
// binding and destination signature before a record is accepted by a sender.
func VerifySIEMAlertRecord(expectedTenant uuid.UUID, record SIEMAlertRecord, ring *subscription.CredentialRing, verifiedAt time.Time) error {
	if expectedTenant == uuid.Nil || ring == nil || verifiedAt.IsZero() ||
		record.Payload.Tenant != expectedTenant.String() || record.Payload.Kind != subscription.EventSecurityAlert ||
		record.Envelope.Tenant != expectedTenant.String() || record.Envelope.Kind != subscription.EventSecurityAlert ||
		record.Payload.Sequence == 0 || record.Payload.Sequence != record.Envelope.Sequence ||
		record.Payload.SchemaVersion != SIEMAlertSchemaVersion || record.Envelope.SchemaVersion != SIEMAlertSchemaVersion ||
		record.Envelope.EffectiveAt.UTC() != record.Payload.OccurredAt.UTC() ||
		record.Envelope.ProvenanceRef != "securityevidence.alert/"+record.Payload.AlertDigest ||
		record.Envelope.PayloadDigest != siemAlertPayloadDigest(record.Payload) ||
		record.Signature.MessageDigest != record.Envelope.Digest() {
		return ErrSIEMExportInvalid
	}
	return ring.Verify(record.Signature, record.Envelope.Digest(), verifiedAt)
}

func siemAlertPayloadDigest(payload SIEMAlertPayload) string {
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}
