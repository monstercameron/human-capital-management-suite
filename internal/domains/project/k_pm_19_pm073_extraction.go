package project

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrInvalidExtractionPlan = errors.New("project: invalid extraction plan")
	ErrPilotGateFailed       = errors.New("project: pilot gate failed")
	ErrExtractionManifest    = errors.New("project: extraction manifest does not match source snapshot")
)

type ExtractionPhase string

const (
	ExtractionCopy     ExtractionPhase = "COPY"
	ExtractionVerify   ExtractionPhase = "VERIFY"
	ExtractionFence    ExtractionPhase = "FENCE"
	ExtractionCutover  ExtractionPhase = "CUTOVER"
	ExtractionDrain    ExtractionPhase = "DRAIN"
	ExtractionRestored ExtractionPhase = "RESTORED"
)

// ExtractionSnapshot is the placement-independent state required to preserve
// project API behavior during a storage move. IDs, revisions, memberships and
// the event cursor are explicit so a copy cannot silently omit one authority.
type ExtractionSnapshot struct {
	TenantID        string
	ProjectID       string
	ProjectRevision uint64
	Tasks           []TaskCheckpoint
	Memberships     []MembershipCheckpoint
	EventCursor     uint64
}

type TaskCheckpoint struct {
	ID       string
	Revision uint64
}

type MembershipCheckpoint struct {
	UserID   string
	Revision uint64
}

type PilotGate struct {
	MixedLoadWithinSLO    bool
	SeparateCapacityReady bool
	RehearsalApproved     bool
}

type ExtractionPlan struct {
	Snapshot          ExtractionSnapshot
	SourcePlacement   string
	TargetPlacement   string
	APIContractDigest string
	Pilot             PilotGate
}

type ExtractionManifest struct {
	Snapshot          ExtractionSnapshot
	APIContractDigest string
}

type RestoreEvidence struct {
	OperationID string
	Source      string
	Target      string
	Reason      string
}

type ExtractionResult struct {
	Phases               []ExtractionPhase
	Manifest             ExtractionManifest
	Cutover              bool
	Drained              bool
	APIContractPreserved bool
	Restore              RestoreEvidence
	RequiredAction       string
}

// ProjectExtractor is the storage-placement port. Implementations keep their
// own database credentials and transactions; no cross-database join is
// implied by this contract.
type ProjectExtractor interface {
	Copy(context.Context, ExtractionPlan) (ExtractionManifest, error)
	Verify(context.Context, ExtractionPlan, ExtractionManifest) error
	Fence(context.Context, ExtractionPlan) error
	Cutover(context.Context, ExtractionPlan, ExtractionManifest) error
	Drain(context.Context, ExtractionPlan) error
	Restore(context.Context, ExtractionPlan, ExtractionManifest, string) (RestoreEvidence, error)
}

// RehearseExtraction performs the conditional copy/verify/fence/cutover/drain
// sequence. Once a target copy exists, every later failure requests cleanup or
// restore evidence, leaving the source usable for a safe retry instead of
// reporting a partially moved project as complete.
func RehearseExtraction(ctx context.Context, extractor ProjectExtractor, plan ExtractionPlan) (ExtractionResult, error) {
	if extractor == nil {
		return ExtractionResult{}, ErrInvalidExtractionPlan
	}
	if err := plan.Validate(); err != nil {
		return ExtractionResult{}, err
	}
	result := ExtractionResult{Phases: make([]ExtractionPhase, 0, 6)}
	if !plan.Pilot.MixedLoadWithinSLO || !plan.Pilot.SeparateCapacityReady || !plan.Pilot.RehearsalApproved {
		result.RequiredAction = "SEPARATE_PROJECT_DATABASE_CAPACITY_AND_RERUN"
		return result, ErrPilotGateFailed
	}

	manifest, err := extractor.Copy(ctx, plan)
	result.Phases = append(result.Phases, ExtractionCopy)
	if err != nil {
		return result, err
	}
	result.Manifest = manifest
	if err = validateManifest(plan, manifest); err != nil {
		return restoreAfterFailure(ctx, extractor, plan, manifest, result, true, err)
	}
	if err = extractor.Verify(ctx, plan, manifest); err != nil {
		return restoreAfterFailure(ctx, extractor, plan, manifest, result, true, err)
	}
	result.Phases = append(result.Phases, ExtractionVerify)

	cleanupNeeded := true
	if err = extractor.Fence(ctx, plan); err != nil {
		return restoreAfterFailure(ctx, extractor, plan, manifest, result, cleanupNeeded, err)
	}
	result.Phases = append(result.Phases, ExtractionFence)
	if err = extractor.Cutover(ctx, plan, manifest); err != nil {
		return restoreAfterFailure(ctx, extractor, plan, manifest, result, cleanupNeeded, err)
	}
	result.Phases = append(result.Phases, ExtractionCutover)
	result.Cutover = true
	if err = extractor.Drain(ctx, plan); err != nil {
		return restoreAfterFailure(ctx, extractor, plan, manifest, result, cleanupNeeded, err)
	}
	result.Phases = append(result.Phases, ExtractionDrain)
	result.Drained = true
	result.APIContractPreserved = true
	return result, nil
}

func restoreAfterFailure(ctx context.Context, extractor ProjectExtractor, plan ExtractionPlan, manifest ExtractionManifest, result ExtractionResult, cleanupNeeded bool, cause error) (ExtractionResult, error) {
	if !cleanupNeeded {
		return result, cause
	}
	evidence, restoreErr := extractor.Restore(context.WithoutCancel(ctx), plan, manifest, cause.Error())
	if restoreErr == nil && validRestoreEvidence(evidence, plan) {
		result.Restore = evidence
		result.Phases = append(result.Phases, ExtractionRestored)
		return result, cause
	}
	if restoreErr == nil {
		restoreErr = ErrInvalidExtractionPlan
	}
	return result, errors.Join(cause, restoreErr)
}

func (p ExtractionPlan) Validate() error {
	if err := p.Snapshot.Validate(); err != nil || strings.TrimSpace(p.SourcePlacement) == "" || strings.TrimSpace(p.TargetPlacement) == "" || p.SourcePlacement == p.TargetPlacement || strings.TrimSpace(p.APIContractDigest) == "" {
		return ErrInvalidExtractionPlan
	}
	return nil
}

func (s ExtractionSnapshot) Validate() error {
	if strings.TrimSpace(s.TenantID) == "" || strings.TrimSpace(s.ProjectID) == "" || s.ProjectRevision == 0 || s.EventCursor == 0 {
		return ErrInvalidExtractionPlan
	}
	seenTasks := make(map[string]struct{}, len(s.Tasks))
	for _, task := range s.Tasks {
		if strings.TrimSpace(task.ID) == "" || task.Revision == 0 {
			return ErrInvalidExtractionPlan
		}
		if _, exists := seenTasks[task.ID]; exists {
			return ErrInvalidExtractionPlan
		}
		seenTasks[task.ID] = struct{}{}
	}
	seenMembers := make(map[string]struct{}, len(s.Memberships))
	for _, member := range s.Memberships {
		if strings.TrimSpace(member.UserID) == "" || member.Revision == 0 {
			return ErrInvalidExtractionPlan
		}
		if _, exists := seenMembers[member.UserID]; exists {
			return ErrInvalidExtractionPlan
		}
		seenMembers[member.UserID] = struct{}{}
	}
	return nil
}

func validateManifest(plan ExtractionPlan, manifest ExtractionManifest) error {
	if manifest.APIContractDigest != plan.APIContractDigest || manifest.Snapshot.TenantID != plan.Snapshot.TenantID || manifest.Snapshot.ProjectID != plan.Snapshot.ProjectID || manifest.Snapshot.ProjectRevision != plan.Snapshot.ProjectRevision || manifest.Snapshot.EventCursor != plan.Snapshot.EventCursor {
		return ErrExtractionManifest
	}
	if err := manifest.Snapshot.Validate(); err != nil {
		return ErrExtractionManifest
	}
	if !sameTaskCheckpoints(plan.Snapshot.Tasks, manifest.Snapshot.Tasks) || !sameMembershipCheckpoints(plan.Snapshot.Memberships, manifest.Snapshot.Memberships) {
		return ErrExtractionManifest
	}
	return nil
}

func sameTaskCheckpoints(a, b []TaskCheckpoint) bool {
	left, right := append([]TaskCheckpoint(nil), a...), append([]TaskCheckpoint(nil), b...)
	sort.Slice(left, func(i, j int) bool { return left[i].ID < left[j].ID })
	sort.Slice(right, func(i, j int) bool { return right[i].ID < right[j].ID })
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameMembershipCheckpoints(a, b []MembershipCheckpoint) bool {
	left, right := append([]MembershipCheckpoint(nil), a...), append([]MembershipCheckpoint(nil), b...)
	sort.Slice(left, func(i, j int) bool { return left[i].UserID < left[j].UserID })
	sort.Slice(right, func(i, j int) bool { return right[i].UserID < right[j].UserID })
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func validRestoreEvidence(evidence RestoreEvidence, plan ExtractionPlan) bool {
	return strings.TrimSpace(evidence.OperationID) != "" && strings.TrimSpace(evidence.Source) == plan.SourcePlacement && strings.TrimSpace(evidence.Target) == plan.TargetPlacement && strings.TrimSpace(evidence.Reason) != ""
}

func (r ExtractionResult) String() string {
	return fmt.Sprintf("phases=%v cutover=%t drained=%t restored=%t", r.Phases, r.Cutover, r.Drained, r.Restore.OperationID != "")
}
