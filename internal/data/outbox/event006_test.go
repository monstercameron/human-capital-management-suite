package outbox

import (
	"errors"
	"testing"
	"time"
)

// event006TestKey signs promotion dossiers in tests. It stands in for
// the offline promotion authority key: RED demands that only a dossier
// sealed with it can promote.
const event006TestKey = "event-006-test-authority-key"

// event006ValidDossier returns a fully evidenced, signed dossier whose
// measured load breaches the Postgres outbox envelope on every axis,
// so the only lawful outcome is promotion.
func event006ValidDossier() PromotionDossier {
	dossier := PromotionDossier{
		BrokerKind:          BrokerRedpandaLite,
		Owner:               "platform-data",
		EstimatedMonthlyUSD: 120,
		MonthlyCapUSD:       500,
		ExitPlan:            "drain broker, replay from postgres outbox checkpoint log",
		Envelope:            OutboxEnvelope{MaxSustainedEPS: 100, MaxBacklog: 1000, MaxLag: time.Minute},
		Load:                MeasuredLoad{SustainedEPS: 250, Backlog: 4000, P99Lag: 5 * time.Minute},
		Parity: ParityEvidence{
			ConformanceDigest:  "sha256:conformance-oss-redpanda-lite-v1",
			RestoreDrillPassed: true,
			OperabilityPassed:  true,
		},
	}
	SignDossier(&dossier, event006TestKey)
	return dossier
}

// event006Resign re-seals a mutated dossier so a fault case isolates
// exactly one failure instead of tripping the signature check as well.
func event006Resign(dossier PromotionDossier) PromotionDossier {
	dossier.Signature = ""
	SignDossier(&dossier, event006TestKey)
	return dossier
}

// TestTodo_EVENT_006 is the PRIMARY gate: promotion happens only on a
// signed dossier that proves a breached outbox envelope plus
// conformance/restore/operability parity and ownership/cost/exit
// evidence. Otherwise Postgres remains the authoritative transport.
func TestTodo_EVENT_006(t *testing.T) {
	t.Run("breached-envelope-promotes", func(t *testing.T) {
		decision, err := EvaluatePromotion(event006ValidDossier(), event006TestKey)
		if err != nil {
			t.Fatalf("promote: %v", err)
		}
		if !decision.Promote {
			t.Fatal("breached envelope with full evidence did not promote")
		}
		if decision.Transport != TransportBroker {
			t.Fatalf("transport = %v, want broker", decision.Transport)
		}
		if decision.BrokerKind != BrokerRedpandaLite {
			t.Fatalf("broker = %q, want %q", decision.BrokerKind, BrokerRedpandaLite)
		}
		if len(decision.BreachReasons) != 3 {
			t.Fatalf("breach reasons = %v, want one per envelope axis", decision.BreachReasons)
		}
	})

	t.Run("intact-envelope-stays-on-postgres", func(t *testing.T) {
		dossier := event006ValidDossier()
		dossier.Load = MeasuredLoad{SustainedEPS: 40, Backlog: 100, P99Lag: 10 * time.Second}
		dossier = event006Resign(dossier)
		decision, err := EvaluatePromotion(dossier, event006TestKey)
		if err != nil {
			t.Fatalf("stay: %v", err)
		}
		if decision.Promote {
			t.Fatal("intact envelope promoted without measured need")
		}
		if decision.Transport != TransportPostgres {
			t.Fatalf("transport = %v, want postgres", decision.Transport)
		}
		if len(decision.BreachReasons) != 0 {
			t.Fatalf("breach reasons = %v, want none", decision.BreachReasons)
		}
	})

	t.Run("single-axis-breach-promotes", func(t *testing.T) {
		dossier := event006ValidDossier()
		dossier.Load = MeasuredLoad{SustainedEPS: 40, Backlog: 5000, P99Lag: 10 * time.Second}
		dossier = event006Resign(dossier)
		decision, err := EvaluatePromotion(dossier, event006TestKey)
		if err != nil {
			t.Fatalf("single breach: %v", err)
		}
		if !decision.Promote || decision.Transport != TransportBroker {
			t.Fatalf("backlog breach did not promote: %+v", decision)
		}
		if len(decision.BreachReasons) != 1 {
			t.Fatalf("breach reasons = %v, want exactly the backlog axis", decision.BreachReasons)
		}
	})

	t.Run("zero-dossier-fails-closed-on-postgres", func(t *testing.T) {
		decision, err := EvaluatePromotion(PromotionDossier{}, event006TestKey)
		if err == nil {
			t.Fatal("zero dossier admitted")
		}
		if decision.Transport != TransportPostgres || decision.Promote {
			t.Fatalf("failed-closed decision left postgres: %+v", decision)
		}
		if got := Transport(99).String(); got == "" {
			t.Fatal("unknown transport has no diagnostic string")
		}
	})
}

// TestTodo_EVENT_006_Fault pins the fail-closed edges: every incomplete,
// unsigned, tampered or malformed dossier is refused with its documented
// sentinel, and the returned decision never leaves Postgres.
func TestTodo_EVENT_006_Fault(t *testing.T) {
	valid := event006ValidDossier()

	cases := []struct {
		name string
		drop func(*PromotionDossier)
		want error
	}{
		{"empty-broker-kind", func(d *PromotionDossier) { d.BrokerKind = "" }, ErrPromotionInvalid},
		{"padded-broker-kind", func(d *PromotionDossier) { d.BrokerKind = " " + BrokerRedpandaLite + " " }, ErrPromotionInvalid},
		{"unknown-broker-kind", func(d *PromotionDossier) { d.BrokerKind = "kafka-enterprise-cloud" }, ErrPromotionInvalid},
		{"negative-envelope", func(d *PromotionDossier) { d.Envelope.MaxBacklog = -1 }, ErrPromotionInvalid},
		{"negative-load", func(d *PromotionDossier) { d.Load.Backlog = -5 }, ErrPromotionInvalid},
		{"missing-owner", func(d *PromotionDossier) { d.Owner = "  " }, ErrPromotionEvidence},
		{"missing-exit-plan", func(d *PromotionDossier) { d.ExitPlan = "" }, ErrPromotionEvidence},
		{"cost-over-cap", func(d *PromotionDossier) { d.EstimatedMonthlyUSD = 9000 }, ErrPromotionEvidence},
		{"missing-cap", func(d *PromotionDossier) { d.MonthlyCapUSD = 0 }, ErrPromotionEvidence},
		{"missing-conformance", func(d *PromotionDossier) { d.Parity.ConformanceDigest = "" }, ErrPromotionEvidence},
		{"restore-drill-missing", func(d *PromotionDossier) { d.Parity.RestoreDrillPassed = false }, ErrPromotionEvidence},
		{"operability-missing", func(d *PromotionDossier) { d.Parity.OperabilityPassed = false }, ErrPromotionEvidence},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dossier := valid
			tc.drop(&dossier)
			dossier = event006Resign(dossier)
			decision, err := EvaluatePromotion(dossier, event006TestKey)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if decision.Promote || decision.Transport != TransportPostgres {
				t.Fatalf("refused dossier left postgres: %+v", decision)
			}
		})
	}

	t.Run("unsigned", func(t *testing.T) {
		dossier := valid
		dossier.Signature = ""
		if _, err := EvaluatePromotion(dossier, event006TestKey); !errors.Is(err, ErrPromotionUnsigned) {
			t.Fatalf("unsigned err = %v, want %v", err, ErrPromotionUnsigned)
		}
	})

	t.Run("tampered-after-signing", func(t *testing.T) {
		dossier := valid
		dossier.Load.Backlog++
		if _, err := EvaluatePromotion(dossier, event006TestKey); !errors.Is(err, ErrPromotionUnsigned) {
			t.Fatalf("tampered err = %v, want %v", err, ErrPromotionUnsigned)
		}
	})

	t.Run("wrong-authority-key", func(t *testing.T) {
		if _, err := EvaluatePromotion(valid, "impostor-key"); !errors.Is(err, ErrPromotionUnsigned) {
			t.Fatalf("wrong key err = %v, want %v", err, ErrPromotionUnsigned)
		}
	})
}

// FuzzTodo_EVENT_006 proves the breach predicate never panics and never
// disagrees with its oracle: breached exactly when any measured axis
// exceeds the envelope.
func FuzzTodo_EVENT_006(f *testing.F) {
	f.Add(100.0, 50.0, int64(1000), int64(10), int64(60_000_000_000), int64(1_000_000_000))
	f.Add(100.0, 250.0, int64(1000), int64(4000), int64(60_000_000_000), int64(300_000_000_000))
	f.Add(0.0, 0.0, int64(0), int64(0), int64(0), int64(0))
	f.Fuzz(func(t *testing.T, maxEPS, eps float64, maxBacklog, backlog, maxLagNs, lagNs int64) {
		envelope := OutboxEnvelope{MaxSustainedEPS: maxEPS, MaxBacklog: maxBacklog, MaxLag: time.Duration(maxLagNs)}
		load := MeasuredLoad{SustainedEPS: eps, Backlog: backlog, P99Lag: time.Duration(lagNs)}
		breached, reasons := EnvelopeBreached(envelope, load)
		want := eps > maxEPS || backlog > maxBacklog || lagNs > maxLagNs
		if breached != want {
			t.Fatalf("breached=%v want=%v for envelope=%+v load=%+v", breached, want, envelope, load)
		}
		if breached != (len(reasons) > 0) {
			t.Fatalf("breached=%v but reasons=%v", breached, reasons)
		}
	})
}
