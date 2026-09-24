package delivery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var ErrDNSLookup = errors.New("delivery: sending-domain DNS lookup failed")

// ActiveSendingDomain is the persisted, tenant-owned profile selected for
// transactional sends. The configured selectors are retained in Profile so
// scheduled checks query the same DKIM keys that were originally verified.
type ActiveSendingDomain struct {
	TenantID      string
	Profile       DomainProfile
	Owner         string
	LastCheckedAt time.Time
	AlertCycle    string
}

// SendingDomainRepository persists active profiles and their verification
// status. Implementations must scope every operation to TenantID.
type SendingDomainRepository interface {
	ListActive(context.Context) ([]ActiveSendingDomain, error)
	BeginCheck(context.Context, ActiveSendingDomain, time.Time) error
	SaveProfile(context.Context, ActiveSendingDomain) error
}

// DNSRecordResolver supplies live DNS evidence for a profile.
type DNSRecordResolver interface {
	Resolve(context.Context, DomainProfile) (DNSRecords, error)
}

// DomainAlertOwner creates or resolves the one alert owned by a domain
// profile. Implementations must be idempotent for tenant and domain.
type DomainAlertOwner interface {
	Raise(context.Context, ActiveSendingDomain, string, time.Time) error
	Resolve(context.Context, ActiveSendingDomain, time.Time) error
}

// DomainReverifier performs one scheduled pass over active sending domains.
// DNS and persistence are ports; all SPF/DKIM/DMARC policy remains in
// VerifyDomain/Rotate.
type DomainReverifier struct {
	Profiles SendingDomainRepository
	DNS      DNSRecordResolver
	Alerts   DomainAlertOwner
	Now      func() time.Time
}

func (r DomainReverifier) RunOnce(ctx context.Context) error {
	if ctx == nil || r.Profiles == nil || r.DNS == nil || r.Alerts == nil || r.Now == nil {
		return ErrInvalidEmail
	}
	domains, err := r.Profiles.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("delivery: list active sending domains: %w", err)
	}
	var failures []error
	for _, domain := range domains {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if strings.TrimSpace(domain.TenantID) == "" || strings.TrimSpace(domain.Profile.Domain) == "" || strings.TrimSpace(domain.Owner) == "" {
			failures = append(failures, fmt.Errorf("%w: active domain profile is incomplete", ErrInvalidEmail))
			continue
		}
		now := r.Now().UTC()
		if now.IsZero() {
			failures = append(failures, fmt.Errorf("%w: verification time is required", ErrInvalidEmail))
			continue
		}
		// Keep the verified profile as the comparison/rotation base. BeginCheck
		// revokes the persisted send permit, but that temporary revocation must
		// not erase the verified state needed by DomainProfile.Rotate.
		verifiedProfile := domain.Profile
		domain.LastCheckedAt = now
		// Revoke the previously persisted send permit before network I/O. That
		// makes a later DNS, profile-write, or alert-write failure visible to
		// every sender instance that reads the shared profile row.
		if domain.AlertCycle == "" {
			domain.AlertCycle = now.Format(time.RFC3339Nano)
		}
		domain.Profile.Verified = false
		domain.Profile.SPFVerified = false
		domain.Profile.DKIMVerified = false
		domain.Profile.DMARCVerified = false
		if err := r.Profiles.BeginCheck(ctx, domain, now); err != nil {
			failures = append(failures, fmt.Errorf("delivery: revoke send permit for %s: %w", domain.Profile.Domain, err))
			failures = append(failures, fmt.Errorf("%w: %s", ErrDomainUnverified, domain.Profile.Domain))
			if alertErr := r.Alerts.Raise(ctx, domain, "DNS authentication verification could not be started safely", now); alertErr != nil {
				failures = append(failures, fmt.Errorf("delivery: raise domain alert %s: %w", domain.Profile.Domain, alertErr))
			}
			continue
		}
		dns, resolveErr := r.DNS.Resolve(ctx, domain.Profile)
		if resolveErr != nil {
			resolveErr = fmt.Errorf("%w: %s", ErrDNSLookup, domain.Profile.Domain)
		} else {
			var next DomainProfile
			next, resolveErr = reverifiedProfile(verifiedProfile, dns, now)
			if resolveErr == nil {
				domain.Profile = next
				domain.Profile.Verified = true
				if err := r.Profiles.SaveProfile(ctx, domain); err != nil {
					failures = append(failures, fmt.Errorf("delivery: save verified profile %s: %w", next.Domain, err))
					continue
				}
				if domain.AlertCycle != "" {
					if err := r.Alerts.Resolve(ctx, domain, now); err != nil {
						failures = append(failures, fmt.Errorf("delivery: resolve domain alert %s: %w", next.Domain, err))
						continue
					}
					domain.AlertCycle = ""
					if err := r.Profiles.SaveProfile(ctx, domain); err != nil {
						failures = append(failures, fmt.Errorf("delivery: clear domain alert cycle %s: %w", next.Domain, err))
					}
				}
				continue
			}
		}
		if err := r.Profiles.SaveProfile(ctx, domain); err != nil {
			failures = append(failures, fmt.Errorf("delivery: persist unverified profile %s: %w", domain.Profile.Domain, err))
		}
		if err := r.Alerts.Raise(ctx, domain, resolveErr.Error(), now); err != nil {
			failures = append(failures, fmt.Errorf("delivery: raise domain alert %s: %w", domain.Profile.Domain, err))
		}
		failures = append(failures, fmt.Errorf("%w: %s", ErrDomainUnverified, domain.Profile.Domain))
	}
	return errors.Join(failures...)
}

func reverifiedProfile(current DomainProfile, dns DNSRecords, now time.Time) (DomainProfile, error) {
	observed, err := VerifyDomain(current.Domain, dns, now)
	if err != nil {
		return DomainProfile{}, err
	}
	if current.RecordDigest == observed.RecordDigest {
		current.Verified = true
		current.SPFVerified = true
		current.DKIMVerified = true
		current.DMARCVerified = true
		current.DKIMSelectors = append([]string(nil), observed.DKIMSelectors...)
		return current, nil
	}
	if current.Verified {
		return current.Rotate(dns, now)
	}
	// Recovery from a failed check still links the new authenticated profile
	// to the last retained version while keeping VerifyDomain authoritative.
	observed.Version = current.Version + 1
	observed.PreviousDigest = current.Digest
	observed.Digest = domainProfileDigest(observed)
	return observed, nil
}

func domainProfileDigest(p DomainProfile) string {
	if p.PreviousDigest == "" {
		return hashEmail(struct {
			Domain       string `json:"domain"`
			Version      uint64 `json:"version"`
			RecordDigest string `json:"record_digest"`
			VerifiedAt   string `json:"verified_at"`
		}{p.Domain, p.Version, p.RecordDigest, p.VerifiedAt})
	}
	return hashEmail(struct {
		Domain         string `json:"domain"`
		Version        uint64 `json:"version"`
		RecordDigest   string `json:"record_digest"`
		VerifiedAt     string `json:"verified_at"`
		PreviousDigest string `json:"previous_digest"`
	}{p.Domain, p.Version, p.RecordDigest, p.VerifiedAt, p.PreviousDigest})
}

// NetDNSResolver reads SPF and DMARC TXT records and each retained DKIM
// selector. TXT chunks are joined as required by DNS TXT semantics.
type TXTResolver interface {
	LookupTXT(context.Context, string) ([]string, error)
}

type NetDNSResolver struct{ Resolver TXTResolver }

func (r NetDNSResolver) Resolve(ctx context.Context, profile DomainProfile) (DNSRecords, error) {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	lookup := func(name, prefix string) (string, error) {
		records, err := resolver.LookupTXT(ctx, name)
		if err != nil {
			return "", err
		}
		var matches []string
		for _, record := range records {
			record = strings.TrimSpace(record)
			if strings.HasPrefix(strings.ToLower(record), strings.ToLower(prefix)) {
				matches = append(matches, record)
			}
		}
		if len(matches) != 1 {
			return "", fmt.Errorf("expected exactly one %s TXT record", prefix)
		}
		return matches[0], nil
	}
	spf, err := lookup(profile.Domain, "v=spf1")
	if err != nil {
		return DNSRecords{}, fmt.Errorf("%w: SPF", err)
	}
	dmarc, err := lookup("_dmarc."+profile.Domain, "v=DMARC1;")
	if err != nil {
		return DNSRecords{}, fmt.Errorf("%w: DMARC", err)
	}
	dkim := make(map[string]string, len(profile.DKIMSelectors))
	for _, selector := range profile.DKIMSelectors {
		selector = strings.TrimSpace(selector)
		if selector == "" || strings.ContainsAny(selector, ".\r\n/ \"") {
			return DNSRecords{}, ErrInvalidEmail
		}
		name := selector + "._domainkey." + profile.Domain
		records, err := resolver.LookupTXT(ctx, name)
		if err != nil {
			return DNSRecords{}, fmt.Errorf("%w: DKIM selector %s", err, selector)
		}
		if len(records) != 1 {
			return DNSRecords{}, fmt.Errorf("delivery: DKIM selector %s must publish exactly one TXT record", selector)
		}
		key, err := parseDKIMTXT(records[0])
		if err != nil {
			return DNSRecords{}, fmt.Errorf("delivery: DKIM selector %s: %w", selector, err)
		}
		dkim[selector] = key
	}
	return DNSRecords{SPF: spf, DKIMSelectors: dkim, DMARC: dmarc}, nil
}

func parseDKIMTXT(raw string) (string, error) {
	record := strings.TrimSpace(raw)
	if record == "" {
		return "", ErrDomainUnverified
	}
	tags := make(map[string]string)
	for i, part := range strings.Split(record, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return "", ErrDomainUnverified
		}
		if _, exists := tags[key]; exists {
			return "", ErrDomainUnverified
		}
		if key == "v" && (i != 0 || value != "DKIM1") {
			return "", ErrDomainUnverified
		}
		tags[key] = value
	}
	if key, ok := tags["p"]; !ok || key == "" {
		return "", ErrDomainUnverified
	}
	return record, nil
}
