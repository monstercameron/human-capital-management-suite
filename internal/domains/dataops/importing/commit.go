package importing

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

// DATAOPS-006: an approved import commits resumably and reconciles every item.
//
// Only a signed simulation whose approval binds its own digest, batch digest
// and mapping digest may commit, and only against the exact batch and mapping
// it simulated. Each CREATE or CHANGE draft commits through the governed item
// committer under an idempotency key derived from the simulation digest and
// row id, and is counted COMMITTED only when it returns transaction,
// observation and matched reconciliation lineage. A checkpoint is saved after
// every item under compare-and-set, so an interrupted commit resumes where it
// stopped, a committed item is never committed twice, a concurrent resume
// loses the compare-and-set instead of duplicating writes, and a checkpoint
// bound to a different simulation is refused. NO_OP, ERROR and CONFLICT drafts
// are classified, never written. The result counts partition the input
// exactly. Undoing a committed import is a set of governed corrective intents
// ([CorrectiveIntents]); nothing is ever deleted from the ledger.

// Commit errors.
var (
	ErrCommitBinding = errors.New("importing: commit is not bound to the approved simulation")
	ErrCommitInput   = errors.New("importing: commit input is invalid")
)

// CommitApproval is the recorded approval of one simulation.
type CommitApproval struct {
	SimulationDigest string
	BatchDigest      string
	MappingDigest    string
	ApprovalRef      string
	ApprovedBy       string
}

// CommitInput is one commit or resume.
type CommitInput struct {
	ImportID    string
	Simulation  ImportSimulation
	Verifier    Verifier
	Approval    CommitApproval
	Batch       Batch
	Mapping     MappingProfile
	Committer   ItemCommitter
	Checkpoints CheckpointStore
	// Limit bounds how many items this call attempts; zero means all. A
	// bounded call is how a long import commits in resumable slices.
	Limit int
}

// ItemOutcome is one draft's result.
type ItemOutcome struct {
	RowID       string
	Ordinal     int
	Status      string
	Lineage     ItemLineage
	FailureCode string
	Resumed     bool
}

// CommitResult is the partitioned result of a commit call.
type CommitResult struct {
	Committed, NoOps, Errors, Conflicts, Failed, Pending int
	Items                                                []ItemOutcome
	Complete                                             bool
}

// CommitImport commits (or resumes committing) an approved import.
func CommitImport(ctx context.Context, in CommitInput) (CommitResult, error) {
	if in.ImportID == "" || in.Committer == nil || in.Checkpoints == nil || in.Verifier == nil || in.Limit < 0 {
		return CommitResult{}, fmt.Errorf("%w: import id, committer, checkpoint store and verifier are required", ErrCommitInput)
	}
	sim := in.Simulation
	if err := VerifyImportSimulation(sim, in.Verifier); err != nil {
		return CommitResult{}, fmt.Errorf("%w: %w", ErrCommitBinding, err)
	}
	switch {
	case in.Approval.ApprovalRef == "" || in.Approval.ApprovedBy == "":
		return CommitResult{}, fmt.Errorf("%w: no recorded approval", ErrCommitBinding)
	case in.Approval.SimulationDigest != sim.Digest || in.Approval.BatchDigest != sim.BatchDigest || in.Approval.MappingDigest != sim.MappingDigest:
		return CommitResult{}, fmt.Errorf("%w: approval names a different proposal", ErrCommitBinding)
	case in.Batch.Digest() != sim.BatchDigest:
		return CommitResult{}, fmt.Errorf("%w: batch changed since simulation", ErrCommitBinding)
	case in.Mapping.Digest != sim.MappingDigest:
		return CommitResult{}, fmt.Errorf("%w: mapping changed since simulation", ErrCommitBinding)
	}

	cp, found, err := in.Checkpoints.Load(ctx, in.ImportID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("importing: load checkpoint: %w", err)
	}
	if found {
		if cp.SimulationDigest != sim.Digest || cp.BatchDigest != sim.BatchDigest || cp.MappingDigest != sim.MappingDigest {
			return CommitResult{}, fmt.Errorf("%w: checkpoint belongs to a different simulation", ErrCommitBinding)
		}
	} else {
		cp = Checkpoint{ImportID: in.ImportID, SimulationDigest: sim.Digest, BatchDigest: sim.BatchDigest, MappingDigest: sim.MappingDigest}
	}
	items := maps.Clone(cp.Items)
	if items == nil {
		items = map[string]ItemState{}
	}

	res := CommitResult{}
	attempted := 0
	for _, d := range sim.Drafts {
		out := ItemOutcome{RowID: d.RowID, Ordinal: d.Ordinal}
		switch d.Status {
		case "NO_OP":
			out.Status = ItemNoOp
			res.NoOps++
		case "ERROR":
			out.Status = ItemError
			res.Errors++
		case "CONFLICT":
			out.Status = ItemConflict
			res.Conflicts++
		case "CREATE", "CHANGE":
			if prior, ok := items[d.RowID]; ok && prior.Status == ItemCommitted {
				out.Status, out.Lineage, out.Resumed = ItemCommitted, prior.Lineage, true
				res.Committed++
				break
			}
			if in.Limit > 0 && attempted >= in.Limit {
				out.Status = "PENDING"
				res.Pending++
				break
			}
			attempted++
			state := commitOne(ctx, in, d)
			next := maps.Clone(items)
			next[d.RowID] = state
			saved := cp
			saved.Items, saved.Version = next, cp.Version+1
			if err := in.Checkpoints.Save(ctx, saved, cp.Version); err != nil {
				return CommitResult{}, fmt.Errorf("importing: save checkpoint after %s: %w", d.RowID, err)
			}
			cp, items = saved, next
			out.Status, out.Lineage, out.FailureCode = state.Status, state.Lineage, state.FailureCode
			if state.Status == ItemCommitted {
				res.Committed++
			} else {
				res.Failed++
			}
		default:
			return CommitResult{}, fmt.Errorf("%w: draft %s has unknown status %q", ErrCommitInput, d.RowID, d.Status)
		}
		res.Items = append(res.Items, out)
	}
	res.Complete = res.Pending == 0 && res.Failed == 0
	return res, nil
}

func commitOne(ctx context.Context, in CommitInput, d BusinessIntentDraft) ItemState {
	lineage, err := in.Committer.CommitItem(ctx, ItemRequest{
		Tenant: in.Simulation.Tenant, ImportID: in.ImportID, SimulationDigest: in.Simulation.Digest,
		IdempotencyKey: IdempotencyKey(in.Simulation.Digest, d.RowID), Draft: d,
	})
	switch {
	case err != nil:
		return ItemState{Status: ItemFailed, FailureCode: "COMMIT_FAILED"}
	case lineage.TransactionRef == "" || lineage.ObservationRef == "":
		return ItemState{Status: ItemFailed, Lineage: lineage, FailureCode: "LINEAGE_INCOMPLETE"}
	case lineage.ReconciliationStatus != ReconciliationMatched:
		return ItemState{Status: ItemFailed, Lineage: lineage, FailureCode: "RECONCILIATION_MISMATCH"}
	}
	return ItemState{Status: ItemCommitted, Lineage: lineage}
}

// CorrectiveIntent reverses one committed item as a new governed intent.
type CorrectiveIntent struct {
	RowID          string
	IntentType     string
	SubjectID      string
	CorrectsTxRef  string
	Writes         []DraftWrite
	IdempotencyKey string
}

// CorrectiveIntents plans the governed reversal of every committed item: each
// write proposes its baseline value back (or its absence), bound to the
// transaction it corrects. It never deletes history.
func CorrectiveIntents(sim ImportSimulation, res CommitResult) []CorrectiveIntent {
	drafts := map[string]BusinessIntentDraft{}
	for _, d := range sim.Drafts {
		drafts[d.RowID] = d
	}
	var out []CorrectiveIntent
	for _, item := range res.Items {
		if item.Status != ItemCommitted {
			continue
		}
		d := drafts[item.RowID]
		ci := CorrectiveIntent{RowID: d.RowID, IntentType: d.IntentType, SubjectID: d.SubjectID, CorrectsTxRef: item.Lineage.TransactionRef,
			IdempotencyKey: "correct:" + IdempotencyKey(sim.Digest, d.RowID)}
		for _, w := range d.Writes {
			ci.Writes = append(ci.Writes, DraftWrite{Property: w.Property, Current: w.Proposed, CurrentPresent: true,
				Proposed: w.Current, AuthorityRef: w.AuthorityRef, ResourceKey: w.ResourceKey})
		}
		out = append(out, ci)
	}
	return out
}
