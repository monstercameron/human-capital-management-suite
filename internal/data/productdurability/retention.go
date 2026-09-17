package productdurability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ALIGN-039: product records carry retention and disposition. Every record
// kind names how long it is kept and what happens after: archive or
// destroy. A legal hold freezes disposition regardless of age, so nothing
// under hold is archived or destroyed. Expiry is derived from the creation
// instant and the rule — never from wall-clock code scattered across
// callers.

// Retention errors.
var (
	ErrRetentionInvalid = errors.New("productdurability: retention rule is invalid")
	ErrRetentionUnknown = errors.New("productdurability: record kind has no retention rule")
)

// RecordState is the retention state of one record.
type RecordState string

// Retention states.
const (
	RecordActive  RecordState = "ACTIVE"
	RecordExpired RecordState = "EXPIRED"
	RecordHeld    RecordState = "HELD"
)

// DispositionAction is what happens to an expired record.
type DispositionAction string

// Disposition actions.
const (
	DisposeRetain  DispositionAction = "RETAIN"
	DisposeArchive DispositionAction = "ARCHIVE"
	DisposeDestroy DispositionAction = "DESTROY"
)

// RetentionRule keeps one record kind for RetainFor, then applies Then.
type RetentionRule struct {
	Kind      string            `json:"kind"`
	RetainFor time.Duration     `json:"retain_for"`
	Then      DispositionAction `json:"then"`
}

// RetentionSchedule is the versioned rule set. The zero value is not
// usable; build one with NewRetentionSchedule.
type RetentionSchedule struct {
	mu    sync.Mutex
	rules map[string]RetentionRule
}

// NewRetentionSchedule builds an empty schedule.
func NewRetentionSchedule() *RetentionSchedule {
	return &RetentionSchedule{rules: make(map[string]RetentionRule)}
}

// Register adds the rule for one record kind. A non-positive retention and
// a retain-forever rule are both invalid: every kind must name a bounded
// retention and a terminal disposition.
func (s *RetentionSchedule) Register(rule RetentionRule) error {
	if strings.TrimSpace(rule.Kind) == "" {
		return fmt.Errorf("%w: record kind is required", ErrRetentionInvalid)
	}
	if rule.RetainFor <= 0 {
		return fmt.Errorf("%w: %s must name a positive retention", ErrRetentionInvalid, rule.Kind)
	}
	switch rule.Then {
	case DisposeArchive, DisposeDestroy:
	default:
		return fmt.Errorf("%w: %s disposition %q is not archive or destroy", ErrRetentionInvalid, rule.Kind, rule.Then)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[rule.Kind] = rule
	return nil
}

// Classify derives the state and next action for one record. A held record
// is HELD with RETAIN whatever its age; an unexpired record is ACTIVE with
// RETAIN; an expired record is EXPIRED with its terminal disposition.
func (s *RetentionSchedule) Classify(kind string, createdAt, now time.Time, held bool) (RecordState, DispositionAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, ok := s.rules[kind]
	if !ok {
		return "", "", fmt.Errorf("%w: %s", ErrRetentionUnknown, kind)
	}
	if createdAt.IsZero() || now.IsZero() {
		return "", "", fmt.Errorf("%w: creation and evaluation instants are required", ErrRetentionInvalid)
	}
	if held {
		return RecordHeld, DisposeRetain, nil
	}
	if now.Before(createdAt.Add(rule.RetainFor)) {
		return RecordActive, DisposeRetain, nil
	}
	return RecordExpired, rule.Then, nil
}

// Digest returns the content digest over the sorted rules.
func (s *RetentionSchedule) Digest() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kinds := make([]string, 0, len(s.rules))
	for kind := range s.rules {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	h := sha256.New()
	for _, kind := range kinds {
		rule := s.rules[kind]
		fmt.Fprintf(h, "%s|%d|%s;", kind, int64(rule.RetainFor), rule.Then)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
