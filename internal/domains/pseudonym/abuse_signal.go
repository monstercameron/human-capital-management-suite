package pseudonym

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/abuse"
)

const (
	// DenialSignalSourceSystem is the default source stamped on a denial.
	DenialSignalSourceSystem = "pseudonym-scope-limiter"
	// DenialScopeRefType marks a pseudonymous intake scope, never a person.
	DenialScopeRefType = "pseudonymous-scope"
	// DenialCodeRefType identifies the closed-vocabulary denial cause.
	DenialCodeRefType = "pseudonymous-denial"
)

// DenialSignal projects one limiter denial into a minimized ABUSE-001 signal.
func DenialSignal(scope string, code LimitCode, tenant, sourceSystem string, observedAt time.Time, sequence uint64) (abuse.ActivitySignal, error) {
	if strings.TrimSpace(scope) == "" || !code.Valid() {
		return abuse.ActivitySignal{}, fmt.Errorf("%w: denial scope and code are required", ErrLimitPolicy)
	}
	if strings.TrimSpace(tenant) == "" {
		return abuse.ActivitySignal{}, fmt.Errorf("pseudonym: denial signal tenant is required: %w", abuse.ErrSignalTenant)
	}
	if observedAt.IsZero() {
		return abuse.ActivitySignal{}, fmt.Errorf("pseudonym: denial signal instant is required: %w", abuse.ErrSignalObservedAt)
	}
	if strings.TrimSpace(sourceSystem) == "" {
		sourceSystem = DenialSignalSourceSystem
	}
	signal := abuse.ActivitySignal{
		ID:           denialSignalID(scope, code, tenant, observedAt, sequence),
		Kind:         abuse.SignalKindAuthAnomaly,
		Subject:      abuse.Ref{Type: DenialScopeRefType, ID: scope},
		Actor:        abuse.Ref{Type: DenialCodeRefType, ID: string(code)},
		Tenant:       tenant,
		ObservedAt:   observedAt.UTC(),
		SourceSystem: sourceSystem,
	}
	if err := signal.Validate(); err != nil {
		return abuse.ActivitySignal{}, err
	}
	return signal, nil
}

func denialSignalID(scope string, code LimitCode, tenant string, observedAt time.Time, sequence uint64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		scope, string(code), tenant,
		observedAt.UTC().Format(time.RFC3339Nano), fmt.Sprint(sequence),
	}, "\x00")))
	return "pseudonym-denial-" + hex.EncodeToString(sum[:])
}

// DenialSignals returns one governed activity signal per recorded denial.
func (l *ScopeLimiter) DenialSignals(tenant, sourceSystem string) ([]abuse.ActivitySignal, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("pseudonym: denial signal tenant is required: %w", abuse.ErrSignalTenant)
	}
	out := make([]abuse.ActivitySignal, 0, len(l.reviews))
	for index, review := range l.reviews {
		signal, err := DenialSignal(review.Scope, review.Code, tenant, sourceSystem, review.At, uint64(index))
		if err != nil {
			return nil, err
		}
		out = append(out, signal)
	}
	return out, nil
}
