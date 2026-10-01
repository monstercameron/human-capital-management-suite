package agentpersonaeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errToolSelectionTrace = errors.New("agentpersonaeval: invalid tool-selection trace")

// ToolSelectionEventKind separates untrusted model output from server-side
// event records. The kind does not authenticate the event source.
type ToolSelectionEventKind string

const (
	// ToolSelectionProposal records only that the model proposed a named tool.
	ToolSelectionProposal ToolSelectionEventKind = "model_proposal"
	// ToolSelectionDenied records a server-side refusal of a proposed skill.
	ToolSelectionDenied ToolSelectionEventKind = "gateway_denied"
	// ToolSelectionAdmitted records a server-side admission before execution.
	ToolSelectionAdmitted ToolSelectionEventKind = "gateway_admitted"
	// ToolSelectionExecuted records a completed runtime skill call.
	ToolSelectionExecuted ToolSelectionEventKind = "runtime_executed"
	// ToolSelectionNoCall records a runtime result that reports no skill call.
	ToolSelectionNoCall ToolSelectionEventKind = "runtime_no_call"
)

// ToolSelectionEvent contains a bounded event from the model/tool boundary.
// ProposalName is never treated as an observed skill. Runtime and gateway
// events require source-owned receipt digests supplied by the executor.
type ToolSelectionEvent struct {
	Sequence      uint32                 `json:"sequence"`
	Kind          ToolSelectionEventKind `json:"kind"`
	ProposalName  string                 `json:"proposal_name,omitempty"`
	Skill         string                 `json:"skill,omitempty"`
	InvocationID  string                 `json:"invocation_id,omitempty"`
	ReceiptDigest string                 `json:"receipt_digest,omitempty"`
}

// ToolSelectionTraceBinding scopes a trace to one synthetic evaluation run,
// fixture case and exact persona version.
type ToolSelectionTraceBinding struct {
	SyntheticTenantID string `json:"synthetic_tenant_id"`
	RunID             string `json:"run_id"`
	CaseID            string `json:"case_id"`
	CaseDigest        string `json:"case_digest"`
	PersonaDigest     string `json:"persona_digest"`
	ModelDigest       string `json:"model_digest"`
}

// ToolSelectionTrace is a sealed, serializable record of tool-selection
// events. A valid seal preserves supplied receipts; it does not authenticate
// their issuer or convert model proposals into runtime observations.
type ToolSelectionTrace struct {
	Binding             ToolSelectionTraceBinding `json:"binding"`
	AllowedSkills       []string                  `json:"allowed_skills"`
	AllowedSkillsDigest string                    `json:"allowed_skills_digest"`
	Events              []ToolSelectionEvent      `json:"events"`
	Digest              string                    `json:"digest"`
}

// NewPersonaCaseToolSelectionTrace copies and seals events under a validated
// synthetic case and persona policy. Receipt authenticity remains the
// responsibility of the runtime boundary that supplies each event.
func NewPersonaCaseToolSelectionTrace(syntheticTenantID, runID string, persona PersonaVersion, testCase TaskCase, events []ToolSelectionEvent) (ToolSelectionTrace, error) {
	if err := validatePersona(persona); err != nil {
		return ToolSelectionTrace{}, fmt.Errorf("%w: persona binding: %v", errToolSelectionTrace, err)
	}
	if err := validateCase(persona, testCase); err != nil {
		return ToolSelectionTrace{}, fmt.Errorf("%w: case binding: %v", errToolSelectionTrace, err)
	}
	caseDigest, err := digestCases([]TaskCase{testCase})
	if err != nil {
		return ToolSelectionTrace{}, fmt.Errorf("%w: case digest: %v", errToolSelectionTrace, err)
	}
	binding := ToolSelectionTraceBinding{
		SyntheticTenantID: syntheticTenantID, RunID: runID, CaseID: testCase.ID,
		CaseDigest: caseDigest, PersonaDigest: persona.PersonaDigest, ModelDigest: persona.ModelDigest,
	}
	return newToolSelectionTrace(binding, persona.ExpectedSkills, events)
}

func newToolSelectionTrace(binding ToolSelectionTraceBinding, allowedSkills []string, events []ToolSelectionEvent) (ToolSelectionTrace, error) {
	if err := validateToolSelectionBinding(binding); err != nil {
		return ToolSelectionTrace{}, err
	}
	allowed, err := canonicalSkillPolicy(allowedSkills)
	if err != nil {
		return ToolSelectionTrace{}, err
	}
	copiedEvents := append([]ToolSelectionEvent(nil), events...)
	if err := validateToolSelectionEvents(copiedEvents, allowed); err != nil {
		return ToolSelectionTrace{}, err
	}
	trace := ToolSelectionTrace{Binding: binding, AllowedSkills: allowed, AllowedSkillsDigest: digestSkillPolicy(allowed), Events: copiedEvents}
	trace.Digest = digestToolSelectionTrace(trace)
	return trace, nil
}

// MatchesPersonaCase checks whether this record binds to the supplied
// persona version and exact synthetic fixture. It does not authenticate event
// receipts or establish that the recorded runtime effects occurred.
func (trace ToolSelectionTrace) MatchesPersonaCase(persona PersonaVersion, testCase TaskCase) bool {
	if trace.Verify() != nil || validatePersona(persona) != nil || validateCase(persona, testCase) != nil {
		return false
	}
	caseDigest, err := digestCases([]TaskCase{testCase})
	if err != nil || trace.Binding.CaseID != testCase.ID || trace.Binding.CaseDigest != caseDigest ||
		trace.Binding.PersonaDigest != persona.PersonaDigest || trace.Binding.ModelDigest != persona.ModelDigest {
		return false
	}
	allowed, err := canonicalSkillPolicy(persona.ExpectedSkills)
	return err == nil && equalCaseStrings(trace.AllowedSkills, allowed)
}

// Verify checks the trace seal, exact skill-policy digest and event structure.
func (trace ToolSelectionTrace) Verify() error {
	if err := validateToolSelectionBinding(trace.Binding); err != nil {
		return err
	}
	allowed, err := canonicalSkillPolicy(trace.AllowedSkills)
	if err != nil || !equalCaseStrings(allowed, trace.AllowedSkills) || trace.AllowedSkillsDigest != digestSkillPolicy(allowed) {
		return fmt.Errorf("%w: allowed-skill policy is invalid", errToolSelectionTrace)
	}
	if err := validateToolSelectionEvents(trace.Events, allowed); err != nil {
		return err
	}
	if trace.Digest == "" || trace.Digest != digestToolSelectionTrace(trace) {
		return fmt.Errorf("%w: trace seal is invalid", errToolSelectionTrace)
	}
	return nil
}

func validateToolSelectionBinding(binding ToolSelectionTraceBinding) error {
	if !strings.HasPrefix(binding.SyntheticTenantID, "synthetic-") || strings.TrimSpace(binding.RunID) == "" ||
		strings.TrimSpace(binding.CaseID) == "" || !validDigest(binding.CaseDigest) ||
		!validDigest(binding.PersonaDigest) || !validDigest(binding.ModelDigest) {
		return fmt.Errorf("%w: synthetic tenant, run, case and version digests are required", errToolSelectionTrace)
	}
	return nil
}

func validateToolSelectionEvents(events []ToolSelectionEvent, allowed []string) error {
	if len(events) == 0 || len(events) > 64 {
		return fmt.Errorf("%w: trace must contain 1 to 64 events", errToolSelectionTrace)
	}
	admitted := make(map[string]string)
	for i, event := range events {
		if err := validateToolSelectionEvent(event, allowed); err != nil {
			return err
		}
		if i > 0 && events[i-1].Sequence >= event.Sequence {
			return fmt.Errorf("%w: event sequence must increase", errToolSelectionTrace)
		}
		switch event.Kind {
		case ToolSelectionAdmitted:
			if _, duplicate := admitted[event.InvocationID]; duplicate {
				return fmt.Errorf("%w: duplicate invocation admission", errToolSelectionTrace)
			}
			admitted[event.InvocationID] = event.Skill
		case ToolSelectionExecuted:
			if admitted[event.InvocationID] != event.Skill {
				return fmt.Errorf("%w: execution has no matching prior admission", errToolSelectionTrace)
			}
			delete(admitted, event.InvocationID)
		}
	}
	return nil
}

func canonicalSkillPolicy(skills []string) ([]string, error) {
	if len(skills) == 0 {
		return nil, fmt.Errorf("%w: allowed-skill policy is empty", errToolSelectionTrace)
	}
	result := append([]string(nil), skills...)
	for _, skill := range result {
		if strings.TrimSpace(skill) == "" || skill != strings.TrimSpace(skill) {
			return nil, fmt.Errorf("%w: allowed skill is invalid", errToolSelectionTrace)
		}
	}
	sort.Strings(result)
	for i := 1; i < len(result); i++ {
		if result[i-1] == result[i] {
			return nil, fmt.Errorf("%w: duplicate allowed skill", errToolSelectionTrace)
		}
	}
	return result, nil
}

func validateToolSelectionEvent(event ToolSelectionEvent, allowed []string) error {
	if event.Sequence == 0 {
		return fmt.Errorf("%w: event sequence must be positive", errToolSelectionTrace)
	}
	switch event.Kind {
	case ToolSelectionProposal:
		if strings.TrimSpace(event.ProposalName) == "" || event.ProposalName != strings.TrimSpace(event.ProposalName) || event.Skill != "" || event.InvocationID != "" || event.ReceiptDigest != "" {
			return fmt.Errorf("%w: model proposal must remain separate from observed tool use", errToolSelectionTrace)
		}
	case ToolSelectionDenied:
		if strings.TrimSpace(event.Skill) == "" || strings.TrimSpace(event.InvocationID) == "" || !validDigest(event.ReceiptDigest) || event.ProposalName != "" {
			return fmt.Errorf("%w: gateway denial must bind a proposed skill, invocation and receipt", errToolSelectionTrace)
		}
	case ToolSelectionAdmitted:
		if !contains(allowed, event.Skill) || strings.TrimSpace(event.InvocationID) == "" || !validDigest(event.ReceiptDigest) || event.ProposalName != "" {
			return fmt.Errorf("%w: gateway event must bind an allowed skill, invocation and receipt", errToolSelectionTrace)
		}
	case ToolSelectionExecuted:
		if !contains(allowed, event.Skill) || strings.TrimSpace(event.InvocationID) == "" || !validDigest(event.ReceiptDigest) || event.ProposalName != "" {
			return fmt.Errorf("%w: execution event must bind an allowed skill, invocation and receipt", errToolSelectionTrace)
		}
	case ToolSelectionNoCall:
		if event.Skill != "" || event.ProposalName != "" || event.InvocationID == "" || !validDigest(event.ReceiptDigest) {
			return fmt.Errorf("%w: no-call observation must bind a runtime receipt", errToolSelectionTrace)
		}
	default:
		return fmt.Errorf("%w: unknown event kind", errToolSelectionTrace)
	}
	return nil
}

func digestSkillPolicy(skills []string) string {
	encoded, _ := json.Marshal(skills)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-allowed-skills/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestToolSelectionTrace(trace ToolSelectionTrace) string {
	trace.Digest = ""
	encoded, _ := json.Marshal(trace)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-tool-selection-trace/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
