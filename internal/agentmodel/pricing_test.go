package agentmodel

import (
	"context"
	"errors"
	"math"
	"testing"
)

func pricingFixture(t *testing.T) (*PricingSchedule, ModelSelection) {
	t.Helper()
	identity := ModelIdentity{ProviderID: "provider-a", ModelID: "model-x", Version: "2026-09"}
	schedule, err := NewPricingSchedule(PricingSchedule{
		Version: "rates-1", Authority: "finance-admin-7", Signature: "sig-1",
		Entries: []PricingEntry{{Identity: identity, InputMicrosPerToken: 3, OutputMicrosPerToken: 7}},
	})
	if err != nil {
		t.Fatalf("NewPricingSchedule() error = %v", err)
	}
	return schedule, ModelSelection{ProfileID: "profile", ProfileDigest: "digest", Identity: identity}
}

func TestTodo_AGENT_024_Pricing(t *testing.T) {
	schedule, selection := pricingFixture(t)
	usage := ModelUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14, CostMicros: 58}
	got, err := schedule.Reconcile(selection, usage)
	if err != nil || got != 58 {
		t.Fatalf("Reconcile() = %d, %v; want 58", got, err)
	}
	if err := schedule.Validate(context.Background(), selection, ModelLimits{MaxInputTokens: 10, MaxOutputTokens: 4, MaxCostMicros: 58}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestTodo_AGENT_024_PerMillionPricing(t *testing.T) {
	identity := ModelIdentity{ProviderID: "openai", ModelID: "gpt-5-mini-2025-08-07", Version: "2025-08-07"}
	entry := PricingEntry{Identity: identity, InputMicrosPerMillionTokens: 250000, OutputMicrosPerMillionTokens: 2000000}
	schedule, err := NewPricingSchedule(PricingSchedule{Version: "published-test", Authority: "verified-admin", Signature: "test-signature", Entries: []PricingEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	selection := ModelSelection{Identity: identity}
	for _, tc := range []struct{ input, output, want int64 }{{1000, 100, 450}, {1, 0, 1}, {4, 0, 1}, {5, 0, 2}} {
		cost, err := schedule.Cost(selection, ModelUsage{InputTokens: tc.input, OutputTokens: tc.output, TotalTokens: tc.input + tc.output})
		if err != nil || cost != tc.want {
			t.Fatalf("cost(%d,%d)=%d,%v want %d", tc.input, tc.output, cost, err, tc.want)
		}
	}
	entry.InputMicrosPerToken = 1
	if _, err := NewPricingSchedule(PricingSchedule{Version: "mixed", Authority: "admin", Signature: "test", Entries: []PricingEntry{entry}}); !errors.Is(err, ErrPricingInvalid) {
		t.Fatalf("mixed pricing=%v", err)
	}
	entry.InputMicrosPerToken = 0
	entry.CachedInputMicrosPerMillionTokens = 25000
	cachedCost, err := entryCostWithCache(entry, 1000, 1000, 100)
	if err != nil || cachedCost != 225 {
		t.Fatalf("cached cost=%d,%v", cachedCost, err)
	}
	if _, err := entryCostWithCache(entry, 100, 101, 0); !errors.Is(err, ErrPricingInvalid) {
		t.Fatalf("cached token overflow=%v", err)
	}
	entry.InputMicrosPerMillionTokens = math.MaxInt64
	if _, err := entryCost(entry, math.MaxInt64, 0); !errors.Is(err, ErrPricingOverflow) {
		t.Fatalf("overflow=%v", err)
	}
}

func TestPricingSchedule_RejectsUnsignedZeroAndDuplicate(t *testing.T) {
	identity := ModelIdentity{ProviderID: "p", ModelID: "m", Version: "v"}
	tests := []struct {
		name  string
		input PricingSchedule
	}{
		{name: "missing signature", input: PricingSchedule{Version: "v", Authority: "admin", Entries: []PricingEntry{{Identity: identity, InputMicrosPerToken: 1, OutputMicrosPerToken: 1}}}},
		{name: "zero input rate", input: PricingSchedule{Version: "v", Authority: "admin", Signature: "sig", Entries: []PricingEntry{{Identity: identity, OutputMicrosPerToken: 1}}}},
		{name: "duplicate identity", input: PricingSchedule{Version: "v", Authority: "admin", Signature: "sig", Entries: []PricingEntry{{Identity: identity, InputMicrosPerToken: 1, OutputMicrosPerToken: 1}, {Identity: identity, InputMicrosPerToken: 2, OutputMicrosPerToken: 2}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPricingSchedule(tc.input); !errors.Is(err, ErrPricingInvalid) {
				t.Fatalf("error = %v, want ErrPricingInvalid", err)
			}
		})
	}
}

func TestPricingSchedule_ReconcileRefusesMismatchOverflowAndZero(t *testing.T) {
	schedule, selection := pricingFixture(t)
	tests := []struct {
		name  string
		usage ModelUsage
		want  error
	}{
		{name: "reported mismatch", usage: ModelUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, CostMicros: 99}, want: ErrPricingMismatch},
		{name: "zero usage", usage: ModelUsage{TotalTokens: 0, CostMicros: 0}, want: ErrPricingZeroCost},
		{name: "negative usage", usage: ModelUsage{InputTokens: -1, TotalTokens: 0}, want: ErrPricingInvalid},
		{name: "total inconsistent", usage: ModelUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 2, CostMicros: 13}, want: ErrPricingInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := schedule.Reconcile(selection, tc.usage); !errors.Is(err, tc.want) {
				t.Fatalf("Reconcile() error = %v, want %v", err, tc.want)
			}
		})
	}
	tooLarge := ModelUsage{InputTokens: math.MaxInt64, OutputTokens: 0, TotalTokens: math.MaxInt64, CostMicros: 0}
	if _, err := schedule.Reconcile(selection, tooLarge); !errors.Is(err, ErrPricingOverflow) {
		t.Fatalf("overflow error = %v, want ErrPricingOverflow", err)
	}
}

func TestPricingSchedule_ValidatePinsExactIdentityAndDigest(t *testing.T) {
	schedule, selection := pricingFixture(t)
	selection.Identity.Version = "old"
	if err := schedule.Validate(context.Background(), selection, ModelLimits{}); !errors.Is(err, ErrPricingUnavailable) {
		t.Fatalf("wrong version error = %v, want ErrPricingUnavailable", err)
	}
	selection.Identity.Version = "2026-09"
	if err := schedule.Validate(context.Background(), selection, ModelLimits{MaxInputTokens: 10, MaxOutputTokens: 10, MaxCostMicros: 1}); !errors.Is(err, ErrPricingMismatch) {
		t.Fatalf("cost limit error = %v, want ErrPricingMismatch", err)
	}
	if schedule.Digest != PricingScheduleDigest(*schedule) || schedule.Digest == "" {
		t.Fatalf("digest = %q, want canonical schedule digest", schedule.Digest)
	}
	if _, err := NewPricingSchedule(PricingSchedule{Version: schedule.Version, Authority: schedule.Authority, Signature: schedule.Signature, Digest: "bad", Entries: schedule.Entries}); !errors.Is(err, ErrPricingInvalid) {
		t.Fatalf("digest mismatch error = %v, want ErrPricingInvalid", err)
	}
}
