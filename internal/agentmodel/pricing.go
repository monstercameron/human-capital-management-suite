package agentmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strings"
)

var (
	// ErrPricingInvalid marks a malformed or unauthoritative schedule.
	ErrPricingInvalid = errors.New("agentmodel: invalid pricing schedule")
	// ErrPricingUnavailable marks a schedule that has no price for a pinned model.
	ErrPricingUnavailable = errors.New("agentmodel: pricing is unavailable")
	// ErrPricingMismatch marks provider usage whose cost differs from the schedule.
	ErrPricingMismatch = errors.New("agentmodel: pricing does not reconcile")
	// ErrPricingOverflow marks an unrepresentable token-cost multiplication.
	ErrPricingOverflow = errors.New("agentmodel: pricing cost overflow")
	// ErrPricingZeroCost marks a schedule or usage that would silently erase cost.
	ErrPricingZeroCost = errors.New("agentmodel: zero-cost pricing is refused")
)

// PricingEntry pins token rates to the complete provider/model/version identity.
// Rates use integer micro-cost units per token or per million tokens. The two
// representations cannot be mixed; cached input may use an approved lower rate.
type PricingEntry struct {
	Identity                          ModelIdentity `json:"identity"`
	InputMicrosPerToken               int64         `json:"input_micros_per_token"`
	OutputMicrosPerToken              int64         `json:"output_micros_per_token"`
	InputMicrosPerMillionTokens       int64         `json:"input_micros_per_million_tokens,omitempty"`
	OutputMicrosPerMillionTokens      int64         `json:"output_micros_per_million_tokens,omitempty"`
	CachedInputMicrosPerMillionTokens int64         `json:"cached_input_micros_per_million_tokens,omitempty"`
}

// PricingSchedule is an administrator-signed, versioned set of immutable rates.
// Signature verification belongs to the authority that loads the document; this
// package requires the resulting authority and signature references to be present.
type PricingSchedule struct {
	Version   string         `json:"version"`
	Authority string         `json:"authority"`
	Signature string         `json:"signature"`
	Digest    string         `json:"digest,omitempty"`
	Entries   []PricingEntry `json:"entries"`

	entries map[ModelIdentity]PricingEntry
}

// SignedPricingSchedule is the descriptive name used by callers loading an
// administrator-signed document.
type SignedPricingSchedule = PricingSchedule

// NewPricingSchedule verifies and freezes an administrator-supplied schedule.
// The caller must verify Signature against its admin trust store before calling
// this function. No production rates are supplied by this package.
func NewPricingSchedule(input PricingSchedule) (*PricingSchedule, error) {
	if strings.TrimSpace(input.Version) == "" || strings.TrimSpace(input.Authority) == "" || strings.TrimSpace(input.Signature) == "" || len(input.Entries) == 0 {
		return nil, ErrPricingInvalid
	}
	entries := slices.Clone(input.Entries)
	sort.Slice(entries, func(i, j int) bool { return identityKey(entries[i].Identity) < identityKey(entries[j].Identity) })
	indexed := make(map[ModelIdentity]PricingEntry, len(entries))
	for _, entry := range entries {
		if !validIdentity(entry.Identity) || !validPricingRates(entry) {
			return nil, ErrPricingInvalid
		}
		if _, exists := indexed[entry.Identity]; exists {
			return nil, fmt.Errorf("%w: duplicate model identity", ErrPricingInvalid)
		}
		indexed[entry.Identity] = entry
	}
	digest := pricingScheduleDigest(input.Version, input.Authority, entries)
	if input.Digest != "" && input.Digest != digest {
		return nil, fmt.Errorf("%w: schedule digest mismatch", ErrPricingInvalid)
	}
	return &PricingSchedule{Version: input.Version, Authority: input.Authority, Signature: input.Signature, Digest: digest, Entries: entries, entries: indexed}, nil
}

// PricingScheduleDigest returns the canonical digest an administrator signs.
func PricingScheduleDigest(schedule PricingSchedule) string {
	entries := slices.Clone(schedule.Entries)
	sort.Slice(entries, func(i, j int) bool { return identityKey(entries[i].Identity) < identityKey(entries[j].Identity) })
	return pricingScheduleDigest(schedule.Version, schedule.Authority, entries)
}

// Validate confirms that a pinned model has an authoritative rate and that its
// requested maximum token budget is affordable without integer overflow.
func (s *PricingSchedule) Validate(ctx context.Context, selection ModelSelection, limits ModelLimits) error {
	if ctx == nil {
		return ErrPricingInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := s.entry(selection.Identity)
	if err != nil {
		return err
	}
	if limits.MaxInputTokens < 0 || limits.MaxOutputTokens < 0 || limits.MaxCostMicros < 0 {
		return ErrPricingInvalid
	}
	cost, err := entryCost(entry, limits.MaxInputTokens, limits.MaxOutputTokens)
	if err != nil {
		return err
	}
	if limits.MaxCostMicros > 0 && cost > limits.MaxCostMicros {
		return fmt.Errorf("%w: maximum token budget exceeds request limit", ErrPricingMismatch)
	}
	return nil
}

// Reconcile computes the authoritative cost for provider usage and requires the
// provider-reported cost to match it exactly.
func (s *PricingSchedule) Reconcile(selection ModelSelection, usage ModelUsage) (int64, error) {
	cost, err := s.Cost(selection, usage)
	if err != nil {
		return 0, err
	}
	if usage.CostMicros != cost {
		return 0, fmt.Errorf("%w: got %d want %d", ErrPricingMismatch, usage.CostMicros, cost)
	}
	return cost, nil
}

// Cost computes the approved cost from observed token usage. Providers such as
// OpenAI report tokens rather than currency; adapters attach this authoritative
// cost before the egress dispatcher validates the result.
func (s *PricingSchedule) Cost(selection ModelSelection, usage ModelUsage) (int64, error) {
	entry, err := s.entry(selection.Identity)
	if err != nil {
		return 0, err
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.TotalTokens < 0 || usage.CostMicros < 0 || usage.InputTokens > usage.TotalTokens || usage.OutputTokens > usage.TotalTokens-usage.InputTokens || usage.CachedInputTokens < 0 || usage.CachedInputTokens > usage.InputTokens {
		return 0, ErrPricingInvalid
	}
	cost, err := entryCostWithCache(entry, usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens)
	if err != nil {
		return 0, err
	}
	if cost == 0 {
		return 0, ErrPricingZeroCost
	}
	return cost, nil
}

func (s *PricingSchedule) entry(identity ModelIdentity) (PricingEntry, error) {
	if s == nil || s.entries == nil {
		return PricingEntry{}, ErrPricingUnavailable
	}
	entry, ok := s.entries[identity]
	if !ok {
		return PricingEntry{}, ErrPricingUnavailable
	}
	return entry, nil
}

func multiplyCost(input, inputRate, output, outputRate int64) (int64, error) {
	if input < 0 || output < 0 || inputRate <= 0 || outputRate <= 0 {
		return 0, ErrPricingInvalid
	}
	if input > math.MaxInt64/inputRate || output > math.MaxInt64/outputRate {
		return 0, ErrPricingOverflow
	}
	inputCost, outputCost := input*inputRate, output*outputRate
	if inputCost > math.MaxInt64-outputCost {
		return 0, ErrPricingOverflow
	}
	return inputCost + outputCost, nil
}

func validPricingRates(entry PricingEntry) bool {
	return (entry.InputMicrosPerToken > 0 && entry.OutputMicrosPerToken > 0 && entry.InputMicrosPerMillionTokens == 0 && entry.OutputMicrosPerMillionTokens == 0 && entry.CachedInputMicrosPerMillionTokens == 0) ||
		(entry.InputMicrosPerToken == 0 && entry.OutputMicrosPerToken == 0 && entry.InputMicrosPerMillionTokens > 0 && entry.OutputMicrosPerMillionTokens > 0 && entry.CachedInputMicrosPerMillionTokens >= 0 && entry.CachedInputMicrosPerMillionTokens <= entry.InputMicrosPerMillionTokens)
}

// Per-million rates preserve fractional micro-cost per token. Total cost is
// rounded up once to the smallest representable micro-cost unit so budgets
// cannot silently erase inexpensive calls. Mixed rate formats are refused.
func entryCost(entry PricingEntry, input, output int64) (int64, error) {
	return entryCostWithCache(entry, input, 0, output)
}

func entryCostWithCache(entry PricingEntry, input, cached, output int64) (int64, error) {
	if input < 0 || output < 0 || cached < 0 || cached > input || !validPricingRates(entry) {
		return 0, ErrPricingInvalid
	}
	if entry.InputMicrosPerToken > 0 {
		return multiplyCost(input, entry.InputMicrosPerToken, output, entry.OutputMicrosPerToken)
	}
	cachedRate := entry.CachedInputMicrosPerMillionTokens
	if cachedRate == 0 {
		cachedRate = entry.InputMicrosPerMillionTokens
	}
	inputCost := new(big.Int).Mul(big.NewInt(input-cached), big.NewInt(entry.InputMicrosPerMillionTokens))
	inputCost.Add(inputCost, new(big.Int).Mul(big.NewInt(cached), big.NewInt(cachedRate)))
	outputCost := new(big.Int).Mul(big.NewInt(output), big.NewInt(entry.OutputMicrosPerMillionTokens))
	cost := new(big.Int).Add(inputCost, outputCost)
	cost.Add(cost, big.NewInt(999999))
	cost.Quo(cost, big.NewInt(1000000))
	if !cost.IsInt64() {
		return 0, ErrPricingOverflow
	}
	return cost.Int64(), nil
}

func pricingScheduleDigest(version, authority string, entries []PricingEntry) string {
	payload, _ := json.Marshal(struct {
		Version   string         `json:"version"`
		Authority string         `json:"authority"`
		Entries   []PricingEntry `json:"entries"`
	}{version, authority, entries})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func identityKey(identity ModelIdentity) string {
	return identity.ProviderID + "\x00" + identity.ModelID + "\x00" + identity.Version
}
