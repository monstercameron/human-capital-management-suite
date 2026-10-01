package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// BoardProposalSchemaVersion1 is the first wire-stable proposal shape. A
// proposal is input to preview and validation only; it is never live project
// configuration.
const BoardProposalSchemaVersion1 uint32 = 1

type BoardProposalCapability string

const (
	BoardCapabilityStatuses    BoardProposalCapability = "WORKFLOW_STATUSES"
	BoardCapabilityTransitions BoardProposalCapability = "WORKFLOW_TRANSITIONS"
	BoardCapabilityTaskTypes   BoardProposalCapability = "TASK_TYPES"
	BoardCapabilityTaskFields  BoardProposalCapability = "TASK_FIELDS"
	BoardCapabilityBoardViews  BoardProposalCapability = "BOARD_VIEWS"
	BoardCapabilityListViews   BoardProposalCapability = "LIST_VIEWS"
	BoardCapabilitySampleTasks BoardProposalCapability = "SAMPLE_TASKS"
)

var (
	ErrInvalidBoardProposal   = errors.New("project: invalid board proposal")
	ErrUnsupportedProposal    = errors.New("project: unsupported board proposal capability")
	ErrLiveSampleTask         = errors.New("project: board proposal sample task must be synthetic")
	ErrUnsafeProposalMaterial = errors.New("project: board proposal contains unsupported material")
)

// BoardProposal is deliberately narrower than project configuration. It
// describes enabled, reviewable primitives and sample data, never scripts,
// permissions, cycles, or live task mutations.
type BoardProposal struct {
	SchemaVersion       uint32                    `json:"schema_version"`
	EnabledCapabilities []BoardProposalCapability `json:"enabled_capabilities"`
	Workflow            BoardWorkflowProposal     `json:"workflow"`
	Views               []BoardViewProposal       `json:"views"`
	SampleTasks         []BoardSampleTask         `json:"sample_tasks"`
	Rationale           string                    `json:"rationale"`
	Assumptions         []string                  `json:"assumptions"`
	Unknowns            []string                  `json:"unknowns"`
	// These fields make an attempted expansion explicit in the typed schema;
	// Validate rejects them rather than silently treating them as executable.
	Cycles      []string `json:"cycles,omitempty"`
	Scripts     []string `json:"scripts,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type BoardWorkflowProposal struct {
	Statuses    []BoardProposalStatus     `json:"statuses"`
	Transitions []BoardProposalTransition `json:"transitions"`
	TaskTypes   []BoardProposalTaskType   `json:"task_types"`
	Fields      []BoardProposalField      `json:"fields"`
}

type BoardProposalStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BoardProposalTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type BoardProposalTaskType struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	InitialStatus string `json:"initial_status"`
}

type BoardProposalField struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type BoardViewProposal struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	StatusIDs []string `json:"status_ids"`
}

type BoardSampleTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	StatusID  string `json:"status_id"`
	Synthetic bool   `json:"synthetic"`
}

// Validate rejects anything outside the enabled vocabulary. It is safe to
// call before persistence because it has no side effects and does not mutate
// the proposal.
func (p BoardProposal) Validate() error {
	if p.SchemaVersion != BoardProposalSchemaVersion1 {
		return fmt.Errorf("%w: schema version %d", ErrInvalidBoardProposal, p.SchemaVersion)
	}
	capabilities := make(map[BoardProposalCapability]bool, len(p.EnabledCapabilities))
	for _, capability := range p.EnabledCapabilities {
		if !validBoardProposalCapability(capability) {
			return fmt.Errorf("%w: %q", ErrUnsupportedProposal, capability)
		}
		if capabilities[capability] {
			return fmt.Errorf("%w: duplicate capability %q", ErrInvalidBoardProposal, capability)
		}
		capabilities[capability] = true
	}
	if len(p.Cycles) != 0 || len(p.Scripts) != 0 || len(p.Permissions) != 0 {
		return ErrUnsafeProposalMaterial
	}
	if err := validateProposalStrings(p.Assumptions, "assumption"); err != nil {
		return err
	}
	if err := validateProposalStrings(p.Unknowns, "unknown"); err != nil {
		return err
	}

	statuses := make(map[string]bool, len(p.Workflow.Statuses))
	if len(p.Workflow.Statuses) != 0 && !capabilities[BoardCapabilityStatuses] {
		return missingProposalCapability(BoardCapabilityStatuses)
	}
	for _, status := range p.Workflow.Statuses {
		if !validProposalID(status.ID) || strings.TrimSpace(status.Name) == "" || statuses[status.ID] {
			return fmt.Errorf("%w: invalid status", ErrInvalidBoardProposal)
		}
		statuses[status.ID] = true
	}
	if len(p.Workflow.Transitions) != 0 && !capabilities[BoardCapabilityTransitions] {
		return missingProposalCapability(BoardCapabilityTransitions)
	}
	graph := make(map[string][]string, len(statuses))
	for _, transition := range p.Workflow.Transitions {
		if !statuses[transition.From] || !statuses[transition.To] || transition.From == transition.To {
			return fmt.Errorf("%w: invalid transition", ErrInvalidBoardProposal)
		}
		graph[transition.From] = append(graph[transition.From], transition.To)
	}
	if hasProposalCycle(graph, statuses) {
		return fmt.Errorf("%w: workflow cycles are not enabled", ErrInvalidBoardProposal)
	}
	if len(p.Workflow.TaskTypes) != 0 && !capabilities[BoardCapabilityTaskTypes] {
		return missingProposalCapability(BoardCapabilityTaskTypes)
	}
	for _, typ := range p.Workflow.TaskTypes {
		if !validProposalID(typ.ID) || strings.TrimSpace(typ.Name) == "" || !statuses[typ.InitialStatus] {
			return fmt.Errorf("%w: invalid task type", ErrInvalidBoardProposal)
		}
	}
	if len(p.Workflow.Fields) != 0 && !capabilities[BoardCapabilityTaskFields] {
		return missingProposalCapability(BoardCapabilityTaskFields)
	}
	seenFields := map[string]bool{}
	for _, field := range p.Workflow.Fields {
		if !validProposalID(field.ID) || strings.TrimSpace(field.Name) == "" || !validProposalFieldType(field.Type) || seenFields[field.ID] {
			return fmt.Errorf("%w: invalid field", ErrInvalidBoardProposal)
		}
		seenFields[field.ID] = true
	}

	if len(p.Views) != 0 && (!capabilities[BoardCapabilityBoardViews] && !capabilities[BoardCapabilityListViews]) {
		return missingProposalCapability(BoardCapabilityBoardViews)
	}
	seenViews := map[string]bool{}
	for _, view := range p.Views {
		if !validProposalID(view.ID) || seenViews[view.ID] {
			return fmt.Errorf("%w: invalid view", ErrInvalidBoardProposal)
		}
		seenViews[view.ID] = true
		switch view.Kind {
		case "BOARD":
			if !capabilities[BoardCapabilityBoardViews] {
				return missingProposalCapability(BoardCapabilityBoardViews)
			}
		case "LIST":
			if !capabilities[BoardCapabilityListViews] {
				return missingProposalCapability(BoardCapabilityListViews)
			}
		default:
			return fmt.Errorf("%w: view kind %q", ErrInvalidBoardProposal, view.Kind)
		}
		for _, statusID := range view.StatusIDs {
			if !statuses[statusID] {
				return fmt.Errorf("%w: view references unknown status", ErrInvalidBoardProposal)
			}
		}
	}
	if len(p.SampleTasks) != 0 && !capabilities[BoardCapabilitySampleTasks] {
		return missingProposalCapability(BoardCapabilitySampleTasks)
	}
	seenSamples := map[string]bool{}
	for _, sample := range p.SampleTasks {
		if !validProposalID(sample.ID) || strings.TrimSpace(sample.Title) == "" || !statuses[sample.StatusID] || seenSamples[sample.ID] {
			return fmt.Errorf("%w: invalid sample task", ErrInvalidBoardProposal)
		}
		if !sample.Synthetic {
			return ErrLiveSampleTask
		}
		seenSamples[sample.ID] = true
	}
	return nil
}

func (p BoardProposal) CanonicalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

func validBoardProposalCapability(capability BoardProposalCapability) bool {
	switch capability {
	case BoardCapabilityStatuses, BoardCapabilityTransitions, BoardCapabilityTaskTypes, BoardCapabilityTaskFields, BoardCapabilityBoardViews, BoardCapabilityListViews, BoardCapabilitySampleTasks:
		return true
	default:
		return false
	}
}

func missingProposalCapability(capability BoardProposalCapability) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedProposal, capability)
}

func validProposalFieldType(value string) bool {
	switch value {
	case "TEXT", "NUMBER", "DATE", "ENUM", "PERSON", "LINK", "BOOLEAN":
		return true
	default:
		return false
	}
}

func validProposalID(value string) bool {
	return value == strings.TrimSpace(value) && value != "" && len(value) <= 128
}

func validateProposalStrings(values []string, label string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: empty %s", ErrInvalidBoardProposal, label)
		}
	}
	return nil
}

func hasProposalCycle(graph map[string][]string, nodes map[string]bool) bool {
	state := make(map[string]uint8, len(nodes))
	var visit func(string) bool
	visit = func(node string) bool {
		if state[node] == 1 {
			return true
		}
		if state[node] == 2 {
			return false
		}
		state[node] = 1
		for _, next := range graph[node] {
			if visit(next) {
				return true
			}
		}
		state[node] = 2
		return false
	}
	for node := range nodes {
		if visit(node) {
			return true
		}
	}
	return false
}
