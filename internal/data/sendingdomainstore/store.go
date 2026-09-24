// Package sendingdomainstore persists tenant-owned active email sending
// domains and supplies the live send gate.
package sendingdomainstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

var ErrProfileAbsent = errors.New("sendingdomainstore: active sending-domain profile not found")

type Store struct {
	db       dbport.Beginner
	tenant   uuid.UUID
	maxStale time.Duration
	mu       sync.RWMutex
	blocked  map[string]struct{}
}

func New(db dbport.Beginner, tenant uuid.UUID, maxStale ...time.Duration) (*Store, error) {
	if db == nil || tenant == uuid.Nil {
		return nil, errors.New("sendingdomainstore: database and tenant are required")
	}
	staleAfter := 12 * time.Hour
	if len(maxStale) > 1 || (len(maxStale) == 1 && maxStale[0] <= 0) {
		return nil, errors.New("sendingdomainstore: freshness window must be positive")
	}
	if len(maxStale) == 1 {
		staleAfter = maxStale[0]
	}
	return &Store{db: db, tenant: tenant, maxStale: staleAfter, blocked: make(map[string]struct{})}, nil
}

func (s *Store) ListActive(ctx context.Context) ([]delivery.ActiveSendingDomain, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT domain, owner, profile, last_checked_at, COALESCE(alert_cycle,'') FROM sending_domain_profile WHERE tenant_id=$1 AND active=true ORDER BY domain`, s.tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []delivery.ActiveSendingDomain
	for rows.Next() {
		var domain, owner string
		var raw []byte
		var lastChecked time.Time
		var alertCycle string
		if err := rows.Scan(&domain, &owner, &raw, &lastChecked, &alertCycle); err != nil {
			return nil, err
		}
		var profile delivery.DomainProfile
		if err := json.Unmarshal(raw, &profile); err != nil {
			return nil, fmt.Errorf("sendingdomainstore: decode profile %s: %w", domain, err)
		}
		if profile.Domain != domain || profile.Digest == "" {
			return nil, fmt.Errorf("sendingdomainstore: invalid profile for %s", domain)
		}
		out = append(out, delivery.ActiveSendingDomain{TenantID: s.tenant.String(), Profile: profile, Owner: owner, LastCheckedAt: lastChecked, AlertCycle: alertCycle})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) SaveProfile(ctx context.Context, domain delivery.ActiveSendingDomain) error {
	if err := s.validate(domain); err != nil {
		return err
	}
	if !domain.Profile.Verified {
		s.setBlocked(domain.Profile.Domain, true)
	}
	raw, err := json.Marshal(domain.Profile)
	if err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	count, err := tx.Exec(ctx, `UPDATE sending_domain_profile SET profile=$3::jsonb, verified=$4, last_checked_at=$5, alert_cycle=NULLIF($6,'') WHERE tenant_id=$1 AND domain=$2 AND active=true`, s.tenant, domain.Profile.Domain, raw, domain.Profile.Verified, domain.LastCheckedAt, domain.AlertCycle)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrProfileAbsent
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if domain.Profile.Verified {
		s.setBlocked(domain.Profile.Domain, false)
	}
	return nil
}

// BeginCheck durably revokes the prior send permit before scheduled DNS I/O.
// Every sender instance reads this row, so a later DNS or alert failure cannot
// leave an earlier verification active in another process.
func (s *Store) BeginCheck(ctx context.Context, domain delivery.ActiveSendingDomain, at time.Time) error {
	if at.IsZero() {
		return delivery.ErrInvalidEmail
	}
	domain.LastCheckedAt = at.UTC()
	domain.Profile.Verified = false
	domain.Profile.SPFVerified = false
	domain.Profile.DKIMVerified = false
	domain.Profile.DMARCVerified = false
	if strings.TrimSpace(domain.AlertCycle) == "" {
		domain.AlertCycle = at.UTC().Format(time.RFC3339Nano)
	}
	return s.SaveProfile(ctx, domain)
}

// Activate registers an initially verified profile. Domain changes go through
// SaveProfile, which preserves the active row's owner and tenant boundary.
func (s *Store) Activate(ctx context.Context, profile delivery.DomainProfile, owner string) error {
	verifiedAt, err := time.Parse(time.RFC3339, profile.VerifiedAt)
	if err != nil || verifiedAt.IsZero() {
		return delivery.ErrInvalidEmail
	}
	row := delivery.ActiveSendingDomain{TenantID: s.tenant.String(), Profile: profile, Owner: owner, LastCheckedAt: verifiedAt}
	if err := s.validate(row); err != nil {
		return err
	}
	if !profile.Verified {
		return delivery.ErrUnverifiedDomain
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO sending_domain_profile (tenant_id,domain,owner,profile,active,verified,last_checked_at) VALUES ($1,$2,$3,$4::jsonb,true,true,$5) ON CONFLICT (tenant_id,domain) DO UPDATE SET owner=EXCLUDED.owner, profile=EXCLUDED.profile, active=true, verified=true, last_checked_at=EXCLUDED.last_checked_at`, s.tenant, profile.Domain, owner, raw, verifiedAt)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.setBlocked(profile.Domain, false)
	return nil
}

// ActivateFromDNS verifies the initial SPF, DKIM and DMARC evidence before
// making a tenant domain active for transactional sending.
func (s *Store) ActivateFromDNS(ctx context.Context, domain string, records delivery.DNSRecords, owner string, now time.Time) (delivery.DomainProfile, error) {
	profile, err := delivery.VerifyDomain(domain, records, now)
	if err != nil {
		return delivery.DomainProfile{}, err
	}
	if err := s.Activate(ctx, profile, owner); err != nil {
		return delivery.DomainProfile{}, err
	}
	return profile, nil
}

// AuthorizeSendingDomain is checked before every SMTP transaction. A database
// failure, missing row, inactive row, or failed scheduled verification closes
// the send gate.
func (s *Store) AuthorizeSendingDomain(ctx context.Context, tenantID, domain string) error {
	if s == nil || strings.TrimSpace(tenantID) != s.tenant.String() || strings.TrimSpace(domain) == "" {
		return delivery.ErrUnverifiedDomain
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return delivery.ErrUnverifiedDomain
	}
	defer tx.Rollback(ctx)
	var verified bool
	var lastChecked time.Time
	var databaseNow time.Time
	var openAlert bool
	incidentPrefix := domainAlertPrefix(strings.ToLower(domain))
	err = tx.QueryRow(ctx, `SELECT profile.verified,profile.last_checked_at,clock_timestamp(),
		EXISTS (SELECT 1 FROM operational_incident incident WHERE incident.tenant_id=profile.tenant_id AND left(incident.incident_key,length($3))=$3 AND incident.status IN ('OPEN','CONTAINED'))
		FROM sending_domain_profile profile WHERE profile.tenant_id=$1 AND profile.domain=$2 AND profile.active=true`, s.tenant, strings.ToLower(domain), incidentPrefix).Scan(&verified, &lastChecked, &databaseNow, &openAlert)
	if err != nil || !verified || openAlert || lastChecked.IsZero() || lastChecked.After(databaseNow) || databaseNow.Sub(lastChecked) > s.maxStale {
		return delivery.ErrUnverifiedDomain
	}
	s.mu.RLock()
	_, blocked := s.blocked[strings.ToLower(domain)]
	s.mu.RUnlock()
	if blocked {
		return delivery.ErrUnverifiedDomain
	}
	if err := tx.Commit(ctx); err != nil {
		return delivery.ErrUnverifiedDomain
	}
	return nil
}

func (s *Store) setBlocked(domain string, blocked bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if blocked {
		s.blocked[strings.ToLower(domain)] = struct{}{}
	} else {
		delete(s.blocked, strings.ToLower(domain))
	}
}

func (s *Store) validate(domain delivery.ActiveSendingDomain) error {
	if s == nil || domain.TenantID != s.tenant.String() || strings.TrimSpace(domain.Owner) == "" || strings.TrimSpace(domain.Profile.Domain) == "" || domain.Profile.Digest == "" || domain.LastCheckedAt.IsZero() {
		return delivery.ErrInvalidEmail
	}
	if strings.ToLower(domain.Profile.Domain) != domain.Profile.Domain || strings.ContainsAny(domain.Profile.Domain, "@/\r\n") {
		return delivery.ErrInvalidEmail
	}
	return nil
}

func (s *Store) begin(ctx context.Context) (dbport.Tx, error) {
	if s == nil || ctx == nil {
		return nil, delivery.ErrInvalidEmail
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err := tenancy.WithTenant(ctx, tx, s.tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

var _ delivery.SendingDomainRepository = (*Store)(nil)
var _ delivery.SendingDomainGate = (*Store)(nil)
