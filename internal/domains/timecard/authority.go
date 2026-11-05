package timecard

import (
	"strings"
	"time"
)

// AuthorityClass is the WF-CAP-007 per-tenant timecard authority decision.
// NATIVE means the timecard aggregate in this package masters the tenant's
// timecards; DELEGATED means an external timekeeping system does and this
// package only reviews its evidence.
type AuthorityClass string

const (
	AuthorityNative    AuthorityClass = "NATIVE"
	AuthorityDelegated AuthorityClass = "DELEGATED"
)

func (c AuthorityClass) Valid() bool {
	return c == AuthorityNative || c == AuthorityDelegated
}

// TenantAuthority records one tenant's timecard authority decision. It is a
// pure value: recording it here decides nothing globally and grants no
// write capability outside the class it names.
type TenantAuthority struct {
	Tenant       string
	Class        AuthorityClass
	SourceSystem string
	DecidedBy    string
	DecidedAt    time.Time
}

// Native reports whether the tenant's timecards are mastered natively.
func (a TenantAuthority) Native() bool { return a.Class == AuthorityNative }

// Validate checks that the record names a tenant, a declared class, a
// decider and a decision time, and that a delegated record names exactly
// one external source system while a native record names none.
func (a TenantAuthority) Validate() error {
	if strings.TrimSpace(a.Tenant) == "" {
		return reject("authority.tenant", "MISSING", "tenant is required")
	}
	if !a.Class.Valid() {
		return reject("authority.class", "INVALID", "authority class is not declared")
	}
	if a.Class == AuthorityDelegated && strings.TrimSpace(a.SourceSystem) == "" {
		return reject("authority.source_system", "MISSING", "a delegated tenant must name its source system")
	}
	if a.Class == AuthorityNative && strings.TrimSpace(a.SourceSystem) != "" {
		return reject("authority.source_system", "FORBIDDEN", "a native tenant must not name a source system")
	}
	if strings.TrimSpace(a.DecidedBy) == "" || a.DecidedAt.IsZero() {
		return reject("authority.decision", "MISSING", "decider and decision time are required")
	}
	return nil
}

// RecordAuthority records one tenant's timecard authority decision.
func RecordAuthority(tenant string, class AuthorityClass, sourceSystem, decidedBy string, decidedAt time.Time) (TenantAuthority, error) {
	record := TenantAuthority{Tenant: tenant, Class: class, SourceSystem: sourceSystem, DecidedBy: decidedBy, DecidedAt: decidedAt}
	if err := record.Validate(); err != nil {
		return TenantAuthority{}, err
	}
	return record, nil
}
