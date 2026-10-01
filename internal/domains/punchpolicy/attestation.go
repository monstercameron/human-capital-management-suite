package punchpolicy

import (
	"fmt"
	"strings"
)

// QuestionKind is the closed vocabulary of TCLOCK-010 attestation questions.
type QuestionKind string

const (
	QuestionBreakProvided     QuestionKind = "BREAK_PROVIDED"
	QuestionMissedBreakReason QuestionKind = "MISSED_BREAK_REASON"
	QuestionInjury            QuestionKind = "INJURY"
	QuestionCustom            QuestionKind = "CUSTOM"
)

func (k QuestionKind) Valid() bool {
	switch k {
	case QuestionBreakProvided, QuestionMissedBreakReason, QuestionInjury, QuestionCustom:
		return true
	}
	return false
}

// Question is one versioned attestation prompt. TextKey is a localization
// key, never literal text: the device renders it in the worker's language
// from the shared key table, so the policy never pins a locale. Options is
// the closed answer vocabulary for a bounded question (empty for free text).
type Question struct {
	ID       string
	Kind     QuestionKind
	TextKey  string
	Required bool
	Options  []string
}

func (q Question) validate() error {
	if strings.TrimSpace(q.ID) == "" {
		return fmt.Errorf("%w: question id is required", ErrInvalidQuestionSet)
	}
	if !q.Kind.Valid() {
		return fmt.Errorf("%w: question %s has undeclared kind %q", ErrInvalidQuestionSet, q.ID, q.Kind)
	}
	if strings.TrimSpace(q.TextKey) == "" {
		return fmt.Errorf("%w: question %s has no localization key", ErrInvalidQuestionSet, q.ID)
	}
	return nil
}

// QuestionSet is a versioned, jurisdiction-scoped attestation question set
// shown at clock-out.
type QuestionSet struct {
	ID           string
	Version      int
	Jurisdiction string
	Questions    []Question
}

// Validate reports whether the set is well-formed: every question is valid
// and no question id repeats.
func (s QuestionSet) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: question set id is required", ErrInvalidQuestionSet)
	}
	if s.Version < 1 {
		return fmt.Errorf("%w: question set version must be at least 1", ErrInvalidQuestionSet)
	}
	if strings.TrimSpace(s.Jurisdiction) == "" {
		return fmt.Errorf("%w: question set jurisdiction is required", ErrInvalidQuestionSet)
	}
	seen := make(map[string]struct{}, len(s.Questions))
	for _, q := range s.Questions {
		if err := q.validate(); err != nil {
			return err
		}
		if _, dup := seen[q.ID]; dup {
			return fmt.Errorf("%w: duplicate question id %q", ErrInvalidQuestionSet, q.ID)
		}
		seen[q.ID] = struct{}{}
	}
	return nil
}

func (s QuestionSet) find(id string) (Question, bool) {
	for _, q := range s.Questions {
		if q.ID == id {
			return q, true
		}
	}
	return Question{}, false
}

// Answer is the worker's response to one question, given at the device.
type Answer struct {
	QuestionID string
	Value      string
}

// ConsequenceKind is the closed vocabulary of typed effects an answer can
// produce.
type ConsequenceKind string

const (
	// ConsequencePremiumInput is a request for the premium-pay input
	// modeled by REV-045-01: it names the fact (a required break was not
	// provided) that the payroll input layer turns into an authoritative
	// premium calculation. This package never calculates or posts the
	// premium itself.
	ConsequencePremiumInput ConsequenceKind = "PREMIUM_INPUT"
	// ConsequenceCaseTask is a request to open a case task, for example on
	// an injury answer. This package never opens the case: it names the
	// request for the case-management layer to act on.
	ConsequenceCaseTask ConsequenceKind = "CASE_TASK"
)

// PremiumInputRequest names the premium-pay input fact an answer produced.
// RuleRef is the todo that models the premium: REV-045-01.
type PremiumInputRequest struct {
	RuleRef string
	Reason  string
}

// CaseTaskRequest names the case-management task an answer produced.
type CaseTaskRequest struct {
	Reason   string
	Severity string
}

// Consequence is the typed effect of one answer. Exactly one of Premium or
// CaseTask is set, matching Kind.
type Consequence struct {
	Kind       ConsequenceKind
	QuestionID string
	Premium    *PremiumInputRequest
	CaseTask   *CaseTaskRequest
}

// Consequences validates every required question is answered and returns
// the typed effects the answers produce. "Break not provided" produces a
// REV-045-01 premium input request; an injury answer produces a case task
// request. A missing required answer fails closed rather than silently
// skipping the consequence a worker was never asked to disclose.
func Consequences(set QuestionSet, answers []Answer) ([]Consequence, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	byQuestion := make(map[string]Answer, len(answers))
	for _, a := range answers {
		if _, ok := set.find(a.QuestionID); !ok {
			return nil, fmt.Errorf("%w: answer to unknown question %q", ErrInvalidQuestionSet, a.QuestionID)
		}
		byQuestion[a.QuestionID] = a
	}
	for _, q := range set.Questions {
		if q.Required {
			if _, answered := byQuestion[q.ID]; !answered {
				return nil, fmt.Errorf("%w: %s", ErrUnansweredRequired, q.ID)
			}
		}
	}

	var out []Consequence
	for _, q := range set.Questions {
		a, answered := byQuestion[q.ID]
		if !answered {
			continue
		}
		switch q.Kind {
		case QuestionBreakProvided:
			if strings.EqualFold(a.Value, "false") || strings.EqualFold(a.Value, "no") {
				out = append(out, Consequence{
					Kind:       ConsequencePremiumInput,
					QuestionID: q.ID,
					Premium:    &PremiumInputRequest{RuleRef: "REV-045-01", Reason: "meal or rest break not provided"},
				})
			}
		case QuestionInjury:
			if strings.EqualFold(a.Value, "true") || strings.EqualFold(a.Value, "yes") {
				out = append(out, Consequence{
					Kind:       ConsequenceCaseTask,
					QuestionID: q.ID,
					CaseTask:   &CaseTaskRequest{Reason: "worker reported an injury on shift", Severity: "REVIEW"},
				})
			}
		case QuestionMissedBreakReason, QuestionCustom:
			// Recorded as clock evidence with the punch receipt; neither
			// kind produces a typed consequence on its own.
		}
	}
	return out, nil
}
