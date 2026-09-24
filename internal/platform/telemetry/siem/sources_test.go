package siem

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
)

func sourceSignal(t *testing.T, kind securityevidence.SequenceKind) securityevidence.Signal {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	ctx := telemetry.WithCorrelationID(context.Background(), "siem-source-correlation")
	base, err := telemetry.BuildEnvelope(ctx, telemetry.Resource{
		SchemaVersion: 1, ServiceName: "hcmnext", ServiceVersion: "test", ServiceInstanceID: "instance-a",
		Environment: "test", CellID: "cell-a", Region: "us-east", ProcessRole: telemetry.ProcessRoleAPI,
		BuildDigest: "build-a", TenantClass: telemetry.TenantClassStandard,
	}, telemetry.OutcomeDenied, 1, nil, allow)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := securityevidence.NewEnvelope(base, securityevidence.TagSecurity,
		securityevidence.RetentionSecurityDenial, securityevidence.HoldNone, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	if kind == securityevidence.SequenceJITNearTTLCeiling {
		signal, err := securityevidence.NewSignalWithTTL(kind, at, envelope, 50*time.Minute, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return signal
	}
	signal, err := securityevidence.NewSignal(kind, at, envelope)
	if err != nil {
		t.Fatal(err)
	}
	return signal
}

func TestEventInputFromSecuritySources(t *testing.T) {
	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	for _, tc := range []struct {
		kind securityevidence.SequenceKind
		want EventType
	}{
		{securityevidence.SequenceRepeatedDLPRefusal, EventDLP},
		{securityevidence.SequenceCrossTenantDenialBurst, EventAccess},
		{securityevidence.SequenceJITNearTTLCeiling, EventAccess},
		{securityevidence.SequenceBreakGlassUse, EventAdmin},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			input, err := EventInputFromSignal(tenant, sourceSignal(t, tc.kind))
			if err != nil {
				t.Fatal(err)
			}
			if err := input.validate(); err != nil {
				t.Fatalf("projected input invalid: %v", err)
			}
			if input.Tenant != tenant.String() || input.Type != tc.want || !strings.HasPrefix(input.SourceRef, "security-signal:") || input.EvidenceDigest == "" {
				t.Fatalf("projection = %+v, want tenant-bound %s digest reference", input, tc.want)
			}
		})
	}
}

func TestEventInputFromSourcesRejectsForgedAndCrossTenantEvidence(t *testing.T) {
	if _, err := EventInputFromSignal(uuid.Nil, sourceSignal(t, securityevidence.SequenceBreakGlassUse)); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("nil tenant error = %v", err)
	}
	forged := sourceSignal(t, securityevidence.SequenceRepeatedDLPRefusal)
	forged.TTLUsed = time.Minute
	if _, err := EventInputFromSignal(uuid.New(), forged); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("mutated source signal error = %v", err)
	}
	alert := securityevidence.RoutedAlert{
		Revision: 1, RuleID: "security.break-glass-use", RuleVersion: 1,
		Sequence: securityevidence.SequenceBreakGlassUse, AlertName: "Break-glass use",
		Route: securityevidence.RouteIncidentReview, WindowStart: testAt.Add(-time.Minute),
		WindowEnd: testAt, ObservedCount: 1, EvidenceDigest: digest("evidence"),
	}
	alert.Digest = securityevidence.DigestOfAlert(alert)
	input, err := EventInputFromAlert(uuid.New(), alert)
	if err != nil || input.Type != EventAlertRule || input.RuleID != alert.RuleID || input.RuleVersion != alert.RuleVersion {
		t.Fatalf("valid alert projection = %+v, %v", input, err)
	}
	alert.Digest = "forged"
	if _, err := EventInputFromAlert(uuid.New(), alert); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("forged alert digest error = %v", err)
	}
}
