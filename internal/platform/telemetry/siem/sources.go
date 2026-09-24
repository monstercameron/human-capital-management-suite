package siem

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
)

// EventInputFromSignal converts a validated SECARCH-008 signal to the
// customer-facing audit vocabulary. Tenant identity is supplied by the
// caller's trusted tenant binding; the signal itself contains no tenant id.
// The output contains a digest and a digest-derived source reference only.
func EventInputFromSignal(tenant uuid.UUID, signal securityevidence.Signal) (EventInput, error) {
	if tenant == uuid.Nil || signal.At.IsZero() || signal.Envelope.Tag != securityevidence.TagSecurity {
		return EventInput{}, ErrInvalidEvent
	}
	envelope, err := securityevidence.NewEnvelope(signal.Envelope.Base, signal.Envelope.Tag,
		signal.Envelope.RetentionClass, signal.Envelope.HoldState, signal.Envelope.HoldRef)
	if err != nil {
		return EventInput{}, fmt.Errorf("%w: invalid security envelope", ErrInvalidEvent)
	}
	if signal.Kind == securityevidence.SequenceJITNearTTLCeiling {
		_, err = securityevidence.NewSignalWithTTL(signal.Kind, signal.At, envelope, signal.TTLUsed, signal.TTLCeiling)
	} else {
		if signal.TTLUsed != 0 || signal.TTLCeiling != 0 {
			return EventInput{}, ErrInvalidEvent
		}
		_, err = securityevidence.NewSignal(signal.Kind, signal.At, envelope)
	}
	if err != nil {
		return EventInput{}, fmt.Errorf("%w: invalid security signal", ErrInvalidEvent)
	}
	kind, ok := auditKindForSignal(signal.Kind)
	if !ok {
		return EventInput{}, ErrInvalidEvent
	}
	digest := signal.Digest()
	return EventInput{Tenant: tenant.String(), Type: kind, OccurredAt: signal.At.UTC(),
		SourceRef: "security-signal:" + digest, EvidenceDigest: digest}, nil
}

func auditKindForSignal(kind securityevidence.SequenceKind) (EventType, bool) {
	switch kind {
	case securityevidence.SequenceRepeatedDLPRefusal:
		return EventDLP, true
	case securityevidence.SequenceCrossTenantDenialBurst, securityevidence.SequenceJITNearTTLCeiling:
		return EventAccess, true
	case securityevidence.SequenceBreakGlassUse:
		return EventAdmin, true
	default:
		return "", false
	}
}

// EventInputFromAlert projects an SECARCH-008 rule decision after its trusted
// application boundary has bound it to the tenant. The alert digest is
// re-computed here so forged or mutated detector results cannot be exported.
func EventInputFromAlert(tenant uuid.UUID, alert securityevidence.RoutedAlert) (EventInput, error) {
	if tenant == uuid.Nil || alert.Digest == "" || securityevidence.DigestOfAlert(alert) != alert.Digest ||
		strings.TrimSpace(alert.RuleID) == "" || alert.RuleID != strings.TrimSpace(alert.RuleID) ||
		alert.RuleVersion <= 0 || alert.ObservedCount <= 0 || alert.WindowEnd.IsZero() ||
		alert.WindowStart.IsZero() || alert.WindowEnd.Before(alert.WindowStart) || alert.EvidenceDigest == "" {
		return EventInput{}, ErrInvalidEvent
	}
	return EventInput{Tenant: tenant.String(), Type: EventAlertRule, OccurredAt: alert.WindowEnd.UTC(),
		SourceRef: "security-alert:" + alert.Digest, EvidenceDigest: alert.Digest,
		RuleID: alert.RuleID, RuleVersion: alert.RuleVersion}, nil
}
