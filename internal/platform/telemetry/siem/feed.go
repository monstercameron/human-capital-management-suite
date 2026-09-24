// Package siem bridges explicitly attributed security evidence into a
// tenant-scoped, signed and resumable feed. It is an in-memory adapter seam;
// production event producers and durable delivery remain composition work.
package siem

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

const SchemaVersion = 1

type EventType string

const (
	EventAlertRule EventType = "SECURITY_ALERT_RULE"
	EventDLP       EventType = "DLP_SIGNAL"
	EventAccess    EventType = "ACCESS_SIGNAL"
	EventAdmin     EventType = "ADMIN_ACTION"
)

var (
	ErrInvalidEvent  = errors.New("siem: invalid security event")
	ErrInvalidCursor = errors.New("siem: cursor does not match tenant feed")
	ErrInvalidFeed   = errors.New("siem: invalid signed feed")
)

// EventInput is supplied by a trusted producer after it has established the
// tenant association. It carries references and digests only, never payloads.
type EventInput struct {
	Tenant         string
	Type           EventType
	OccurredAt     time.Time
	SourceRef      string
	EvidenceDigest string
	RuleID         string
	RuleVersion    int
}

type Event struct {
	Sequence       uint64    `json:"sequence"`
	Tenant         string    `json:"tenant"`
	Type           EventType `json:"type"`
	OccurredAt     time.Time `json:"occurred_at"`
	SourceRef      string    `json:"source_ref"`
	EvidenceDigest string    `json:"evidence_digest"`
	RuleID         string    `json:"rule_id,omitempty"`
	RuleVersion    int       `json:"rule_version,omitempty"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	Digest         string    `json:"digest"`
}

func validType(kind EventType) bool {
	switch kind {
	case EventAlertRule, EventDLP, EventAccess, EventAdmin:
		return true
	default:
		return false
	}
}

func (in EventInput) validate() error {
	if strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.Tenant) != in.Tenant ||
		!validType(in.Type) || in.OccurredAt.IsZero() ||
		strings.TrimSpace(in.SourceRef) == "" || strings.TrimSpace(in.SourceRef) != in.SourceRef ||
		!validDigest(in.EvidenceDigest) {
		return ErrInvalidEvent
	}
	if in.Type == EventAlertRule {
		if strings.TrimSpace(in.RuleID) == "" || strings.TrimSpace(in.RuleID) != in.RuleID || in.RuleVersion <= 0 {
			return ErrInvalidEvent
		}
	} else if in.RuleID != "" || in.RuleVersion != 0 {
		return ErrInvalidEvent
	}
	return nil
}

func (e Event) validate() bool {
	input := EventInput{Tenant: e.Tenant, Type: e.Type, OccurredAt: e.OccurredAt, SourceRef: e.SourceRef,
		EvidenceDigest: e.EvidenceDigest, RuleID: e.RuleID, RuleVersion: e.RuleVersion}
	return e.Sequence > 0 && input.validate() == nil && e.Digest == eventDigest(e)
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func canonical(e Event) string {
	return fmt.Sprintf("siem/v%d|%d|%s|%s|%s|%s|%s|%s|%d|%s", SchemaVersion,
		e.Sequence, e.Tenant, e.Type, e.OccurredAt.UTC().Format(time.RFC3339Nano),
		e.SourceRef, e.EvidenceDigest, e.RuleID, e.RuleVersion, e.PreviousDigest)
}

func eventDigest(e Event) string {
	sum := sha256.Sum256([]byte(canonical(e)))
	return hex.EncodeToString(sum[:])
}

type Cursor struct {
	Tenant   string `json:"tenant"`
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest,omitempty"`
}

type Feed struct {
	SchemaVersion int                         `json:"schema_version"`
	Tenant        string                      `json:"tenant"`
	From          Cursor                      `json:"from"`
	Next          Cursor                      `json:"next"`
	Events        []Event                     `json:"events"`
	Digest        string                      `json:"digest"`
	Signature     subscription.SignedDelivery `json:"signature"`
}

func (f Feed) signingDigest() string {
	var b strings.Builder
	fmt.Fprintf(&b, "siem-feed/v%d|%s|%d|%s|%d|%s|%d", f.SchemaVersion,
		f.Tenant, f.From.Sequence, f.From.Digest, f.Next.Sequence, f.Next.Digest, len(f.Events))
	for _, event := range f.Events {
		b.WriteByte('|')
		b.WriteString(event.Digest)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

type Stream struct {
	mu       sync.RWMutex
	byTenant map[string][]Event
}

func NewStream() *Stream { return &Stream{byTenant: make(map[string][]Event)} }

// Append assigns the next sequence within this tenant and links it to the
// preceding event. The caller must authenticate the tenant association.
func (s *Stream) Append(input EventInput) (Event, error) {
	if s == nil || input.validate() != nil {
		return Event{}, ErrInvalidEvent
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.byTenant[input.Tenant]
	event := Event{Sequence: uint64(len(items) + 1), Tenant: input.Tenant, Type: input.Type,
		OccurredAt: input.OccurredAt.UTC(), SourceRef: input.SourceRef, EvidenceDigest: input.EvidenceDigest,
		RuleID: input.RuleID, RuleVersion: input.RuleVersion}
	if len(items) > 0 {
		event.PreviousDigest = items[len(items)-1].Digest
	}
	event.Digest = eventDigest(event)
	s.byTenant[input.Tenant] = append(items, event)
	return event, nil
}

// Read returns a signed page after cursor. Cursor tenant and anchor digest
// must match this feed, preventing cross-tenant continuation and cursor forgery.
func (s *Stream) Read(tenant string, cursor Cursor, limit int, at time.Time, signer *subscription.CredentialRing) (Feed, error) {
	if s == nil || signer == nil || strings.TrimSpace(tenant) == "" || tenant != strings.TrimSpace(tenant) ||
		cursor.Tenant != tenant || limit <= 0 || limit > 1000 || at.IsZero() {
		return Feed{}, ErrInvalidCursor
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.byTenant[tenant]
	if cursor.Sequence > uint64(len(items)) {
		return Feed{}, ErrInvalidCursor
	}
	if cursor.Sequence == 0 {
		if cursor.Digest != "" {
			return Feed{}, ErrInvalidCursor
		}
	} else if items[cursor.Sequence-1].Digest != cursor.Digest {
		return Feed{}, ErrInvalidCursor
	}
	end := int(cursor.Sequence) + limit
	if end > len(items) {
		end = len(items)
	}
	page := append([]Event(nil), items[int(cursor.Sequence):end]...)
	return SignPage(tenant, cursor, page, at, signer)
}

// SignPage validates and signs one already-authorized durable page. It is
// shared by the in-memory stream and PostgreSQL pull adapter so both expose
// identical canonical cursor and signature semantics.
func SignPage(tenant string, cursor Cursor, events []Event, at time.Time, signer *subscription.CredentialRing) (Feed, error) {
	if signer == nil || strings.TrimSpace(tenant) == "" || tenant != strings.TrimSpace(tenant) ||
		cursor.Tenant != tenant || at.IsZero() || len(events) > 1000 || cursor.Sequence == 0 && cursor.Digest != "" ||
		cursor.Sequence > 0 && !validDigest(cursor.Digest) {
		return Feed{}, ErrInvalidCursor
	}
	sequence, previous := cursor.Sequence, cursor.Digest
	for _, event := range events {
		sequence++
		if event.Tenant != tenant || event.Sequence != sequence || event.PreviousDigest != previous || !event.validate() {
			return Feed{}, ErrInvalidFeed
		}
		previous = event.Digest
	}
	next := cursor
	if len(events) > 0 {
		last := events[len(events)-1]
		next = Cursor{Tenant: tenant, Sequence: last.Sequence, Digest: last.Digest}
	}
	feed := Feed{SchemaVersion: SchemaVersion, Tenant: tenant, From: cursor, Next: next, Events: append([]Event(nil), events...)}
	feed.Digest = feed.signingDigest()
	signed, err := signer.Sign(feed.Digest, at)
	if err != nil {
		return Feed{}, err
	}
	feed.Signature = signed
	return feed, nil
}

// Verify checks tenant scope, event order and chain links within a page, then
// verifies the page signature through the destination credential ring.
func Verify(feed Feed, tenant string, at time.Time, signer *subscription.CredentialRing) error {
	if signer == nil || at.IsZero() || feed.SchemaVersion != SchemaVersion || tenant == "" || feed.Tenant != tenant ||
		feed.From.Tenant != tenant || feed.Next.Tenant != tenant || feed.Digest != feed.signingDigest() {
		return ErrInvalidFeed
	}
	previous := feed.From.Digest
	sequence := feed.From.Sequence
	for _, event := range feed.Events {
		sequence++
		if event.Tenant != tenant || event.Sequence != sequence || event.PreviousDigest != previous || !event.validate() {
			return ErrInvalidFeed
		}
		previous = event.Digest
	}
	if sequence != feed.Next.Sequence || previous != feed.Next.Digest {
		return ErrInvalidFeed
	}
	if err := signer.Verify(feed.Signature, feed.Digest, at); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFeed, err)
	}
	return nil
}

// EventsOfType returns the closed, lexically ordered event vocabulary.
func EventsOfType() []EventType {
	out := []EventType{EventAlertRule, EventDLP, EventAccess, EventAdmin}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Reconcile delegates gap detection to SUB-008's delivery journal and refuses
// an expectation for another tenant before inspecting delivery evidence.
func Reconcile(journal *subscription.DeliveryJournal, expectation subscription.CompletenessExpectation, tenant string) (subscription.CompletenessReport, error) {
	if tenant == "" || expectation.Tenant != tenant {
		return subscription.CompletenessReport{}, subscription.ErrInvalidCompleteness
	}
	return subscription.ReconcileDelivery(journal, expectation)
}
