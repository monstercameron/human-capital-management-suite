package cryptoagile

import (
	"errors"
	"fmt"
	"time"
)

// KeySource is the production key boundary used by Runtime. Implementations
// keep key material behind the port; CustodyKeySource is the real custody
// adapter and FakeKeySource is reserved for tests.
type KeySource interface {
	SignerPort
	VerifierPort
}

// Runtime composes one registry, dual signer and dual-read verifier for a
// declared migration plan. The value owns the registry so callers cannot
// accidentally share mutable algorithm state between tenants or migrations.
type Runtime struct {
	Plan     MigrationPlan
	Registry *Registry
	Signer   *DualSigner
	Verifier *EnvelopeVerifier
}

// ErrRuntimeSuite reports a migration window that names no registered suite.
var ErrRuntimeSuite = errors.New("cryptoagile: migration plan names an unregistered suite")

// NewRuntime composes the crypto-agility machinery used by a served trust
// boundary. It validates the plan, registers each configured suite exactly
// once, and rejects a plan that could otherwise fail only when its first
// signing window becomes current.
func NewRuntime(plan MigrationPlan, suites []AlgorithmSuite, keys KeySource, now func() time.Time) (*Runtime, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	registry := NewRegistry()
	for _, suite := range suites {
		if err := registry.Register(suite); err != nil {
			return nil, fmt.Errorf("cryptoagile: register suite %q: %w", suite.ID, err)
		}
	}
	for _, window := range plan.Windows {
		for _, suiteID := range []string{window.ActiveSuiteID, window.DualSuiteID} {
			if suiteID == "" {
				continue
			}
			if _, ok := registry.Get(suiteID); !ok {
				return nil, fmt.Errorf("%w: %q", ErrRuntimeSuite, suiteID)
			}
		}
	}
	signer, err := NewDualSigner(plan, keys, now)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Plan:     plan,
		Registry: registry,
		Signer:   signer,
		Verifier: NewEnvelopeVerifier(registry, keys),
	}, nil
}
