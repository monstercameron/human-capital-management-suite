// TCLOCK-010's service part: clock-out attestation answers and tip
// declarations are policy data served through PolicySource
// (TCLOCK-004 hands the same question set to the device) and stored as
// Clock evidence with the punch receipt, never as a separate form
// submission. A "not provided" break answer and an injury answer are
// dispatched as typed consequences through PremiumInputs and CaseTasks;
// this package never calculates a premium or opens a case itself.
package clockservice

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

// dispatchClockOutConsequences validates the worker's clock-out answers
// against the site's pinned attestation question set and dispatches every
// typed consequence the answers produce. A required question with no
// answer fails closed (ErrInvalidRequest wrapping
// punchpolicy.ErrUnansweredRequired) rather than silently skipping a
// consequence the worker was never asked to disclose.
func (s Service) dispatchClockOutConsequences(ctx context.Context, tenant string, receipt PunchReceipt, req ClockOutRequest) error {
	var (
		set     punchpolicy.QuestionSet
		answers []ClockOutAnswer
		conseq  []punchpolicy.Consequence
		tip     *TipDeclarationEvidence
	)
	if len(req.Answers) > 0 {
		if s.Policies == nil {
			return ErrUnavailable
		}
		if strings.TrimSpace(req.SiteID) == "" {
			return reject(ErrInvalidRequest, "site_id", "", "site is required when clock-out answers are supplied")
		}
		var err error
		set, err = s.Policies.AttestationQuestions(ctx, tenant, req.SiteID)
		if err != nil {
			return err
		}
		answers, err = normalizeClockOutAnswers(set, req.Answers)
		if err != nil {
			return err
		}
		policyAnswers := make([]punchpolicy.Answer, 0, len(answers))
		for _, a := range answers {
			policyAnswers = append(policyAnswers, punchpolicy.Answer{QuestionID: a.QuestionID, Value: a.Value})
		}
		conseq, err = punchpolicy.Consequences(set, policyAnswers)
		if err != nil {
			return reject(ErrInvalidRequest, "answers", "", err.Error())
		}
	}
	if strings.TrimSpace(req.TipAmount) != "" {
		var err error
		tip, err = newTipDeclarationEvidence(receipt, strings.TrimSpace(req.TipAmount), s.now())
		if err != nil {
			return reject(ErrInvalidRequest, "tip_amount", "", err.Error())
		}
	}

	// The punch is already authoritative. Evidence is appended after it and
	// is best effort so a transient evidence/outbox failure never turns a
	// successful clock-out into a retry that could duplicate the punch.
	if len(answers) > 0 || tip != nil {
		_ = s.appendClockOutEvidence(ctx, tenant, receipt, req, set, answers, tip)
	}
	for _, c := range conseq {
		switch c.Kind {
		case punchpolicy.ConsequencePremiumInput:
			if s.PremiumInputs == nil {
				return ErrUnavailable
			}
			if err := s.PremiumInputs.RequestPremiumInput(ctx, tenant, receipt.WorkerRef, receipt.SessionID, c.Premium.RuleRef, c.Premium.Reason, s.now()); err != nil {
				return err
			}
		case punchpolicy.ConsequenceCaseTask:
			if s.CaseTasks == nil {
				return ErrUnavailable
			}
			if err := s.CaseTasks.OpenCaseTask(ctx, tenant, receipt.WorkerRef, receipt.SessionID, c.CaseTask.Reason, c.CaseTask.Severity); err != nil {
				return err
			}
		}
	}
	return nil
}
