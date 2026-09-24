// Package userflow owns the machine-readable experience contract shared by
// flow documentation, conformance tooling, and generated views.
package userflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Stage string

type ParticipantRole string

const (
	HumanSelf                 ParticipantRole = "HUMAN_SELF"
	Manager                   ParticipantRole = "MANAGER"
	HRSpecialist              ParticipantRole = "HR_SPECIALIST"
	ReviewerOrApprover        ParticipantRole = "REVIEWER_OR_APPROVER"
	CandidateOrExternalPerson ParticipantRole = "CANDIDATE_OR_EXTERNAL_PERSON"
	DelegateOrRepresentative  ParticipantRole = "DELEGATE_OR_REPRESENTATIVE"
	CaseParticipant           ParticipantRole = "CASE_PARTICIPANT"
	HRISOrPayrollOperator     ParticipantRole = "HRIS_OR_PAYROLL_OPERATOR"
	SupportOrIncidentOperator ParticipantRole = "SUPPORT_OR_INCIDENT_OPERATOR"
)

func ParticipantRoles() []ParticipantRole {
	return []ParticipantRole{HumanSelf, Manager, HRSpecialist, ReviewerOrApprover, CandidateOrExternalPerson, DelegateOrRepresentative, CaseParticipant, HRISOrPayrollOperator, SupportOrIncidentOperator}
}

func (r ParticipantRole) Valid() bool {
	for _, role := range ParticipantRoles() {
		if r == role {
			return true
		}
	}
	return false
}

type Status string

const (
	FlowCandidate   Status = "FLOW_CANDIDATE"
	FlowDraft       Status = "FLOW_DRAFT"
	FlowContracted  Status = "FLOW_CONTRACTED"
	FlowImplemented Status = "FLOW_IMPLEMENTED"
	FlowVerified    Status = "FLOW_VERIFIED"
)

func (s Status) Valid() bool {
	switch s {
	case FlowCandidate, FlowDraft, FlowContracted, FlowImplemented, FlowVerified:
		return true
	default:
		return false
	}
}

const (
	Discover  Stage = "DISCOVER"
	Orient    Stage = "ORIENT"
	Collect   Stage = "COLLECT"
	Validate  Stage = "VALIDATE"
	Simulate  Stage = "SIMULATE"
	Compare   Stage = "COMPARE"
	Confirm   Stage = "CONFIRM"
	Submit    Stage = "SUBMIT"
	Review    Stage = "REVIEW"
	Wait      Stage = "WAIT"
	Track     Stage = "TRACK"
	Replan    Stage = "REPLAN"
	Execute   Stage = "EXECUTE"
	Reconcile Stage = "RECONCILE"
	Repair    Stage = "REPAIR"
	Complete  Stage = "COMPLETE"
	Correct   Stage = "CORRECT"
	Appeal    Stage = "APPEAL"
)

var stageVocabulary = [...]Stage{Discover, Orient, Collect, Validate, Simulate, Compare, Confirm, Submit, Review, Wait, Track, Replan, Execute, Reconcile, Repair, Complete, Correct, Appeal}

func (s Stage) Valid() bool {
	for _, v := range stageVocabulary {
		if s == v {
			return true
		}
	}
	return false
}

// Stages returns the canonical experience-stage vocabulary in documentation order.
func Stages() []Stage { return append([]Stage(nil), stageVocabulary[:]...) }

type Participant struct {
	ID             string `json:"id"`
	Role           string `json:"role"`
	Persona        string `json:"persona"`
	Relationship   string `json:"relationship"`
	DecisionRights string `json:"decision_rights"`
	Representation string `json:"representation"`
	Delegation     string `json:"delegation"`
}

type SemanticReferences struct {
	BusinessIntent string `json:"business_intent"`
	Workflow       string `json:"workflow"`
	VerticalSlice  string `json:"vertical_slice"`
	Action         string `json:"action"`
	Result         string `json:"result"`
}

type EntryPaths struct {
	Entry        string `json:"entry"`
	Discovery    string `json:"discovery"`
	Resume       string `json:"resume"`
	DeepLink     string `json:"deep_link"`
	Notification string `json:"notification"`
}

type StatePresentation struct {
	State      string `json:"state"`
	Understand string `json:"understand"`
	Behavior   string `json:"behavior"`
}

type FlowStage struct {
	ID                      string `json:"id"`
	Stage                   Stage  `json:"stage"`
	ParticipantGoal         string `json:"participant_goal"`
	Surface                 string `json:"surface"`
	SystemState             string `json:"system_state"`
	ParticipantVisibleState string `json:"participant_visible_state"`
	AvailableActions        string `json:"available_actions"`
	Input                   string `json:"input"`
	Validation              string `json:"validation"`
	CapabilityTransition    string `json:"capability_transition"`
	SemanticActionReference string `json:"semantic_action_reference"`
	SemanticResultReference string `json:"semantic_result_reference"`
	VisibleResult           string `json:"visible_result"`
	Evidence                string `json:"evidence"`
	Decision                string `json:"decision"`
	ErrorRecovery           string `json:"error_recovery"`
}

type NotApplicable struct {
	Reason string `json:"reason"`
	Owner  string `json:"owner"`
}

// UserFlowRecord is the complete semantic description of one participant
// journey. Empty values are incomplete; NOT_APPLICABLE requires a reason and owner.
type UserFlowRecord struct {
	FlowID               string                   `json:"flow_id"`
	Version              string                   `json:"version"`
	Status               string                   `json:"status"`
	Owner                string                   `json:"owner"`
	Title                string                   `json:"title"`
	JobToBeDone          string                   `json:"job_to_be_done"`
	SuccessDefinition    string                   `json:"success_definition"`
	RootBusinessIntent   string                   `json:"root_business_intent"`
	ChildBusinessIntents []string                 `json:"child_business_intents"`
	References           SemanticReferences       `json:"references"`
	PrimaryParticipant   Participant              `json:"primary_participant"`
	OtherParticipants    []Participant            `json:"other_participants"`
	IdentityAssurance    string                   `json:"identity_assurance"`
	SessionAssumptions   string                   `json:"session_assumptions"`
	Entry                EntryPaths               `json:"entry_paths"`
	Surfaces             []string                 `json:"surfaces"`
	Channels             []string                 `json:"channels"`
	Devices              []string                 `json:"devices"`
	Preconditions        string                   `json:"preconditions"`
	UnavailableAction    string                   `json:"unavailable_action_behavior"`
	VisibleFacts         string                   `json:"visible_facts"`
	Provenance           string                   `json:"provenance"`
	Freshness            string                   `json:"freshness"`
	MaskedFacts          string                   `json:"masked_facts"`
	HiddenFacts          string                   `json:"hidden_facts"`
	SummaryOnlyFacts     string                   `json:"summary_only_facts"`
	RequestedInput       string                   `json:"requested_input"`
	ServerResolvedTruth  string                   `json:"server_resolved_truth"`
	Forms                string                   `json:"forms"`
	Documents            string                   `json:"documents"`
	EvidenceCompartments string                   `json:"evidence_compartments"`
	Stages               []FlowStage              `json:"stages"`
	StateMatrix          []StatePresentation      `json:"state_presentation_matrix"`
	Cancellation         string                   `json:"cancellation_route"`
	Correction           string                   `json:"correction_route"`
	Supersession         string                   `json:"supersession_route"`
	Completion           string                   `json:"completion_behavior"`
	FollowUp             string                   `json:"follow_up_behavior"`
	Locale               string                   `json:"locale_rules"`
	Accessibility        string                   `json:"accessibility"`
	Accommodation        string                   `json:"accommodation_and_manual_route"`
	Responsive           string                   `json:"responsive_behavior"`
	Offline              string                   `json:"offline_behavior"`
	Privacy              string                   `json:"privacy"`
	Safety               string                   `json:"safety_and_non_disclosure"`
	AnalyticsEvents      string                   `json:"analytics_events"`
	ProhibitedTelemetry  string                   `json:"prohibited_telemetry"`
	Scenarios            []string                 `json:"scenarios"`
	Oracles              []string                 `json:"oracles"`
	OpenFindings         []string                 `json:"open_findings"`
	TodoLinks            []string                 `json:"todo_links"`
	Evidence             []string                 `json:"evidence"`
	Phase                string                   `json:"phase"`
	MaximalConfiguration string                   `json:"maximal_configuration_applicability"`
	NotApplicable        map[string]NotApplicable `json:"not_applicable"`
}

func required(s string) bool { return strings.TrimSpace(s) != "" }

func (r UserFlowRecord) field(field, value string) error {
	na, hasNA := r.NotApplicable[field]
	if hasNA {
		if required(value) {
			return fmt.Errorf("user flow %s: %s is both specified and NOT_APPLICABLE", r.FlowID, field)
		}
		if !required(na.Reason) || !required(na.Owner) {
			return fmt.Errorf("user flow %s: NOT_APPLICABLE %s requires reason and owner", r.FlowID, field)
		}
		return nil
	}
	if !required(value) {
		return fmt.Errorf("user flow %s: missing %s", r.FlowID, field)
	}
	return nil
}

func (r UserFlowRecord) list(field string, values []string) error {
	if len(values) == 0 {
		if na, ok := r.NotApplicable[field]; ok && required(na.Reason) && required(na.Owner) {
			return nil
		}
		return fmt.Errorf("user flow %s: missing %s", r.FlowID, field)
	}
	for i, value := range values {
		if !required(value) {
			return fmt.Errorf("user flow %s: empty %s[%d]", r.FlowID, field, i)
		}
	}
	return nil
}

func (r UserFlowRecord) Validate() error {
	if !Status(r.Status).Valid() {
		return fmt.Errorf("user flow %s: unknown status %q", r.FlowID, r.Status)
	}
	fields := map[string]string{
		"flow_id": r.FlowID, "version": r.Version, "status": r.Status, "owner": r.Owner,
		"title": r.Title, "job_to_be_done": r.JobToBeDone, "success_definition": r.SuccessDefinition,
		"root_business_intent": r.RootBusinessIntent, "workflow_reference": r.References.Workflow,
		"vertical_slice_reference": r.References.VerticalSlice, "primary_participant.id": r.PrimaryParticipant.ID,
		"primary_participant.role": r.PrimaryParticipant.Role, "primary_participant.persona": r.PrimaryParticipant.Persona, "primary_participant.relationship": r.PrimaryParticipant.Relationship,
		"primary_participant.decision_rights": r.PrimaryParticipant.DecisionRights,
		"primary_participant.representation":  r.PrimaryParticipant.Representation,
		"primary_participant.delegation":      r.PrimaryParticipant.Delegation,
		"identity_assurance":                  r.IdentityAssurance, "session_assumptions": r.SessionAssumptions,
		"entry": r.Entry.Entry, "discovery": r.Entry.Discovery, "resume": r.Entry.Resume,
		"preconditions": r.Preconditions, "unavailable_action_behavior": r.UnavailableAction,
		"visible_facts": r.VisibleFacts, "provenance": r.Provenance, "freshness": r.Freshness,
		"masked_facts": r.MaskedFacts, "hidden_facts": r.HiddenFacts, "summary_only_facts": r.SummaryOnlyFacts,
		"requested_input": r.RequestedInput, "server_resolved_truth": r.ServerResolvedTruth,
		"forms": r.Forms, "documents": r.Documents, "evidence_compartments": r.EvidenceCompartments,
		"cancellation_route": r.Cancellation, "correction_route": r.Correction, "supersession_route": r.Supersession,
		"completion_behavior": r.Completion, "follow_up_behavior": r.FollowUp, "locale_rules": r.Locale,
		"accessibility": r.Accessibility, "accommodation_and_manual_route": r.Accommodation,
		"responsive_behavior": r.Responsive, "offline_behavior": r.Offline, "privacy": r.Privacy,
		"safety_and_non_disclosure": r.Safety, "analytics_events": r.AnalyticsEvents,
		"prohibited_telemetry": r.ProhibitedTelemetry, "phase": r.Phase,
		"maximal_configuration_applicability": r.MaximalConfiguration,
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, field := range keys {
		if err := r.field(field, fields[field]); err != nil {
			return err
		}
	}
	knownNotApplicable := map[string]bool{
		"business_intent_reference": true, "action_reference": true, "result_reference": true,
		"child_business_intents": true, "other_participants": true, "surfaces": true,
		"channels": true, "devices": true, "deep_link": true, "notification": true,
		"scenarios": true, "oracles": true, "todo_links": true, "evidence": true,
	}
	for field := range fields {
		knownNotApplicable[field] = true
	}
	for field, na := range r.NotApplicable {
		if !knownNotApplicable[field] || !required(na.Reason) || !required(na.Owner) {
			return fmt.Errorf("user flow %s: invalid NOT_APPLICABLE entry %q", r.FlowID, field)
		}
	}
	if !ParticipantRole(r.PrimaryParticipant.Role).Valid() {
		return fmt.Errorf("user flow %s: unknown primary participant role %q", r.FlowID, r.PrimaryParticipant.Role)
	}
	for field, value := range map[string]string{"business_intent_reference": r.References.BusinessIntent, "action_reference": r.References.Action, "result_reference": r.References.Result} {
		if !required(value) {
			return fmt.Errorf("user flow %s: missing %s", r.FlowID, field)
		}
	}
	for _, field := range []string{"child_business_intents", "other_participants", "surfaces", "channels", "devices", "deep_link", "notification", "scenarios", "oracles", "todo_links", "evidence"} {
		var err error
		switch field {
		case "child_business_intents":
			err = r.list(field, r.ChildBusinessIntents)
		case "other_participants":
			if len(r.OtherParticipants) == 0 {
				if na, ok := r.NotApplicable[field]; !ok || !required(na.Reason) || !required(na.Owner) {
					err = fmt.Errorf("user flow %s: missing %s", r.FlowID, field)
				}
			} else {
				participantIDs := map[string]bool{r.PrimaryParticipant.ID: true}
				for i, participant := range r.OtherParticipants {
					if !required(participant.ID) || !required(participant.Persona) || !required(participant.Relationship) || !required(participant.DecisionRights) || !required(participant.Representation) || !required(participant.Delegation) || !ParticipantRole(participant.Role).Valid() {
						err = fmt.Errorf("user flow %s: incomplete or invalid other_participants[%d]", r.FlowID, i)
						break
					}
					if participantIDs[participant.ID] {
						err = fmt.Errorf("user flow %s: duplicate participant id %q", r.FlowID, participant.ID)
						break
					}
					participantIDs[participant.ID] = true
				}
			}
		case "surfaces":
			err = r.list(field, r.Surfaces)
		case "channels":
			err = r.list(field, r.Channels)
		case "devices":
			err = r.list(field, r.Devices)
		case "deep_link":
			err = r.field(field, r.Entry.DeepLink)
		case "notification":
			err = r.field(field, r.Entry.Notification)
		case "scenarios":
			err = r.list(field, r.Scenarios)
		case "oracles":
			err = r.list(field, r.Oracles)
		case "todo_links":
			err = r.list(field, r.TodoLinks)
		case "evidence":
			err = r.list(field, r.Evidence)
		}
		if err != nil {
			return err
		}
	}
	if r.Status == "FLOW_CONTRACTED" && len(r.OpenFindings) > 0 {
		return errors.New("user flow: FLOW_CONTRACTED cannot have open findings")
	}
	if len(r.Stages) == 0 {
		return errors.New("user flow: stages are required")
	}
	seen := map[string]bool{}
	for i, s := range r.Stages {
		if !required(s.ID) || seen[s.ID] {
			return fmt.Errorf("user flow: invalid or duplicate stage id at index %d", i)
		}
		seen[s.ID] = true
		if !s.Stage.Valid() {
			return fmt.Errorf("user flow: unknown stage %q", s.Stage)
		}
		stageFields := map[string]string{"participant_goal": s.ParticipantGoal, "surface": s.Surface, "system_state": s.SystemState, "participant_visible_state": s.ParticipantVisibleState, "available_actions": s.AvailableActions, "input": s.Input, "validation": s.Validation, "capability_transition": s.CapabilityTransition, "visible_result": s.VisibleResult, "evidence": s.Evidence, "decision": s.Decision, "error_recovery": s.ErrorRecovery}
		for name, value := range stageFields {
			if !required(value) {
				return fmt.Errorf("user flow stage %s: missing %s", s.ID, name)
			}
		}
		if !required(s.SemanticActionReference) || !required(s.SemanticResultReference) {
			return fmt.Errorf("user flow stage %s: semantic action and result references are required", s.ID)
		}
	}
	if len(r.StateMatrix) == 0 {
		return errors.New("user flow: state presentation matrix is required")
	}
	stateSeen := map[string]bool{}
	for _, s := range r.StateMatrix {
		if !required(s.State) || !required(s.Understand) || !required(s.Behavior) {
			return errors.New("user flow: incomplete state presentation matrix")
		}
		if stateSeen[s.State] {
			return fmt.Errorf("user flow: duplicate state %q", s.State)
		}
		stateSeen[s.State] = true
	}
	return nil
}

// CanonicalBytes returns the validated JSON representation used for shared digests.
func CanonicalBytes(r UserFlowRecord) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func (r UserFlowRecord) CanonicalDigest() (string, error) {
	b, err := CanonicalBytes(r)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
