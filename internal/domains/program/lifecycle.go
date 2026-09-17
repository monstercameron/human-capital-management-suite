// PROGRAM-004: govern the participate, enroll and withdraw lifecycle.
// Every transition is version-fenced and recorded with eligibility,
// elections, dates, reason and downstream effects; illegal, stale or
// duplicate transitions — and withdrawals contrary to open obligations —
// fail without mutating state.
package program

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidEnrollment = errors.New("program: invalid enrollment")
	ErrInvalidTransition = errors.New("program: invalid lifecycle transition")
	ErrStaleTransition   = errors.New("program: stale lifecycle transition")
	ErrUnknownEnrollment = errors.New("program: unknown enrollment")
)

// ParticipationState is the closed enrollment lifecycle vocabulary.
type ParticipationState string

const (
	EnrollmentProposed  ParticipationState = "PROPOSED"
	EnrollmentEnrolled  ParticipationState = "ENROLLED"
	EnrollmentSuspended ParticipationState = "SUSPENDED"
	EnrollmentWithdrawn ParticipationState = "WITHDRAWN"
)

func validParticipationState(s ParticipationState) bool {
	return s == EnrollmentProposed || s == EnrollmentEnrolled ||
		s == EnrollmentSuspended || s == EnrollmentWithdrawn
}

// allowedTransitions is the lifecycle graph. WITHDRAWN is terminal.
var allowedTransitions = map[ParticipationState][]ParticipationState{
	EnrollmentProposed:  {EnrollmentEnrolled, EnrollmentWithdrawn},
	EnrollmentEnrolled:  {EnrollmentSuspended, EnrollmentWithdrawn},
	EnrollmentSuspended: {EnrollmentEnrolled, EnrollmentWithdrawn},
	EnrollmentWithdrawn: {},
}

// Enrollment is one participant's versioned lifecycle record.
type Enrollment struct {
	ID                string
	ProgramID         string
	Participant       string
	Tenant            string
	State             ParticipationState
	EligibilityRef    string
	Elections         []string
	StartsAt          time.Time
	EndsAt            time.Time
	Reason            string
	Effects           []string
	Obligations       []string
	ObligationRelease string
	Version           uint64
	Digest            string
}

// Transition is one requested lifecycle move.
type Transition struct {
	EnrollmentID      string
	To                ParticipationState
	Reason            string
	At                time.Time
	ExpectedVersion   uint64
	ObligationRelease string
}

// ValidateTransition checks a transition's shape without applying it.
func ValidateTransition(tr Transition) error {
	if strings.TrimSpace(tr.EnrollmentID) == "" || strings.TrimSpace(tr.Reason) == "" {
		return fmt.Errorf("%w: enrollment and reason are required", ErrInvalidTransition)
	}
	if !validParticipationState(tr.To) {
		return fmt.Errorf("%w: unknown state %q", ErrInvalidTransition, tr.To)
	}
	if tr.At.IsZero() {
		return fmt.Errorf("%w: instant is required", ErrInvalidTransition)
	}
	if tr.ExpectedVersion == 0 {
		return fmt.Errorf("%w: expected version is required", ErrStaleTransition)
	}
	return nil
}

func enrollmentDigest(e Enrollment) (string, error) {
	w := canonicalbytes.New("program-enrollment", 1)
	w.String("id", e.ID)
	w.String("program_id", e.ProgramID)
	w.String("participant", e.Participant)
	w.String("tenant", e.Tenant)
	w.String("state", string(e.State))
	w.String("eligibility_ref", e.EligibilityRef)
	w.SortedStrings("elections", e.Elections)
	w.String("starts_at", e.StartsAt.UTC().Format(time.RFC3339))
	w.String("reason", e.Reason)
	w.SortedStrings("effects", e.Effects)
	w.SortedStrings("obligations", e.Obligations)
	w.String("obligation_release", e.ObligationRelease)
	w.Int("version", int64(e.Version))
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// CreateEnrollment opens a PROPOSED enrollment.
func (c *Catalog) CreateEnrollment(caller Caller, e Enrollment) (Enrollment, error) {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.ProgramID) == "" ||
		strings.TrimSpace(e.Participant) == "" || strings.TrimSpace(e.Tenant) == "" ||
		strings.TrimSpace(e.EligibilityRef) == "" || strings.TrimSpace(e.Reason) == "" {
		return Enrollment{}, fmt.Errorf("%w: id, program, participant, tenant, eligibility and reason are required", ErrInvalidEnrollment)
	}
	if e.State != "" && e.State != EnrollmentProposed {
		return Enrollment{}, fmt.Errorf("%w: new enrollments open PROPOSED", ErrInvalidEnrollment)
	}
	if e.StartsAt.IsZero() {
		return Enrollment{}, fmt.Errorf("%w: start instant is required", ErrInvalidEnrollment)
	}
	if err := c.authorize(caller, e.Tenant); err != nil {
		return Enrollment{}, err
	}
	if _, dup := c.enrollments[e.ID]; dup {
		return Enrollment{}, fmt.Errorf("%w: %s", ErrInvalidEnrollment, e.ID)
	}
	e.State = EnrollmentProposed
	e.Version = 1
	digest, err := enrollmentDigest(e)
	if err != nil {
		return Enrollment{}, err
	}
	e.Digest = digest
	c.enrollments[e.ID] = e
	c.journal = append(c.journal, JournalEntry{
		Op: "enroll-open", Ref: e.ID,
		Detail: fmt.Sprintf("program=%s participant=%s eligibility=%s elections=%s effects=%s",
			e.ProgramID, e.Participant, e.EligibilityRef,
			strings.Join(e.Elections, ","), strings.Join(e.Effects, ",")),
	})
	return e, nil
}

// LookupEnrollment returns the current enrollment record.
func (c *Catalog) LookupEnrollment(id string) (Enrollment, bool) {
	e, ok := c.enrollments[id]
	return e, ok
}

// ApplyTransition moves one enrollment along the lifecycle graph.
func (c *Catalog) ApplyTransition(caller Caller, tr Transition) (Enrollment, error) {
	if err := ValidateTransition(tr); err != nil {
		return Enrollment{}, err
	}
	current, ok := c.enrollments[tr.EnrollmentID]
	if !ok {
		return Enrollment{}, fmt.Errorf("%w: %s", ErrUnknownEnrollment, tr.EnrollmentID)
	}
	if err := c.authorize(caller, current.Tenant); err != nil {
		return Enrollment{}, err
	}
	if tr.ExpectedVersion != current.Version {
		return Enrollment{}, fmt.Errorf("%w: enrollment at version %d", ErrStaleTransition, current.Version)
	}
	allowed := false
	for _, next := range allowedTransitions[current.State] {
		if next == tr.To {
			allowed = true
		}
	}
	if !allowed {
		return Enrollment{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.State, tr.To)
	}
	if tr.To == EnrollmentWithdrawn && len(current.Obligations) > 0 && strings.TrimSpace(tr.ObligationRelease) == "" {
		return Enrollment{}, fmt.Errorf("%w: open obligations %s require a release",
			ErrInvalidTransition, strings.Join(current.Obligations, ","))
	}
	current.State = tr.To
	current.Reason = tr.Reason
	if tr.ObligationRelease != "" {
		current.ObligationRelease = tr.ObligationRelease
	}
	current.Version++
	digest, err := enrollmentDigest(current)
	if err != nil {
		return Enrollment{}, err
	}
	current.Digest = digest
	c.enrollments[tr.EnrollmentID] = current
	c.journal = append(c.journal, JournalEntry{
		Op: "transition", Ref: tr.EnrollmentID,
		Detail: fmt.Sprintf("to=%s reason=%s version=%d effects=%s",
			tr.To, tr.Reason, current.Version, strings.Join(current.Effects, ",")),
	})
	return current, nil
}
