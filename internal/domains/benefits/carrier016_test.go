package benefits

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/roundtrip"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// TestTodo_WF_CAP_016 proves continuation coverage, insurability evidence
// and outbound enrollment files are DELEGATED contracts over connector
// bindings, with kernel-pure domain rules underneath.
func TestTodo_WF_CAP_016(t *testing.T) {
	t.Run("GREEN_bindings_are_valid_delegated_contracts", func(t *testing.T) {
		bindings := []roundtrip.Binding{ContinuationBinding(), InsurabilityBinding(), EnrollmentBinding()}
		registry := roundtrip.NewRegistry()
		for _, binding := range bindings {
			if err := binding.Validate(); err != nil {
				t.Fatalf("%s: %v", binding.ID, err)
			}
			if err := registry.Register(binding); err != nil {
				t.Fatalf("register %s: %v", binding.ID, err)
			}
		}
		for _, tc := range []struct {
			binding      roundtrip.Binding
			capabilityID string
		}{
			{bindings[0], ContinuationDispatchCapability},
			{bindings[1], InsurabilityDispatchCapability},
			{bindings[2], EnrollmentDispatchCapability},
		} {
			if err := roundtrip.ResolveDelegated(tc.binding, tc.capabilityID, tc.binding.Version); err != nil {
				t.Fatalf("ResolveDelegated(%s): %v", tc.capabilityID, err)
			}
			if err := roundtrip.ResolveDelegated(tc.binding, tc.capabilityID, tc.binding.Version+99); !errors.Is(err, roundtrip.ErrBindingMismatch) {
				t.Fatalf("stale version resolves: %v", err)
			}
			if err := roundtrip.ResolveDelegated(tc.binding, "hcmnext.unknown", tc.binding.Version); !errors.Is(err, roundtrip.ErrBindingMismatch) {
				t.Fatalf("foreign capability resolves through %s", tc.binding.ID)
			}
		}
	})

	t.Run("GREEN_continuation_offer_table", func(t *testing.T) {
		cases := []struct {
			event ContinuationEvent
			want  int
		}{
			{ContinuationEventTermination, 18},
			{ContinuationEventHoursReduction, 18},
			{ContinuationEventDivorce, 36},
			{ContinuationEventDeath, 36},
			{ContinuationEventMedicare, 36},
			{ContinuationEventLossOfDependentStatus, 36},
		}
		for _, tc := range cases {
			offer, err := ContinuationOfferFor(ContinuationRequest{
				Event: tc.event, EventDate: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
				NotifiedOn: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
				PlanRef:    "plan-ppo-2026", MemberRef: "worker-1",
			})
			if err != nil {
				t.Fatalf("%s: %v", tc.event, err)
			}
			if offer.Months != tc.want {
				t.Fatalf("%s months = %d, want %d", tc.event, offer.Months, tc.want)
			}
			if want := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC); !offer.ElectBy.Equal(want) {
				t.Fatalf("%s elect-by = %v, want %v", tc.event, offer.ElectBy, want)
			}
			if !offer.CoverageFrom.Equal(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("%s coverage-from = %v, want the event date", tc.event, offer.CoverageFrom)
			}
			if want := offer.CoverageFrom.AddDate(0, tc.want, 0); !offer.CoverageThrough.Equal(want) {
				t.Fatalf("%s coverage-through = %v, want %v", tc.event, offer.CoverageThrough, want)
			}
		}
	})

	t.Run("RED_continuation_refuses", func(t *testing.T) {
		base := ContinuationRequest{
			Event: ContinuationEventTermination, EventDate: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
			NotifiedOn: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
			PlanRef:    "plan-ppo-2026", MemberRef: "worker-1",
		}
		for _, tc := range []struct {
			name   string
			mutate func(*ContinuationRequest)
		}{
			{"unknown event", func(r *ContinuationRequest) { r.Event = "LAYOFF_RUMOR" }},
			{"missing plan", func(r *ContinuationRequest) { r.PlanRef = "" }},
			{"missing member", func(r *ContinuationRequest) { r.MemberRef = "" }},
			{"notice before event", func(r *ContinuationRequest) { r.NotifiedOn = r.EventDate.AddDate(0, 0, -1) }},
		} {
			mutated := base
			tc.mutate(&mutated)
			if _, err := ContinuationOfferFor(mutated); !errors.Is(err, ErrContinuationInvalid) {
				t.Fatalf("%s: err = %v, want ErrContinuationInvalid", tc.name, err)
			}
		}
	})

	t.Run("GREEN_insurability_decisions_apply", func(t *testing.T) {
		stage := InsurabilityCase{MemberRef: "worker-1", CoverageAmount: "500000", GuaranteedIssue: "100000"}
		approved, err := ApplyInsurabilityDecision(stage, InsurabilityApproved, nil)
		if err != nil {
			t.Fatal(err)
		}
		if approved.EnrolledAmount != "500000" || approved.Pending {
			t.Fatalf("approved = %+v, want full amount enrolled", approved)
		}
		denied, err := ApplyInsurabilityDecision(stage, InsurabilityDenied, nil)
		if err != nil {
			t.Fatal(err)
		}
		if denied.EnrolledAmount != "100000" || denied.Pending {
			t.Fatalf("denied = %+v, want guaranteed-issue cap", denied)
		}
		pending, err := ApplyInsurabilityDecision(stage, InsurabilityMoreEvidence, []string{"exam", "labs"})
		if err != nil {
			t.Fatal(err)
		}
		if !pending.Pending || len(pending.NeededEvidence) != 2 {
			t.Fatalf("more-evidence = %+v, want pending with needed list", pending)
		}
		if _, err := ApplyInsurabilityDecision(stage, "MAYBE", nil); !errors.Is(err, ErrInsurabilityInvalid) {
			t.Fatalf("unknown decision err = %v, want ErrInsurabilityInvalid", err)
		}
	})

	t.Run("GREEN_enrollment_file_is_deterministic", func(t *testing.T) {
		asOf := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
		members := []EnrollmentMember{
			{MemberID: "w-2", LastName: "B", FirstName: "B", DateOfBirth: "1990-02-02", PlanCode: "PPO", Tier: "FAMILY", Action: EnrollmentChange, Effective: "2026-04-01"},
			{MemberID: "w-1", LastName: "A", FirstName: "A", DateOfBirth: "1985-01-01", PlanCode: "PPO", Tier: "EMPLOYEE", Action: EnrollmentAdd, Effective: "2026-04-01"},
		}
		first, err := BuildEnrollmentFile("carrier:acme", asOf, members)
		if err != nil {
			t.Fatal(err)
		}
		again, err := BuildEnrollmentFile("carrier:acme", asOf, members)
		if err != nil {
			t.Fatal(err)
		}
		if string(first.Bytes) != string(again.Bytes) || first.Digest != again.Digest {
			t.Fatal("same inputs produced different files")
		}
		lines := strings.Split(string(first.Bytes), "\n")
		if len(lines) != 4 || !strings.HasPrefix(lines[0], "HDR|carrier:acme|") || !strings.HasPrefix(lines[3], "TRL|2|1|1|0|") {
			t.Fatalf("file shape:\n%s", first.Bytes)
		}
		if !strings.HasPrefix(lines[1], "MEM|w-1|") || !strings.HasPrefix(lines[2], "MEM|w-2|") {
			t.Fatalf("members are not sorted by member id:\n%s", first.Bytes)
		}
		if _, err := BuildEnrollmentFile("carrier:acme", asOf, nil); !errors.Is(err, ErrEnrollmentInvalid) {
			t.Fatalf("empty roster err = %v, want ErrEnrollmentInvalid", err)
		}
		bad := []EnrollmentMember{{MemberID: "w-9", Action: "ENCHANT"}}
		if _, err := BuildEnrollmentFile("carrier:acme", asOf, bad); !errors.Is(err, ErrEnrollmentInvalid) {
			t.Fatalf("unknown action err = %v, want ErrEnrollmentInvalid", err)
		}
	})
}

// TestTodo_WF_CAP_016_Integration completes an insurability evidence round
// trip against a simulator and expands every binding to its fragment.
func TestTodo_WF_CAP_016_Integration(t *testing.T) {
	ctx := context.Background()
	binding := InsurabilityBinding()
	vendor := &cap016Vendor{}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	trip, err := roundtrip.StartTrip(ctx, vendor, binding, roundtrip.VendorCall{InstanceID: "instance-1", NodeID: "await_insurability", Inputs: map[string]string{"member": "worker:w1"}}, now)
	if err != nil {
		t.Fatalf("StartTrip: %v", err)
	}
	if trip.State != roundtrip.TripDispatched || len(vendor.keys) != 1 || vendor.keys[0] != "vendor:benefits.insurability.vendor:instance-1:await_insurability" {
		t.Fatalf("trip = %+v keys = %v", trip, vendor.keys)
	}
	sub, err := roundtrip.SubscriptionFor(binding, values.TenantId("acme"), trip, values.NewInstant(now.Add(48*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("sim-key")
	payload := []byte(`{"decision":"APPROVED"}`)
	sig := signal.Signal{Tenant: values.TenantId("acme"), Source: binding.Callback.AcceptedSources[0], EventType: binding.Callback.EventType,
		SchemaRef: binding.Callback.SchemaRef, CorrelationKey: binding.Callback.CorrelationKey, CorrelationValue: trip.CorrelationValue,
		IdempotencyKey: "cb-1", Payload: payload, Signature: cap016Sign(key, payload), ReceivedAt: values.NewInstant(now.Add(time.Hour))}
	trip, result, err := roundtrip.AcceptCallback(trip, sub, sig, nil, cap016Verifier{key: key}, values.NewInstant(now.Add(time.Hour)))
	if err != nil {
		t.Fatalf("AcceptCallback: %v", err)
	}
	if result.Status != signal.StatusAccepted || trip.State != roundtrip.TripCallbackAccepted || !trip.Terminal() {
		t.Fatalf("result = %+v trip = %+v", result, trip)
	}

	for _, binding := range []roundtrip.Binding{ContinuationBinding(), InsurabilityBinding(), EnrollmentBinding()} {
		entry, err := roundtrip.FragmentEntry(binding)
		if err != nil {
			t.Fatalf("%s fragment: %v", binding.ID, err)
		}
		if len(entry.Expansion.Nodes) != 3 || len(entry.Expansion.Edges) != 2 {
			t.Fatalf("%s expands to %d nodes %d edges, want dispatch/signal/observe", binding.ID, len(entry.Expansion.Nodes), len(entry.Expansion.Edges))
		}
	}
}

type cap016Vendor struct {
	keys []string
}

func (v *cap016Vendor) Dispatch(_ context.Context, _ roundtrip.Binding, call roundtrip.VendorCall, idempotencyKey string) (roundtrip.DispatchReceipt, error) {
	v.keys = append(v.keys, idempotencyKey)
	return roundtrip.DispatchReceipt{CorrelationValue: "eoi-" + call.NodeID, DispatchedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}, nil
}

func (v *cap016Vendor) Poll(context.Context, roundtrip.Binding, roundtrip.Trip) (roundtrip.PollReading, error) {
	return roundtrip.PollReading{Found: false}, nil
}

type cap016Verifier struct{ key []byte }

func (v cap016Verifier) Verify(sig signal.Signal) error {
	mac := hmac.New(sha256.New, v.key)
	mac.Write(sig.Payload)
	if !hmac.Equal(mac.Sum(nil), sig.Signature) {
		return errors.New("bad signature")
	}
	return nil
}

func cap016Sign(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// TestTodo_WF_CAP_016_Golden pins the generated enrollment file byte for
// byte: header, sorted members, trailer totals and digest.
func TestTodo_WF_CAP_016_Golden(t *testing.T) {
	asOf := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	file, err := BuildEnrollmentFile("carrier:acme", asOf, []EnrollmentMember{
		{MemberID: "w-2", LastName: "Rivera", FirstName: "Bo", DateOfBirth: "1990-02-02", PlanCode: "PPO", Tier: "FAMILY", Action: EnrollmentTerminate, Effective: "2026-04-01"},
		{MemberID: "w-1", LastName: "Adeyemi", FirstName: "Amara", DateOfBirth: "1985-01-01", PlanCode: "PPO", Tier: "EMPLOYEE", Action: EnrollmentAdd, Effective: "2026-04-01"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Pinned byte for byte; the trailer digest is the sha256 of the header
	// and member lines, independently recomputed outside the test binary.
	want := "HDR|carrier:acme|2026-04-01T12:00:00Z|2\n" +
		"MEM|w-1|Adeyemi|Amara|1985-01-01|PPO|EMPLOYEE|ADD|2026-04-01\n" +
		"MEM|w-2|Rivera|Bo|1990-02-02|PPO|FAMILY|TERMINATE|2026-04-01\n" +
		"TRL|2|1|0|1|sha256:85ff208311cee8e158e574b7513b7a8b5dfed973022fa5a22c4b24e5af682dff"
	if string(file.Bytes) != want {
		t.Fatalf("file bytes:\n%s\nwant:\n%s", file.Bytes, want)
	}
	if file.Digest != "sha256:85ff208311cee8e158e574b7513b7a8b5dfed973022fa5a22c4b24e5af682dff" {
		t.Fatalf("file digest = %q", file.Digest)
	}
}
