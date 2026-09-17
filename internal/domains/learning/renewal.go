// LEARN-006: expire and renew credentials. Trusted-time expiry emits
// each warning exactly once; renewal mints a new credential version with
// new evidence and never extends the old credential.
package learning

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidRenewal  = errors.New("learning: invalid renewal request")
	ErrCredentialSpent = errors.New("learning: credential version is spent")
	ErrNotExpired      = errors.New("learning: credential has not expired")
)

// RenewalRequest is one credential-renewal request.
type RenewalRequest struct {
	CredentialID   string
	NewEvidenceRef string
	At             time.Time
}

// ValidateRenewalRequest checks a renewal request without applying it.
func ValidateRenewalRequest(req RenewalRequest) error {
	if strings.TrimSpace(req.CredentialID) == "" || strings.TrimSpace(req.NewEvidenceRef) == "" {
		return fmt.Errorf("%w: credential and new evidence are required", ErrInvalidRenewal)
	}
	if req.At.IsZero() {
		return fmt.Errorf("%w: instant is required", ErrInvalidRenewal)
	}
	return nil
}

// ExpireCredentials expires every credential past its date as of the
// trusted instant, warning once per credential. Repeat passes emit
// nothing new.
func (r *Registry) ExpireCredentials(caller Caller, now time.Time) ([]Credential, error) {
	if now.IsZero() {
		return nil, errors.New("learning: trusted instant is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, "tenant-acme"); err != nil {
		return nil, err
	}
	expired := []Credential{}
	for _, cred := range r.credentials {
		if cred.Tenant != "tenant-acme" {
			continue
		}
		if now.Before(cred.ExpiresAt) {
			continue
		}
		if r.warned[cred.ID] {
			continue
		}
		r.warned[cred.ID] = true
		expired = append(expired, cred)
		r.journal = append(r.journal, JournalEntry{
			Op: "warn-expiry", Ref: cred.ID,
			Detail: fmt.Sprintf("learner=%s expired=%s", cred.LearnerID, cred.ExpiresAt.UTC().Format(time.RFC3339)),
		})
	}
	return expired, nil
}

// Renew mints the next credential version for one expired credential.
// The old record is never extended; a spent version renews only once.
func (r *Registry) Renew(caller Caller, req RenewalRequest) (Credential, error) {
	if err := ValidateRenewalRequest(req); err != nil {
		return Credential{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.credentials[req.CredentialID]
	if !ok {
		return Credential{}, fmt.Errorf("%w: %s", ErrUnknownCourse, req.CredentialID)
	}
	if err := r.authorize(caller, old.Tenant); err != nil {
		return Credential{}, err
	}
	if req.At.Before(old.ExpiresAt) {
		return Credential{}, fmt.Errorf("%w: %s", ErrNotExpired, req.CredentialID)
	}
	if r.renewed[req.CredentialID] {
		return Credential{}, fmt.Errorf("%w: %s", ErrCredentialSpent, req.CredentialID)
	}
	days := 730
	if v, ok := r.courses.LoadVersion(old.CourseID, old.Version); ok {
		days = expiryDays(v.ExpiryPolicy)
	}
	next := Credential{
		ID:        fmt.Sprintf("%s#r%d", old.ID, old.CredentialVersion+1),
		LearnerID: old.LearnerID, CourseID: old.CourseID, Version: old.Version,
		Tenant: old.Tenant, Issuer: old.Issuer, Scope: old.Scope,
		IssuedAt:          req.At.UTC(),
		ExpiresAt:         req.At.UTC().AddDate(0, 0, days),
		EvidenceRef:       req.NewEvidenceRef,
		CredentialVersion: old.CredentialVersion + 1, RenewalOf: old.ID,
	}
	digest, err := credentialDigest(next)
	if err != nil {
		return Credential{}, err
	}
	next.Digest = digest
	r.credentials[next.ID] = next
	r.renewed[req.CredentialID] = true
	r.journal = append(r.journal, JournalEntry{
		Op: "renew-credential", Ref: next.ID, Detail: "renewal_of=" + old.ID,
	})
	return next, nil
}
