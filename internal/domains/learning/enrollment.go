// LEARN-003: assign and enroll learners. Duplicate or stale assignments
// and invalid capacity or windows fail; enrollment is idempotent and
// capacity-fenced. Every assignment records its requirement source, due
// date, waiver or escalation route and participation state.
package learning

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidAssignment = errors.New("learning: invalid assignment")
	ErrUnknownAssignment = errors.New("learning: unknown assignment")
	ErrCourseFull        = errors.New("learning: course capacity is full")
	ErrOutsideWindow     = errors.New("learning: enrollment is outside the window")
)

// Assignment states.
const (
	AssignmentAssigned = "ASSIGNED"
	AssignmentEnrolled = "ENROLLED"
)

// Assignment is one learner's version-pinned assignment record.
type Assignment struct {
	ID                string
	LearnerID         string
	CourseID          string
	Version           uint64
	Tenant            string
	RequirementSource string
	DueAt             time.Time
	WaiverRef         string
	EscalationRef     string
	Capacity          int
	WindowStart       time.Time
	WindowEnd         time.Time
	State             string
	Digest            string
}

// ValidateAssignment checks an assignment without recording it.
func ValidateAssignment(a Assignment) error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.LearnerID) == "" ||
		strings.TrimSpace(a.CourseID) == "" || a.Version == 0 ||
		strings.TrimSpace(a.Tenant) == "" || strings.TrimSpace(a.RequirementSource) == "" {
		return fmt.Errorf("%w: id, learner, course, version, tenant and requirement source are required", ErrInvalidAssignment)
	}
	if a.DueAt.IsZero() {
		return fmt.Errorf("%w: due date is required", ErrInvalidAssignment)
	}
	if a.Capacity <= 0 {
		return fmt.Errorf("%w: capacity must be positive", ErrInvalidAssignment)
	}
	if a.WindowStart.IsZero() || a.WindowEnd.IsZero() || !a.WindowEnd.After(a.WindowStart) {
		return fmt.Errorf("%w: enrollment window must be a valid interval", ErrInvalidAssignment)
	}
	return nil
}

func assignmentDigest(a Assignment) (string, error) {
	w := canonicalbytes.New("learning-assignment", 1)
	w.String("id", a.ID)
	w.String("learner_id", a.LearnerID)
	w.String("course_id", a.CourseID)
	w.Int("version", int64(a.Version))
	w.String("tenant", a.Tenant)
	w.String("requirement_source", a.RequirementSource)
	w.String("due_at", a.DueAt.UTC().Format(time.RFC3339))
	w.String("waiver_ref", a.WaiverRef)
	w.String("escalation_ref", a.EscalationRef)
	w.Int("capacity", int64(a.Capacity))
	w.String("window_start", a.WindowStart.UTC().Format(time.RFC3339))
	w.String("window_end", a.WindowEnd.UTC().Format(time.RFC3339))
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// Assign records one ASSIGNED assignment for a recorded course version.
func (r *Registry) Assign(caller Caller, a Assignment) (Assignment, error) {
	if err := ValidateAssignment(a); err != nil {
		return Assignment{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorize(caller, a.Tenant); err != nil {
		return Assignment{}, err
	}
	if _, ok := r.courses.LoadVersion(a.CourseID, a.Version); !ok {
		return Assignment{}, fmt.Errorf("%w: %s", ErrUnknownCourse, versionKey(a.CourseID, a.Version))
	}
	if _, dup := r.assignments[a.ID]; dup {
		return Assignment{}, fmt.Errorf("%w: assignment %s", ErrDuplicateEntry, a.ID)
	}
	a.State = AssignmentAssigned
	digest, err := assignmentDigest(a)
	if err != nil {
		return Assignment{}, err
	}
	a.Digest = digest
	r.assignments[a.ID] = a
	r.journal = append(r.journal, JournalEntry{
		Op: "assign", Ref: a.ID,
		Detail: fmt.Sprintf("learner=%s course=%s source=%s", a.LearnerID, versionKey(a.CourseID, a.Version), a.RequirementSource),
	})
	return a, nil
}

// Enroll moves one assignment to ENROLLED. Repeat enrollment is
// idempotent: it returns the stored record byte-identically.
func (r *Registry) Enroll(caller Caller, id string, at time.Time) (Assignment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.assignments[id]
	if !ok {
		return Assignment{}, fmt.Errorf("%w: %s", ErrUnknownAssignment, id)
	}
	if err := r.authorize(caller, a.Tenant); err != nil {
		return Assignment{}, err
	}
	if a.State == AssignmentEnrolled {
		return a, nil
	}
	if at.IsZero() || at.Before(a.WindowStart) || !at.Before(a.WindowEnd) {
		return Assignment{}, fmt.Errorf("%w: %s", ErrOutsideWindow, id)
	}
	key := versionKey(a.CourseID, a.Version)
	if r.enrollCounts[key] >= a.Capacity {
		return Assignment{}, fmt.Errorf("%w: %s", ErrCourseFull, key)
	}
	a.State = AssignmentEnrolled
	r.assignments[id] = a
	r.enrollCounts[key]++
	r.journal = append(r.journal, JournalEntry{Op: "enroll", Ref: id, Detail: "learner=" + a.LearnerID})
	return a, nil
}
