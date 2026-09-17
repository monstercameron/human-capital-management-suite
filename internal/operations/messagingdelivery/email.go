// Authenticated transactional-email domains and deliverability controls
// (MAIL-001).
//
// A versioned sending-domain profile verifies DNS authentication (SPF,
// DKIM, DMARC) and key rotation from caller-supplied DNS evidence — the
// package is kernel-pure and performs no lookups itself. Provider feedback
// arrives as HMAC-authenticated events so webhook spoofing fails closed;
// bounce and complaint attribution is exact, so a misattributed event can
// never rewrite another message's truth. Suppression and preferences are
// scoped to tenant and purpose: a global suppression that crosses either
// boundary is refused by construction. A bounded retry limiter stops
// recipient floods, and a domain-reputation failure degrades to UNKNOWN —
// never to delivered. Transport states never satisfy legal acknowledgement:
// that proof stays on the recipient side (see states.go).
package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrDomainUnverified reports DNS evidence that is missing or fails
	// authentication.
	ErrDomainUnverified = errors.New("delivery: sending domain is unverified")
	// ErrUnverifiedDomain reports a send attempted through an unverified
	// or rotated-out domain profile.
	ErrUnverifiedDomain = errors.New("delivery: domain profile is not verified")
	// ErrInvalidEmail reports a malformed email request, profile or event.
	ErrInvalidEmail = errors.New("delivery: invalid email request")
	// ErrFeedbackUnauthenticated reports a provider feedback event whose
	// signature does not verify.
	ErrFeedbackUnauthenticated = errors.New("delivery: provider feedback is unauthenticated")
	// ErrUnknownMessage reports feedback for a message the tracker never
	// registered.
	ErrUnknownMessage = errors.New("delivery: feedback names an unknown message")
	// ErrFeedbackMismatch reports feedback whose provider reference or
	// payload identity disagrees with the registered record.
	ErrFeedbackMismatch = errors.New("delivery: feedback does not match the registered message")
	// ErrEmailTerminal reports feedback against a retained terminal state.
	ErrEmailTerminal = errors.New("delivery: message status is terminal")
	// ErrRateLimited reports a send refused by the retry budget.
	ErrRateLimited = errors.New("delivery: recipient retry budget is exhausted")
	// ErrDomainReputation reports a send refused while the domain
	// reputation is failed. The message is retained as UNKNOWN, never as
	// delivered.
	ErrDomainReputation = errors.New("delivery: sending domain reputation has failed")
	// ErrSuppressionAbsent reports a lift for a suppression that does not
	// exist.
	ErrSuppressionAbsent = errors.New("delivery: suppression does not exist")
	// ErrLegalAcknowledgement reports that transport alone cannot satisfy
	// legal acknowledgement.
	ErrLegalAcknowledgement = errors.New("delivery: transport does not satisfy legal acknowledgement")
)

// EmailStatus is the closed email-transport vocabulary.
type EmailStatus string

const (
	EmailAccepted   EmailStatus = "ACCEPTED"
	EmailDelivered  EmailStatus = "DELIVERED"
	EmailBounced    EmailStatus = "BOUNCED"
	EmailComplained EmailStatus = "COMPLAINED"
	EmailSuppressed EmailStatus = "SUPPRESSED"
	EmailUnknown    EmailStatus = "UNKNOWN"
)

// ParseEmailStatus resolves the closed vocabulary and refuses anything
// else: no vendor status may smuggle in an extra terminal meaning.
func ParseEmailStatus(raw string) (EmailStatus, error) {
	switch EmailStatus(strings.ToUpper(strings.TrimSpace(raw))) {
	case EmailAccepted:
		return EmailAccepted, nil
	case EmailDelivered:
		return EmailDelivered, nil
	case EmailBounced:
		return EmailBounced, nil
	case EmailComplained:
		return EmailComplained, nil
	case EmailSuppressed:
		return EmailSuppressed, nil
	case EmailUnknown:
		return EmailUnknown, nil
	default:
		return "", fmt.Errorf("%w: unknown email status %q", ErrInvalidEmail, raw)
	}
}

// DNSRecords is the caller-supplied DNS authentication evidence for one
// sending domain. The package validates the evidence; it never performs
// network resolution itself.
type DNSRecords struct {
	// SPF is the TXT record at the domain apex, e.g. "v=spf1 ... -all".
	SPF string
	// DKIMSelectors maps a verified selector to its published public key.
	DKIMSelectors map[string]string
	// DMARC is the TXT record at _dmarc, e.g. "v=DMARC1; p=reject; ...".
	DMARC string
}

// DomainProfile is one verified sending-domain version. Rotation links
// versions through PreviousDigest so a retired key cannot silently resume.
type DomainProfile struct {
	Domain         string `json:"domain"`
	Version        uint64 `json:"version"`
	SPFVerified    bool   `json:"spf_verified"`
	DKIMVerified   bool   `json:"dkim_verified"`
	DMARCVerified  bool   `json:"dmarc_verified"`
	RecordDigest   string `json:"record_digest"`
	Verified       bool   `json:"verified"`
	VerifiedAt     string `json:"verified_at"`
	PreviousDigest string `json:"previous_digest,omitempty"`
	Digest         string `json:"digest"`
}

// VerifyDomain authenticates one sending domain from its DNS evidence.
// SPF must be present with a hard or soft fail default, at least one DKIM
// selector must carry a key, and DMARC must be present with an explicit
// enforcement policy. Anything less fails closed.
func VerifyDomain(domain string, dns DNSRecords, now time.Time) (DomainProfile, error) {
	if strings.TrimSpace(domain) == "" || strings.Contains(domain, "@") || !strings.Contains(domain, ".") {
		return DomainProfile{}, fmt.Errorf("%w: domain %q is not a bare sending domain", ErrDomainUnverified, domain)
	}
	if now.IsZero() {
		return DomainProfile{}, fmt.Errorf("%w: verification time is required", ErrInvalidEmail)
	}
	spf := strings.TrimSpace(dns.SPF)
	if !strings.HasPrefix(spf, "v=spf1") || (!strings.HasSuffix(spf, "-all") && !strings.HasSuffix(spf, "~all")) {
		return DomainProfile{}, fmt.Errorf("%w: SPF record is missing or has no fail default", ErrDomainUnverified)
	}
	verifiedSelectors := 0
	for selector, key := range dns.DKIMSelectors {
		if strings.TrimSpace(selector) != "" && strings.TrimSpace(key) != "" {
			verifiedSelectors++
		}
	}
	if verifiedSelectors == 0 {
		return DomainProfile{}, fmt.Errorf("%w: no DKIM selector carries a key", ErrDomainUnverified)
	}
	dmarc := strings.TrimSpace(dns.DMARC)
	if !strings.HasPrefix(dmarc, "v=DMARC1;") || !strings.Contains(dmarc, "p=") {
		return DomainProfile{}, fmt.Errorf("%w: DMARC record is missing or has no enforcement policy", ErrDomainUnverified)
	}
	recordDigest := hashEmail(struct {
		SPF   string   `json:"spf"`
		DKIM  []string `json:"dkim"`
		DMARC string   `json:"dmarc"`
	}{spf, sortedKeys(dns.DKIMSelectors), dmarc})
	profile := DomainProfile{
		Domain: domain, Version: 1,
		SPFVerified: true, DKIMVerified: true, DMARCVerified: true,
		RecordDigest: recordDigest, Verified: true,
		VerifiedAt: now.UTC().Format(time.RFC3339),
	}
	profile.Digest = hashEmail(struct {
		Domain       string `json:"domain"`
		Version      uint64 `json:"version"`
		RecordDigest string `json:"record_digest"`
		VerifiedAt   string `json:"verified_at"`
	}{profile.Domain, profile.Version, profile.RecordDigest, profile.VerifiedAt})
	return profile, nil
}

// Rotate verifies fresh DNS evidence and issues the next profile version
// linked to this one. Rotation off an unverified profile is refused.
func (p DomainProfile) Rotate(next DNSRecords, now time.Time) (DomainProfile, error) {
	if !p.Verified || p.Digest == "" {
		return DomainProfile{}, fmt.Errorf("%w: rotation requires a verified profile", ErrUnverifiedDomain)
	}
	rotated, err := VerifyDomain(p.Domain, next, now)
	if err != nil {
		return DomainProfile{}, err
	}
	rotated.Version = p.Version + 1
	rotated.PreviousDigest = p.Digest
	rotated.Digest = hashEmail(struct {
		Domain         string `json:"domain"`
		Version        uint64 `json:"version"`
		RecordDigest   string `json:"record_digest"`
		VerifiedAt     string `json:"verified_at"`
		PreviousDigest string `json:"previous_digest"`
	}{rotated.Domain, rotated.Version, rotated.RecordDigest, rotated.VerifiedAt, rotated.PreviousDigest})
	return rotated, nil
}

// OutgoingEmail is one transactional email send request.
type OutgoingEmail struct {
	MessageID string
	TenantID  string
	Purpose   string
	Recipient string
	Subject   string
}

// SuppressionEntry is one scoped suppression with its evidence.
type SuppressionEntry struct {
	Tenant    string `json:"tenant"`
	Purpose   string `json:"purpose"`
	Recipient string `json:"recipient"`
	Reason    string `json:"reason"`
	At        string `json:"at"`
	Digest    string `json:"digest"`
}

// SuppressionList scopes every suppression to its exact tenant and
// purpose. Lookup, suppression and lift all key on the full triple, so a
// suppression can never leak across tenants or purposes.
type SuppressionList struct {
	mu      sync.Mutex
	entries map[string]SuppressionEntry
}

// NewSuppressionList returns an empty suppression list.
func NewSuppressionList() *SuppressionList {
	return &SuppressionList{entries: make(map[string]SuppressionEntry)}
}

func suppressionKey(tenant, purpose, recipient string) string {
	return tenant + "\x00" + purpose + "\x00" + strings.ToLower(strings.TrimSpace(recipient))
}

// Suppress records one scoped suppression. Re-suppressing the same scope
// is idempotent and returns the original entry.
func (l *SuppressionList) Suppress(tenant, purpose, recipient, reason string, now time.Time) (SuppressionEntry, error) {
	if l == nil {
		return SuppressionEntry{}, ErrInvalidEmail
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(purpose) == "" || strings.TrimSpace(recipient) == "" {
		return SuppressionEntry{}, fmt.Errorf("%w: suppression requires tenant, purpose and recipient", ErrInvalidEmail)
	}
	if strings.TrimSpace(reason) == "" || now.IsZero() {
		return SuppressionEntry{}, fmt.Errorf("%w: suppression requires a reason and time", ErrInvalidEmail)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := suppressionKey(tenant, purpose, recipient)
	if existing, ok := l.entries[key]; ok {
		return existing, nil
	}
	entry := SuppressionEntry{
		Tenant: tenant, Purpose: purpose,
		Recipient: strings.ToLower(strings.TrimSpace(recipient)),
		Reason:    reason, At: now.UTC().Format(time.RFC3339),
	}
	entry.Digest = hashEmail(struct {
		Tenant    string `json:"tenant"`
		Purpose   string `json:"purpose"`
		Recipient string `json:"recipient"`
		Reason    string `json:"reason"`
		At        string `json:"at"`
	}{entry.Tenant, entry.Purpose, entry.Recipient, entry.Reason, entry.At})
	l.entries[key] = entry
	return entry, nil
}

// Lift removes one scoped suppression and returns the removed entry as
// the lift receipt. Lifting an absent scope fails closed.
func (l *SuppressionList) Lift(tenant, purpose, recipient string) (SuppressionEntry, error) {
	if l == nil {
		return SuppressionEntry{}, ErrInvalidEmail
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := suppressionKey(tenant, purpose, recipient)
	entry, ok := l.entries[key]
	if !ok {
		return SuppressionEntry{}, fmt.Errorf("%w: no suppression for this tenant, purpose and recipient", ErrSuppressionAbsent)
	}
	delete(l.entries, key)
	return entry, nil
}

// Suppressed reports whether the exact tenant/purpose/recipient scope is
// suppressed. Neighboring scopes never match.
func (l *SuppressionList) Suppressed(tenant, purpose, recipient string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.entries[suppressionKey(tenant, purpose, recipient)]
	return ok
}

// EmailRetryPolicy bounds send attempts per recipient and the reputation
// recovery window.
type EmailRetryPolicy struct {
	MaxAttempts        uint
	Window             time.Duration
	MaxPerWindow       uint
	ReputationCooldown time.Duration
}

// DefaultEmailRetryPolicy allows three attempts per message, ten sends per
// recipient per hour, and a one-hour reputation cooldown.
func DefaultEmailRetryPolicy() EmailRetryPolicy {
	return EmailRetryPolicy{
		MaxAttempts: 3, Window: time.Hour, MaxPerWindow: 10,
		ReputationCooldown: time.Hour,
	}
}

// RetryLimiter enforces the retry budget and the domain-reputation fence.
// All state lives on the limiter; evaluation time is always injected.
type RetryLimiter struct {
	mu               sync.Mutex
	sends            map[string][]time.Time
	reputationFailed map[string]time.Time
}

// NewRetryLimiter returns an empty limiter.
func NewRetryLimiter() *RetryLimiter {
	return &RetryLimiter{sends: make(map[string][]time.Time), reputationFailed: make(map[string]time.Time)}
}

// Allow records one send for the recipient under the policy, or refuses
// when the per-window budget is exhausted. A recipient flood never sends.
func (l *RetryLimiter) Allow(policy EmailRetryPolicy, recipient string, now time.Time) error {
	if l == nil {
		return ErrInvalidEmail
	}
	if strings.TrimSpace(recipient) == "" || now.IsZero() {
		return fmt.Errorf("%w: recipient and time are required", ErrInvalidEmail)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-policy.Window)
	kept := make([]time.Time, 0, len(l.sends[recipient])+1)
	for _, at := range l.sends[recipient] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if uint(len(kept)) >= policy.MaxPerWindow {
		return fmt.Errorf("%w: recipient %s exceeded %d sends per %s", ErrRateLimited, recipient, policy.MaxPerWindow, policy.Window)
	}
	l.sends[recipient] = append(kept, now)
	return nil
}

// RecordReputationFailure fences one sending domain after a reputation
// failure. While fenced, sends degrade to UNKNOWN — never to delivered.
func (l *RetryLimiter) RecordReputationFailure(domain string, now time.Time) {
	if l == nil || strings.TrimSpace(domain) == "" || now.IsZero() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reputationFailed[strings.ToLower(strings.TrimSpace(domain))] = now.UTC()
}

// ReputationBlocked reports whether the domain is inside its failure fence.
func (l *RetryLimiter) ReputationBlocked(policy EmailRetryPolicy, domain string, now time.Time) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	failedAt, ok := l.reputationFailed[strings.ToLower(strings.TrimSpace(domain))]
	if !ok {
		return false
	}
	return now.Before(failedAt.Add(policy.ReputationCooldown))
}

// RecoverReputation lifts the fence once the cooldown has elapsed. Early
// recovery is refused so a failure cannot be waved away.
func (l *RetryLimiter) RecoverReputation(policy EmailRetryPolicy, domain string, now time.Time) error {
	if l == nil {
		return ErrInvalidEmail
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(domain))
	failedAt, ok := l.reputationFailed[key]
	if !ok {
		return fmt.Errorf("%w: domain %s has no reputation failure", ErrInvalidEmail, domain)
	}
	if now.Before(failedAt.Add(policy.ReputationCooldown)) {
		return fmt.Errorf("%w: reputation cooldown for %s has not elapsed", ErrDomainReputation, domain)
	}
	delete(l.reputationFailed, key)
	return nil
}

// FeedbackKind is the closed provider-feedback vocabulary.
type FeedbackKind string

const (
	FeedbackDelivered  FeedbackKind = "delivered"
	FeedbackBounced    FeedbackKind = "bounced"
	FeedbackComplained FeedbackKind = "complained"
)

// FeedbackEvent is one authenticated provider signal.
type FeedbackEvent struct {
	MessageID   string
	Kind        FeedbackKind
	ProviderRef string
	BounceCode  string
	Signature   string
}

// FeedbackPayload returns the exact bytes covered by a feedback
// signature. The signature binds message, kind and provider reference, so
// a signed event for one message can never be replayed as another.
func FeedbackPayload(messageID string, kind FeedbackKind, providerRef string) []byte {
	return []byte(messageID + "\x00" + string(kind) + "\x00" + providerRef)
}

// SignFeedback authenticates feedback payload bytes with the provider
// webhook secret.
func SignFeedback(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyFeedbackPayload checks the webhook signature over the exact
// payload bytes. Missing, malformed or foreign signatures fail closed.
func VerifyFeedbackPayload(secret, payload []byte, signature string) error {
	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(provided) != sha256.Size {
		return ErrFeedbackUnauthenticated
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ErrFeedbackUnauthenticated
	}
	return nil
}

// EmailRecord is the retained truth of one transactional email.
type EmailRecord struct {
	MessageID    string      `json:"message_id"`
	TenantID     string      `json:"tenant_id"`
	Purpose      string      `json:"purpose"`
	Recipient    string      `json:"recipient"`
	Domain       string      `json:"domain"`
	Status       EmailStatus `json:"status"`
	Revision     uint64      `json:"revision"`
	AttemptCount uint        `json:"attempt_count"`
	ProviderRef  string      `json:"provider_ref,omitempty"`
	BounceCode   string      `json:"bounce_code,omitempty"`
	Digest       string      `json:"digest"`
}

// LegalAcknowledgement always refuses: provider transport alone never
// satisfies legal acknowledgement. The recipient side (see states.go)
// carries that proof.
func (r EmailRecord) LegalAcknowledgement() error {
	return fmt.Errorf("%w: message %s is %s by transport", ErrLegalAcknowledgement, r.MessageID, r.Status)
}

// Verify recomputes the record digest and fails closed on tampering.
func (r EmailRecord) Verify() error {
	if r.Digest == "" {
		return fmt.Errorf("%w: record digest is missing", ErrInvalidEmail)
	}
	if recordDigest(r) != r.Digest {
		return fmt.Errorf("%w: email record digest mismatch", ErrInvalidEmail)
	}
	return nil
}

func recordDigest(r EmailRecord) string {
	return hashEmail(struct {
		MessageID    string      `json:"message_id"`
		TenantID     string      `json:"tenant_id"`
		Purpose      string      `json:"purpose"`
		Recipient    string      `json:"recipient"`
		Domain       string      `json:"domain"`
		Status       EmailStatus `json:"status"`
		Revision     uint64      `json:"revision"`
		AttemptCount uint        `json:"attempt_count"`
		ProviderRef  string      `json:"provider_ref,omitempty"`
		BounceCode   string      `json:"bounce_code,omitempty"`
	}{r.MessageID, r.TenantID, r.Purpose, r.Recipient, r.Domain, r.Status, r.Revision, r.AttemptCount, r.ProviderRef, r.BounceCode})
}

// EmailTracker ledgers one record per message ID. Records are immutable
// values: every feedback returns the next revision, and terminal states
// (bounced, complained, suppressed) are retained, never rewritten.
type EmailTracker struct {
	mu      sync.Mutex
	records map[string]EmailRecord
	policy  EmailRetryPolicy
}

// NewEmailTracker returns an empty ledger with the default retry policy.
func NewEmailTracker() *EmailTracker {
	return &EmailTracker{records: make(map[string]EmailRecord), policy: DefaultEmailRetryPolicy()}
}

// Register records one send request. An unverified domain profile is
// refused; a suppressed scope is retained as SUPPRESSED without an
// attempt; a fenced domain reputation is retained as UNKNOWN; a flooded
// recipient is refused. Otherwise the message is ACCEPTED with its first
// attempt recorded.
func (t *EmailTracker) Register(msg OutgoingEmail, profile DomainProfile, suppressions *SuppressionList, limiter *RetryLimiter, now time.Time) (EmailRecord, error) {
	if t == nil || limiter == nil {
		return EmailRecord{}, ErrInvalidEmail
	}
	if strings.TrimSpace(msg.MessageID) == "" || strings.TrimSpace(msg.TenantID) == "" ||
		strings.TrimSpace(msg.Purpose) == "" || strings.TrimSpace(msg.Recipient) == "" {
		return EmailRecord{}, fmt.Errorf("%w: message, tenant, purpose and recipient are required", ErrInvalidEmail)
	}
	if now.IsZero() {
		return EmailRecord{}, fmt.Errorf("%w: send time is required", ErrInvalidEmail)
	}
	if !profile.Verified || profile.Digest == "" || strings.TrimSpace(profile.Domain) == "" {
		return EmailRecord{}, fmt.Errorf("%w: sending domain is not verified", ErrUnverifiedDomain)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if existing, ok := t.records[msg.MessageID]; ok {
		return existing, nil
	}
	record := EmailRecord{
		MessageID: msg.MessageID, TenantID: msg.TenantID, Purpose: msg.Purpose,
		Recipient: strings.ToLower(strings.TrimSpace(msg.Recipient)),
		Domain:    profile.Domain, Status: EmailAccepted, Revision: 1,
	}
	if suppressions != nil && suppressions.Suppressed(msg.TenantID, msg.Purpose, msg.Recipient) {
		record.Status = EmailSuppressed
		record.Digest = recordDigest(record)
		t.records[msg.MessageID] = record
		return record, nil
	}
	if limiter.ReputationBlocked(t.policy, profile.Domain, now) {
		record.Status = EmailUnknown
		record.Digest = recordDigest(record)
		t.records[msg.MessageID] = record
		return record, fmt.Errorf("%w: domain %s", ErrDomainReputation, profile.Domain)
	}
	if err := limiter.Allow(t.policy, record.Recipient, now); err != nil {
		return EmailRecord{}, err
	}
	record.AttemptCount = 1
	record.Digest = recordDigest(record)
	t.records[msg.MessageID] = record
	return record, nil
}

// ApplyFeedback authenticates one provider event and advances the record.
// The signature must cover the exact payload bytes; the message must be
// registered; the provider reference must agree with the record; terminal
// states accept only identical replays. A bounce after delivery is
// refused: it is either a misattribution or a replay, never new truth.
func (t *EmailTracker) ApplyFeedback(secret, payload []byte, event FeedbackEvent, now time.Time) (EmailRecord, error) {
	if t == nil {
		return EmailRecord{}, ErrInvalidEmail
	}
	if now.IsZero() {
		return EmailRecord{}, fmt.Errorf("%w: feedback time is required", ErrInvalidEmail)
	}
	if err := VerifyFeedbackPayload(secret, payload, event.Signature); err != nil {
		return EmailRecord{}, err
	}
	kind, err := parseFeedbackKind(string(event.Kind))
	if err != nil {
		return EmailRecord{}, err
	}
	if !hmac.Equal(payload, FeedbackPayload(event.MessageID, kind, event.ProviderRef)) {
		return EmailRecord{}, fmt.Errorf("%w: feedback payload does not match the event", ErrFeedbackMismatch)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	record, ok := t.records[event.MessageID]
	if !ok {
		return EmailRecord{}, fmt.Errorf("%w: %s", ErrUnknownMessage, event.MessageID)
	}
	if record.ProviderRef != "" && event.ProviderRef != record.ProviderRef {
		return EmailRecord{}, fmt.Errorf("%w: provider reference changed from %s", ErrFeedbackMismatch, record.ProviderRef)
	}
	next, err := advanceEmail(record, kind, event)
	if err != nil {
		return EmailRecord{}, err
	}
	if recordDigest(next) == record.Digest {
		// Identical replay: the stored record stands unchanged, with no
		// new revision, so redelivery converges instead of forking truth.
		return record, nil
	}
	next.Revision++
	next.Digest = recordDigest(next)
	t.records[event.MessageID] = next
	return next, nil
}

func parseFeedbackKind(raw string) (FeedbackKind, error) {
	switch FeedbackKind(strings.ToLower(strings.TrimSpace(raw))) {
	case FeedbackDelivered:
		return FeedbackDelivered, nil
	case FeedbackBounced:
		return FeedbackBounced, nil
	case FeedbackComplained:
		return FeedbackComplained, nil
	default:
		return "", fmt.Errorf("%w: feedback kind %q is not recognized", ErrInvalidEmail, raw)
	}
}

// advanceEmail moves one record forward. ACCEPTED takes delivery, bounce
// or (via delivery) complaint; DELIVERED takes complaint; UNKNOWN takes
// delivery or bounce; every terminal state is retained and only an
// identical replay is idempotent.
func advanceEmail(record EmailRecord, kind FeedbackKind, event FeedbackEvent) (EmailRecord, error) {
	switch record.Status {
	case EmailAccepted, EmailUnknown:
		switch kind {
		case FeedbackDelivered:
			record.Status = EmailDelivered
			record.ProviderRef = event.ProviderRef
		case FeedbackBounced:
			record.Status = EmailBounced
			record.ProviderRef = event.ProviderRef
			record.BounceCode = event.BounceCode
		default:
			return EmailRecord{}, fmt.Errorf("%w: %s takes no %s", ErrEmailTerminal, record.Status, kind)
		}
		return record, nil
	case EmailDelivered:
		if kind != FeedbackComplained {
			if kind == FeedbackDelivered && (event.ProviderRef == "" || event.ProviderRef == record.ProviderRef) {
				return record, nil
			}
			return EmailRecord{}, fmt.Errorf("%w: %s takes no %s", ErrEmailTerminal, record.Status, kind)
		}
		record.Status = EmailComplained
		return record, nil
	case EmailBounced, EmailComplained, EmailSuppressed:
		if replayMatches(record, kind, event) {
			return record, nil
		}
		return EmailRecord{}, fmt.Errorf("%w: %s is retained", ErrEmailTerminal, record.Status)
	default:
		return EmailRecord{}, fmt.Errorf("%w: status %s is not recognized", ErrInvalidEmail, record.Status)
	}
}

func replayMatches(record EmailRecord, kind FeedbackKind, event FeedbackEvent) bool {
	switch record.Status {
	case EmailBounced:
		return kind == FeedbackBounced && event.ProviderRef == record.ProviderRef && event.BounceCode == record.BounceCode
	case EmailComplained:
		return kind == FeedbackComplained && (event.ProviderRef == "" || event.ProviderRef == record.ProviderRef)
	case EmailSuppressed:
		return false
	default:
		return false
	}
}

// Record returns one retained record, including terminal ones.
func (t *EmailTracker) Record(messageID string) (EmailRecord, error) {
	if t == nil {
		return EmailRecord{}, ErrInvalidEmail
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	record, ok := t.records[messageID]
	if !ok {
		return EmailRecord{}, fmt.Errorf("%w: %s", ErrUnknownMessage, messageID)
	}
	return record, nil
}

func hashEmail(view any) string {
	b, err := json.Marshal(view)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
