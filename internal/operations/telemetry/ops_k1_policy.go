// Package telemetry owns the versioned operational policy that the live
// telemetry evaluator enforces. It contains policy facts only; signal
// construction and export adapters remain in internal/platform/telemetry.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const ContractVersion = 1

// CriticalFailure is a closed class whose telemetry is never sampled away.
type CriticalFailure string

const (
	SecurityDenial           CriticalFailure = "security_denial"
	FinancialMutation        CriticalFailure = "financial_mutation"
	IrreversibleEffect       CriticalFailure = "irreversible_effect"
	AmbiguousResult          CriticalFailure = "ambiguous_result"
	CorrectnessFailure       CriticalFailure = "correctness_failure"
	TelemetryPipelineFailure CriticalFailure = "telemetry_pipeline_failure"
)

// Policy is the OPS-002 runtime contract. The platform evaluator supplies the
// attribute allow-list and per-key budgets; this contract supplies the
// bounded global policy and the forced-retention set they must honor.
type Policy struct {
	Version                int
	SuccessSampleRate      float64
	MetricCardinalityLimit int
	CriticalFailures       []CriticalFailure
}

// Receipt is a deterministic policy receipt attached to every sampling
// decision. Signature is a digest of the versioned policy and decision facts;
// it is evidence of which policy was applied, never a business authority.
type Receipt struct {
	PolicyVersion int
	PolicyDigest  string
	Decision      string
	FailureClass  CriticalFailure
	Retained      bool
	Signature     string
}

var ErrInvalidPolicy = errors.New("telemetry policy: invalid policy")

// DefaultPolicy is the sole compiled-in OPS-002 policy used by the live
// platform evaluator. Its values mirror the published telemetry contract.
func DefaultPolicy() Policy {
	return Policy{
		Version:                ContractVersion,
		SuccessSampleRate:      0.1,
		MetricCardinalityLimit: 256,
		CriticalFailures: []CriticalFailure{
			SecurityDenial,
			FinancialMutation,
			IrreversibleEffect,
			AmbiguousResult,
			CorrectnessFailure,
			TelemetryPipelineFailure,
		},
	}
}

func (p Policy) Validate() error {
	if p.Version != ContractVersion {
		return fmt.Errorf("%w: version %d", ErrInvalidPolicy, p.Version)
	}
	if p.SuccessSampleRate < 0 || p.SuccessSampleRate > 1 {
		return fmt.Errorf("%w: success sample rate must be between 0 and 1", ErrInvalidPolicy)
	}
	if p.MetricCardinalityLimit <= 0 {
		return fmt.Errorf("%w: metric cardinality limit must be positive", ErrInvalidPolicy)
	}
	seen := make(map[CriticalFailure]struct{}, len(p.CriticalFailures))
	for _, class := range p.CriticalFailures {
		if strings.TrimSpace(string(class)) == "" {
			return fmt.Errorf("%w: critical failure class is empty", ErrInvalidPolicy)
		}
		if _, ok := seen[class]; ok {
			return fmt.Errorf("%w: duplicate critical failure class %q", ErrInvalidPolicy, class)
		}
		seen[class] = struct{}{}
	}
	return nil
}

func (p Policy) RequiresRetention(class string) bool {
	for _, critical := range p.CriticalFailures {
		if string(critical) == class {
			return true
		}
	}
	return false
}

// Digest identifies policy facts without including any customer or payload
// value. Sorting makes it stable even if a caller copied the class set in a
// different order.
func (p Policy) Digest() string {
	classes := make([]string, 0, len(p.CriticalFailures))
	for _, class := range p.CriticalFailures {
		classes = append(classes, string(class))
	}
	sort.Strings(classes)
	canonical := fmt.Sprintf("ops-002/v%d|sample=%.6f|metric_limit=%d|critical=%s", p.Version, p.SuccessSampleRate, p.MetricCardinalityLimit, strings.Join(classes, ","))
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Receipt records the exact policy application without retaining a raw
// attribute value, correlation id, or operation payload.
func (p Policy) Receipt(decision string, class CriticalFailure, retained bool) Receipt {
	digest := p.Digest()
	canonical := fmt.Sprintf("%s|decision=%s|failure=%s|retained=%t", digest, decision, class, retained)
	sum := sha256.Sum256([]byte(canonical))
	return Receipt{
		PolicyVersion: p.Version,
		PolicyDigest:  digest,
		Decision:      decision,
		FailureClass:  class,
		Retained:      retained,
		Signature:     "sha256:" + hex.EncodeToString(sum[:]),
	}
}
