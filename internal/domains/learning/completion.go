// LEARN-004: receive authoritative learning completion. Every provider
// event resolves learner, course, version, assessment, time and source
// authority, dedupes by event identity across registry instances sharing
// one log, and keeps acceptance distinct from verified completion.
package learning

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidCompletion = errors.New("learning: invalid completion event")
	ErrUnknownCompletion = errors.New("learning: unknown completion event")
	ErrUntrustedSource   = errors.New("learning: untrusted completion source")
)

// Completion statuses: acceptance is never verification.
const (
	CompletionAccepted = "ACCEPTED"
	CompletionVerified = "VERIFIED"
)

// CompletionEvent is one provider completion claim.
type CompletionEvent struct {
	EventID         string
	LearnerID       string
	CourseID        string
	Version         uint64
	AssessmentRef   string
	CompletedAt     time.Time
	SourceAuthority string
	Tenant          string
}

// CompletionRecord is the stored intake record.
type CompletionRecord struct {
	EventID         string
	LearnerID       string
	CourseID        string
	Version         uint64
	AssessmentRef   string
	CompletedAt     time.Time
	SourceAuthority string
	Tenant          string
	Status          string
	EvidenceRef     string
	Duplicate       bool
	Digest          string
}

// ValidateCompletionEvent checks an event without recording it.
func ValidateCompletionEvent(ev CompletionEvent) error {
	for _, ref := range []struct {
		name, value string
	}{
		{"event_id", ev.EventID},
		{"learner_id", ev.LearnerID},
		{"course_id", ev.CourseID},
		{"assessment_ref", ev.AssessmentRef},
		{"source_authority", ev.SourceAuthority},
		{"tenant", ev.Tenant},
	} {
		if strings.TrimSpace(ref.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidCompletion, ref.name)
		}
	}
	if ev.Version == 0 {
		return fmt.Errorf("%w: version is required", ErrInvalidCompletion)
	}
	if ev.CompletedAt.IsZero() {
		return fmt.Errorf("%w: completion instant is required", ErrInvalidCompletion)
	}
	return nil
}

func completionDigest(ev CompletionEvent) (string, error) {
	w := canonicalbytes.New("learning-completion", 1)
	w.String("event_id", ev.EventID)
	w.String("learner_id", ev.LearnerID)
	w.String("course_id", ev.CourseID)
	w.Int("version", int64(ev.Version))
	w.String("assessment_ref", ev.AssessmentRef)
	w.String("completed_at", ev.CompletedAt.UTC().Format(time.RFC3339))
	w.String("source_authority", ev.SourceAuthority)
	w.String("tenant", ev.Tenant)
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// CompletionLog is the completion persistence port: the source of
// cross-instance idempotency.
type CompletionLog interface {
	Append(rec CompletionRecord) (duplicate bool, err error)
	Verify(eventID, evidenceRef string) (CompletionRecord, error)
	Get(eventID string) (CompletionRecord, bool)
	FindVerified(learnerID, courseID string, version uint64) (CompletionRecord, bool)
	Count() int
}

type memoryCompletionLog struct {
	mu      sync.Mutex
	records map[string]CompletionRecord
}

// NewMemoryCompletionLog returns an empty in-memory completion log.
func NewMemoryCompletionLog() *memoryCompletionLog {
	return &memoryCompletionLog{records: map[string]CompletionRecord{}}
}

func (l *memoryCompletionLog) Append(rec CompletionRecord) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, dup := l.records[rec.EventID]; dup {
		existing.Duplicate = true
		return true, nil
	}
	rec.Duplicate = false
	l.records[rec.EventID] = rec
	return false, nil
}

func (l *memoryCompletionLog) Verify(eventID, evidenceRef string) (CompletionRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[eventID]
	if !ok {
		return CompletionRecord{}, fmt.Errorf("%w: %s", ErrUnknownCompletion, eventID)
	}
	rec.Status = CompletionVerified
	rec.EvidenceRef = evidenceRef
	l.records[eventID] = rec
	out := rec
	out.Duplicate = false
	return out, nil
}

func (l *memoryCompletionLog) Get(eventID string) (CompletionRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[eventID]
	return rec, ok
}

func (l *memoryCompletionLog) FindVerified(learnerID, courseID string, version uint64) (CompletionRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, rec := range l.records {
		if rec.LearnerID == learnerID && rec.CourseID == courseID &&
			rec.Version == version && rec.Status == CompletionVerified {
			return rec, true
		}
	}
	return CompletionRecord{}, false
}

func (l *memoryCompletionLog) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records)
}

// AcceptCompletion intakes one provider event as ACCEPTED. Replays dedupe
// to the identical digest with Duplicate set.
func (r *Registry) AcceptCompletion(caller Caller, ev CompletionEvent) (CompletionRecord, error) {
	if err := ValidateCompletionEvent(ev); err != nil {
		return CompletionRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, ev.Tenant); err != nil {
		return CompletionRecord{}, err
	}
	v, ok := r.courses.LoadVersion(ev.CourseID, ev.Version)
	if !ok || v.Tenant != ev.Tenant {
		return CompletionRecord{}, fmt.Errorf("%w: %s", ErrUnknownCourse, versionKey(ev.CourseID, ev.Version))
	}
	if !r.trustedProvider(ev.SourceAuthority) {
		return CompletionRecord{}, fmt.Errorf("%w: %s", ErrUntrustedSource, ev.SourceAuthority)
	}
	known := false
	for _, ref := range v.AssessmentRefs {
		if ref == ev.AssessmentRef {
			known = true
		}
	}
	if !known {
		return CompletionRecord{}, fmt.Errorf("%w: assessment %s is outside %s",
			ErrInvalidCompletion, ev.AssessmentRef, versionKey(ev.CourseID, ev.Version))
	}
	digest, err := completionDigest(ev)
	if err != nil {
		return CompletionRecord{}, err
	}
	rec := CompletionRecord{
		EventID: ev.EventID, LearnerID: ev.LearnerID, CourseID: ev.CourseID,
		Version: ev.Version, AssessmentRef: ev.AssessmentRef, CompletedAt: ev.CompletedAt,
		SourceAuthority: ev.SourceAuthority, Tenant: ev.Tenant,
		Status: CompletionAccepted, Digest: digest,
	}
	dup, err := r.completions.Append(rec)
	if err != nil {
		return CompletionRecord{}, err
	}
	if dup {
		existing, _ := r.completions.Get(ev.EventID)
		existing.Duplicate = true
		return existing, nil
	}
	r.journal = append(r.journal, JournalEntry{
		Op: "accept-completion", Ref: ev.EventID,
		Detail: fmt.Sprintf("learner=%s course=%s source=%s", ev.LearnerID, versionKey(ev.CourseID, ev.Version), ev.SourceAuthority),
	})
	return rec, nil
}

// VerifyCompletion promotes one accepted intake to VERIFIED against fresh
// evidence. The sealed acceptance digest never changes.
func (r *Registry) VerifyCompletion(caller Caller, eventID, evidenceRef string) (CompletionRecord, error) {
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(evidenceRef) == "" {
		return CompletionRecord{}, fmt.Errorf("%w: event and evidence are required", ErrInvalidCompletion)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.completions.Get(eventID)
	if !ok {
		return CompletionRecord{}, fmt.Errorf("%w: %s", ErrUnknownCompletion, eventID)
	}
	if err := r.authorize(caller, rec.Tenant); err != nil {
		return CompletionRecord{}, err
	}
	verified, err := r.completions.Verify(eventID, evidenceRef)
	if err != nil {
		return CompletionRecord{}, err
	}
	r.journal = append(r.journal, JournalEntry{
		Op: "verify-completion", Ref: eventID, Detail: "evidence=" + evidenceRef,
	})
	return verified, nil
}

// CompletionCount returns the number of stored intake records.
func (r *Registry) CompletionCount() int {
	return r.completions.Count()
}
