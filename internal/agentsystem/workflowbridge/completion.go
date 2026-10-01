// Package workflowbridge derives workflow results from durable agent evidence.
package workflowbridge

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

const (
	EventType      = "agents.run.completed"
	SchemaRef      = "agents.run.result/v1"
	Source         = "agents.runtime"
	CorrelationKey = "agent_run_id"
)

var (
	ErrEvidence = errors.New("agent workflow: invalid durable completion evidence")
	ErrPending  = errors.New("agent workflow: run is not terminal")
)

type Outcome string

const (
	Succeeded Outcome = "SUCCEEDED"
	Refused   Outcome = "REFUSED"
	Failed    Outcome = "FAILED"
	Cancelled Outcome = "CANCELLED"
	Expired   Outcome = "EXPIRED"
)

// Result contains only pinned identities and protected output references.
// A deterministic workflow may branch on Outcome; model text cannot select it.
type Result struct {
	RunID         string              `json:"run_id"`
	Agent         agentrun.VersionRef `json:"agent"`
	ContextDigest string              `json:"context_digest"`
	Outcome       Outcome             `json:"outcome"`
	Code          string              `json:"code,omitempty"`
	OutputRef     string              `json:"output_ref,omitempty"`
	OutputDigest  string              `json:"output_digest,omitempty"`
}

// Derive accepts no caller-provided result. The admission and execution must
// agree, and successful output must have matching validation/delivery evidence.
func Derive(record agentrun.Record, run runstate.Run) (Result, error) {
	if err := agentrun.ValidateAdmissionRecord(record); err != nil || record.Request.Source.Kind != agentrun.SourceWorkflow {
		return Result{}, ErrEvidence
	}
	result := Result{RunID: record.ID, Agent: record.Request.Agent, ContextDigest: record.Request.Context.Digest}
	if record.Decision == agentrun.DecisionRefused {
		result.Outcome, result.Code = Refused, record.RefusalCode
		return result, nil
	}
	if run.ID != record.ID || run.AdmissionID != record.ID || run.TenantID != record.Request.Source.TenantID ||
		run.RequestDigest != record.RequestDigest || run.AgentID != record.Authority.Agent.AgentID ||
		run.AgentVersion != record.Authority.Agent.Version || run.AgentDigest != record.Authority.Agent.Digest ||
		run.ContextDigest != record.Authority.Context.Digest || !run.Deadline.Equal(record.Request.Deadline) {
		return Result{}, ErrEvidence
	}
	result.Agent, result.ContextDigest = record.Authority.Agent, record.Authority.Context.Digest
	for _, effect := range run.Effects {
		if effect.Status != runstate.EffectApplied && effect.Status != runstate.EffectNotApplied {
			return Result{}, ErrEvidence
		}
	}
	switch run.State {
	case runstate.StateCompleted:
		var validated, delivered runstate.Checkpoint
		for _, checkpoint := range run.Checkpoints {
			if checkpoint.Phase == runstate.PhaseValidation {
				validated = checkpoint
			}
			if checkpoint.Phase == runstate.PhaseDelivery {
				delivered = checkpoint
			}
		}
		if validated.Sequence == 0 || delivered.Sequence <= validated.Sequence ||
			validated.Ref == "" || validated.Ref != delivered.Ref || validated.Digest != delivered.Digest ||
			!strings.HasPrefix(validated.Digest, "sha256:") || len(validated.Digest) != 71 {
			return Result{}, fmt.Errorf("%w: completed run lacks validated output", ErrEvidence)
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(validated.Digest, "sha256:")); err != nil {
			return Result{}, ErrEvidence
		}
		result.Outcome, result.OutputRef, result.OutputDigest = Succeeded, validated.Ref, validated.Digest
	case runstate.StateFailed, runstate.StateNeedsRepair:
		result.Outcome, result.Code = Failed, run.TerminalCode
	case runstate.StateCancelled:
		result.Outcome, result.Code = Cancelled, run.TerminalCode
	case runstate.StateExpired:
		result.Outcome, result.Code = Expired, run.TerminalCode
	default:
		return Result{}, ErrPending
	}
	if run.Lease != nil {
		return Result{}, ErrEvidence
	}
	return result, nil
}
