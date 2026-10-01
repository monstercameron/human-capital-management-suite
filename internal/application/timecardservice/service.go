package timecardservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Service is the application-service boundary for the time-keeping lane.
// Every field is a small port; the zero value of any required port makes
// every command return ErrUnavailable rather than panic.
type Service struct {
	Timecards    TimecardStore
	Shifts       ShiftStore
	Profiles     ProfileStore
	MissedPunch  MissedPunchStore
	Allocations  AllocationStore
	Ledger       LedgerStore
	Destinations map[string]Dispatcher
	DestBindings DestinationStore
	Premiums     PremiumCalculator

	Auth    Authorizer
	Workers WorkerDirectory
	Notify  Notifier
	IDGen   IDs
	Clock   func() time.Time
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validPrincipal(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || string(p.Tenant()) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func tenantOf(p *trust.Principal) string { return string(p.Tenant()) }

// digest returns a stable content digest of a command payload, used both as
// the idempotency check and as the value pinned into approval/allocation
// records.
func digest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newID(s Service, tenant, actor, operation, key string) string {
	if s.IDGen != nil {
		return s.IDGen.NewID(tenant, actor, operation, key)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{tenant, actor, operation, key}, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// authorize is the one place every command checks capability before any
// side effect. It requires a non-empty workerRef so a capability can never
// be evaluated against an unscoped record.
func (s Service) authorize(ctx context.Context, p *trust.Principal, tenant, workerRef string, cap Capability) error {
	if s.Auth == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(workerRef) == "" {
		return ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, tenant, workerRef, cap); err != nil {
		return err
	}
	return nil
}

// requireInScope additionally verifies, through WorkerDirectory, that the
// deciding actor's supervisory scope currently covers workerRef. It is used
// by commands (missed-punch decisions, timecard approval) where scope is a
// second, independent check beyond the coarse capability grant.
func (s Service) requireInScope(ctx context.Context, tenant, supervisor, workerRef string) error {
	if s.Workers == nil {
		return ErrUnavailable
	}
	if supervisor == workerRef {
		return ErrForbidden
	}
	ok, err := s.Workers.InScope(ctx, tenant, supervisor, workerRef)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

// dispatcherFor resolves the Dispatcher bound to a profile's destination. It
// consults DestBindings when set (a tenant-configurable indirection) and
// otherwise looks up the destination's own string key directly in
// Destinations, so a test can wire Destinations without a DestinationStore.
func (s Service) dispatcherFor(ctx context.Context, tenant string, dest timeprofile.Destination) (Dispatcher, error) {
	key := string(dest)
	if s.DestBindings != nil {
		resolved, err := s.DestBindings.DispatcherKeyFor(ctx, tenant, dest)
		if err != nil {
			return nil, err
		}
		if resolved != "" {
			key = resolved
		}
	}
	d, ok := s.Destinations[key]
	if !ok || d == nil {
		return nil, ErrNoDestination
	}
	return d, nil
}
