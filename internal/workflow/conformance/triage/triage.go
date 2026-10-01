// Package triage proves agent-assisted HR case triage cannot become an
// employment-decision path (CONF-023): quarantine and minimum-necessary
// redaction precede read-only agent execution, typed cited proposals plus
// deterministic rules route high-risk and unknown cases to a specialist,
// and every failure takes the deterministic manual path. Final evidence
// always states the agent never made an employment decision.
package triage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Routes: the closed routing vocabulary.
const (
	RouteSpecialist     = "ROUTE_SPECIALIST"
	RouteGovernedCreate = "GOVERNED_CREATE"
	RouteManual         = "MANUAL"
	RouteHold           = "HOLD"
)

// ServingContractID identifies the read-only HR case triage contract
// composed by the shipped application cell.
const ServingContractID = "hcmnext.conformance.agent-case-triage/v1"

// ValidateServingContract exercises the deterministic no-proposer fallback.
// A served cell must retain a manual path and an explicit no-employment-
// decision attestation when the agent path is unavailable.
func ValidateServingContract() error {
	verdict, err := Triage(CaseRequest{
		ID: "serving-contract", Subject: "policy inquiry",
		Messages: []Message{{Role: "user", Text: "Please route this question."}},
		Taxonomy: TaxonomyStandardLeave,
	}, nil, nil)
	if err != nil {
		return fmt.Errorf("triage: serving contract fallback: %w", err)
	}
	if verdict.Route != RouteManual || !verdict.AgentMadeNoDecision {
		return fmt.Errorf("triage: serving contract fallback is unsafe: %#v", verdict)
	}
	if err := verdict.Verify(); err != nil {
		return fmt.Errorf("triage: serving contract seal: %w", err)
	}
	return nil
}

// Cited taxonomy: only cited categories route; everything else is unknown.
const (
	TaxonomyStandardLeave        = "standard/leave-balance"
	TaxonomyStandardPayroll      = "standard/payroll-question"
	TaxonomyStandardBenefits     = "standard/benefits-info"
	TaxonomySpecialHarassment    = "specialist/harassment"
	TaxonomySpecialRetaliation   = "specialist/retaliation"
	TaxonomySpecialAccommodation = "specialist/accommodation"
	TaxonomySpecialTermination   = "specialist/termination-risk"
)

var (
	// ErrHostileRoles reports client-supplied system or tool messages.
	ErrHostileRoles = errors.New("triage: client system/tool messages are refused")
	// ErrQuarantine reports an attachment that never cleared quarantine.
	ErrQuarantine = errors.New("triage: unquarantined attachment holds the case")
	// ErrWriteAttempt reports a model attempt to invoke a write.
	ErrWriteAttempt = errors.New("triage: model write attempt takes the manual path")
	// ErrUncitedTaxonomy reports a missing or invalid taxonomy citation.
	ErrUncitedTaxonomy = errors.New("triage: uncited or invalid taxonomy routes to specialist")
	// ErrEmploymentDecision reports a proposal carrying a decision effect.
	ErrEmploymentDecision = errors.New("triage: agent proposals never carry employment decisions")
)

// protectedSignals force specialist routing: they are never auto-decided
// and never enter prompt material unredacted.
var protectedSignals = []string{
	"pregnant", "disability", "union", "retaliation",
	"harassment", "accommodation", "termination", "protected",
}

// Attachment is one uploaded artifact with its quarantine verdict.
type Attachment struct {
	ID          string
	Bytes       string
	Quarantined bool
	Clean       bool
}

// Message is one conversation turn. Only Role "user" is admissible from
// the client: system and tool messages are server-issued.
type Message struct {
	Role string
	Text string
}

// CaseRequest is one triage intake.
type CaseRequest struct {
	ID       string
	Subject  string
	Messages []Message
	Files    []Attachment
	Taxonomy string
}

// Proposal is the model's read-only output: a routing recommendation with
// zero decision authority.
type Proposal struct {
	Route              string
	Taxonomy           string
	Cited              bool
	EmploymentDecision bool
	WriteAttempts      []string
	Failure            string
}

// Proposer executes the read-only agent step behind quarantine and
// redaction. Implementations never execute writes.
type Proposer interface {
	Propose(prompt string) (Proposal, error)
}

// TelemetrySink records delivery evidence. Only digests enter default
// telemetry: raw prompts and outputs never do.
type TelemetrySink struct {
	mu     sync.Mutex
	events []string
}

// Record stores one digest-only event.
func (s *TelemetrySink) Record(event, digest string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event+":"+digest)
}

// Events returns recorded events in order.
func (s *TelemetrySink) Events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func digestMaterial(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verdict is one deterministic triage outcome with its evidence.
type Verdict struct {
	CaseID              string
	Route               string
	Redactions          int
	PromptDigest        string
	ProtectedRouted     bool
	AgentMadeNoDecision bool
	ManualCause         string
	Evidence            []string
	Digest              string
}

func verdictDigest(v Verdict) string {
	return digestMaterial("triage-verdict", v.CaseID, v.Route, v.PromptDigest, v.ManualCause, strings.Join(v.Evidence, "\x00"))
}

func knownTaxonomy(taxonomy string) bool {
	switch taxonomy {
	case TaxonomyStandardLeave, TaxonomyStandardPayroll, TaxonomyStandardBenefits,
		TaxonomySpecialHarassment, TaxonomySpecialRetaliation,
		TaxonomySpecialAccommodation, TaxonomySpecialTermination:
		return true
	default:
		return false
	}
}

func specialistTaxonomy(taxonomy string) bool {
	return strings.HasPrefix(taxonomy, "specialist/")
}

func redact(text string) (string, int) {
	out := text
	count := 0
	for _, signal := range protectedSignals {
		for {
			lower := strings.ToLower(out)
			idx := strings.Index(lower, signal)
			if idx < 0 {
				break
			}
			out = out[:idx] + "[REDACTED]" + out[idx+len(signal):]
			count++
		}
	}
	return out, count
}

func protectedPresent(text string) bool {
	lower := strings.ToLower(text)
	for _, signal := range protectedSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// Triage executes the guarded pipeline. Failures return a MANUAL verdict
// carrying their cause; only hostile roles and employment decisions are
// hard errors, and both leave zero proposal behind.
func Triage(request CaseRequest, proposer Proposer, telemetry *TelemetrySink) (Verdict, error) {
	verdict := Verdict{CaseID: request.ID, AgentMadeNoDecision: true}
	chronicle := func(format string, args ...any) {
		verdict.Evidence = append(verdict.Evidence, fmt.Sprintf("t%d: "+format, append([]any{len(verdict.Evidence)}, args...)...))
	}
	manual := func(cause string) (Verdict, error) {
		verdict.Route = RouteManual
		verdict.ManualCause = cause
		chronicle("failure takes deterministic manual path: %s", cause)
		verdict.Digest = verdictDigest(verdict)
		return verdict, nil
	}
	for _, message := range request.Messages {
		if message.Role == "system" || message.Role == "tool" {
			return Verdict{}, fmt.Errorf("%w: role %q", ErrHostileRoles, message.Role)
		}
	}
	for _, file := range request.Files {
		if !file.Quarantined || !file.Clean {
			chronicle("attachment %s never cleared quarantine", file.ID)
			verdict.Route = RouteHold
			verdict.Digest = verdictDigest(verdict)
			return verdict, nil
		}
	}
	var prompt strings.Builder
	subject, redactions := redact(request.Subject)
	verdict.Redactions = redactions
	fmt.Fprintf(&prompt, "subject: %s\ntaxonomy: %s\n", subject, request.Taxonomy)
	for _, message := range request.Messages {
		text, count := redact(message.Text)
		verdict.Redactions += count
		fmt.Fprintf(&prompt, "user: %s\n", text)
	}
	material := prompt.String()
	for _, file := range request.Files {
		if file.Bytes != "" && strings.Contains(material, file.Bytes) {
			return Verdict{}, fmt.Errorf("%w: attachment %s", ErrQuarantine, file.ID)
		}
	}
	verdict.PromptDigest = digestMaterial(material)
	chronicle("quarantine and redaction precede read-only execution (%d redactions)", verdict.Redactions)
	if telemetry != nil {
		telemetry.Record("triage-prompt", verdict.PromptDigest)
	}
	if proposer == nil {
		return manual("no proposer: kill-switch fallback")
	}
	proposal, err := proposer.Propose(material)
	if err != nil {
		return manual("proposer failure: " + err.Error())
	}
	if len(proposal.WriteAttempts) > 0 {
		chronicle("model write attempt refused: %q", proposal.WriteAttempts[0])
		return manual(ErrWriteAttempt.Error())
	}
	if proposal.EmploymentDecision {
		return Verdict{}, ErrEmploymentDecision
	}
	if !proposal.Cited || !knownTaxonomy(proposal.Taxonomy) {
		chronicle("uncited or invalid taxonomy routes to specialist")
		verdict.Route = RouteSpecialist
		verdict.Digest = verdictDigest(verdict)
		return verdict, nil
	}
	if protectedPresent(request.Subject) {
		verdict.ProtectedRouted = true
	}
	switch {
	case specialistTaxonomy(proposal.Taxonomy) || verdict.ProtectedRouted || proposal.Taxonomy != request.Taxonomy:
		verdict.Route = RouteSpecialist
		chronicle("high-risk, protected or disputed case routes to specialist")
	case proposal.Failure != "":
		return manual(proposal.Failure)
	default:
		verdict.Route = RouteGovernedCreate
		chronicle("standard cited case routes to governed creation")
	}
	verdict.Digest = verdictDigest(verdict)
	return verdict, nil
}

// Verify recomputes the verdict seal.
func (v Verdict) Verify() error {
	if v.Digest == "" || verdictDigest(v) != v.Digest {
		return errors.New("triage: verdict seal is broken")
	}
	if !v.AgentMadeNoDecision {
		return errors.New("triage: verdict must state the agent made no employment decision")
	}
	return nil
}
