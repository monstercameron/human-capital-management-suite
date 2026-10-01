package clockservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

const (
	clockOutAttestationSource = "CLOCK_OUT_ATTESTATION"
	clockOutAttestationEvent  = "CLOCK_OUT_ATTESTATION"
	tipAdjustmentEvent        = "TIP_ADJUSTMENT"
)

// ClockOutAnswer is the public device-facing form of one answer. The
// ClockOutRequest helper methods below keep the request's compatibility with
// the original package-private wire shape while allowing transports outside
// this package to build a request safely.
type ClockOutAnswer struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

// ClockOutEvidence is the immutable evidence linked to the accepted OUT
// observation. Question-set identity is retained with the answers so a later
// reader never has to guess which policy version asked the worker.
type ClockOutEvidence struct {
	SchemaVersion        int                     `json:"schema_version"`
	ReceiptObservationID string                  `json:"receipt_observation_id"`
	TenantID             string                  `json:"tenant_id"`
	WorkerRef            string                  `json:"worker_ref"`
	AssignmentRef        string                  `json:"assignment_ref"`
	SessionID            string                  `json:"session_id"`
	SiteID               string                  `json:"site_id,omitempty"`
	QuestionSetID        string                  `json:"question_set_id,omitempty"`
	QuestionSetVersion   int                     `json:"question_set_version,omitempty"`
	Jurisdiction         string                  `json:"jurisdiction,omitempty"`
	Answers              []ClockOutAnswer        `json:"answers,omitempty"`
	TipDeclaration       *TipDeclarationEvidence `json:"tip_declaration,omitempty"`
	RecordedAt           time.Time               `json:"recorded_at"`
}

// TipDeclarationEvidence is the JSON-safe projection of the worker's tip
// statement. Decimal values are represented as their exact fixed-point text.
type TipDeclarationEvidence struct {
	WorkerID   string    `json:"worker_id"`
	ShiftID    string    `json:"shift_id"`
	DeclaredBy string    `json:"declared_by"`
	DeclaredAt time.Time `json:"declared_at"`
	Amount     string    `json:"amount"`
}

// WithAttestationAnswers returns a request with the supplied device answers.
// It is the transport-safe constructor for ClockOutRequest.Answers.
func (r ClockOutRequest) WithAttestationAnswers(answers ...ClockOutAnswer) ClockOutRequest {
	r.Answers = make([]punchAnswer, len(answers))
	for i, answer := range answers {
		r.Answers[i] = punchAnswer{QuestionID: answer.QuestionID, Value: answer.Value}
	}
	return r
}

// NewClockOutAnswer constructs one answer without exposing the request's
// internal answer representation to device transports.
func NewClockOutAnswer(questionID, value string) ClockOutAnswer {
	return ClockOutAnswer{QuestionID: questionID, Value: value}
}

func normalizeClockOutAnswers(set punchpolicy.QuestionSet, raw []punchAnswer) ([]ClockOutAnswer, error) {
	if err := set.Validate(); err != nil {
		return nil, reject(ErrInvalidRequest, "answers", "", err.Error())
	}
	questions := make(map[string]punchpolicy.Question, len(set.Questions))
	for _, question := range set.Questions {
		questions[question.ID] = question
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]ClockOutAnswer, 0, len(raw))
	for _, answer := range raw {
		id := strings.TrimSpace(answer.QuestionID)
		value := strings.TrimSpace(answer.Value)
		if id == "" || value == "" {
			return nil, reject(ErrInvalidRequest, "answers", "", "question id and answer value are required")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, reject(ErrInvalidRequest, "answers", "", fmt.Sprintf("question %q was answered more than once", id))
		}
		question, known := questions[id]
		if !known {
			return nil, reject(ErrInvalidRequest, "answers", "", fmt.Sprintf("answer to unknown question %q", id))
		}
		if len(question.Options) > 0 && !containsQuestionOption(question.Options, value) {
			return nil, reject(ErrInvalidRequest, "answers", "", fmt.Sprintf("answer %q is not an option for question %q", value, id))
		}
		if question.Kind == punchpolicy.QuestionBreakProvided || question.Kind == punchpolicy.QuestionInjury {
			lower := strings.ToLower(value)
			if lower != "true" && lower != "false" && lower != "yes" && lower != "no" {
				return nil, reject(ErrInvalidRequest, "answers", "", fmt.Sprintf("question %q requires a yes/no answer", id))
			}
		}
		seen[id] = struct{}{}
		out = append(out, ClockOutAnswer{QuestionID: id, Value: value})
	}
	return out, nil
}

func containsQuestionOption(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}

func (s Service) appendClockOutEvidence(ctx context.Context, tenant string, receipt PunchReceipt, req ClockOutRequest, set punchpolicy.QuestionSet, answers []ClockOutAnswer, tip *TipDeclarationEvidence) error {
	if s.Observations == nil || s.IDs == nil {
		return ErrUnavailable
	}
	now := s.now()
	evidence := ClockOutEvidence{
		SchemaVersion: 1, ReceiptObservationID: receipt.ObservationID, TenantID: tenant,
		WorkerRef: receipt.WorkerRef, AssignmentRef: receipt.AssignmentRef, SessionID: receipt.SessionID,
		SiteID: req.SiteID, QuestionSetID: set.ID, QuestionSetVersion: set.Version,
		Jurisdiction: set.Jurisdiction, Answers: append([]ClockOutAnswer(nil), answers...), TipDeclaration: tip,
		RecordedAt: now,
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	digestBytes := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(digestBytes[:])
	key := "clock-out-attestation:" + receipt.ObservationID
	id := s.IDs.Deterministic(tenant, "clock-out-attestation", receipt.ObservationID)
	if strings.TrimSpace(receipt.ObservationID) == "" || strings.TrimSpace(id) == "" || now.IsZero() {
		return ErrInvalidRequest
	}
	_, _, err = s.Observations.AppendObservation(ctx, tenant, ObservationRecord{
		ID: id, TenantID: tenant, WorkerRef: receipt.WorkerRef, AssignmentRef: receipt.AssignmentRef,
		Source: clockOutAttestationSource, EventType: clockOutAttestationEvent,
		IdempotencyKey: key, Digest: digest, CorrectsID: receipt.ObservationID,
		OccurredAt: req.DeviceTime, ReceivedAt: now, Payload: payload,
	})
	return err
}
