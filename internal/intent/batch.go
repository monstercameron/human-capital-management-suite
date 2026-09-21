package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
)

// Batch subject outcomes: every frozen subject lands in exactly one bucket.
const (
	BatchEligible   = "eligible"
	BatchIneligible = "ineligible"
	BatchDenied     = "denied"
	BatchUnknown    = "unknown"
	BatchSucceeded  = "succeeded"
	BatchFailed     = "failed"
	BatchRepaired   = "repaired"
)

// BatchLimits bounds the blast radius, cost and rate of one batch. Zero
// values refuse: unbounded batches never compile.
type BatchLimits struct {
	MaxSubjects  int
	MaxCost      int
	MaxRate      int
	CostPerChild int
}

// BatchSpec compiles bulk features into one population-scoped change
// request. The population arrives only as a frozen snapshot: the compiler
// never runs a live query, so membership cannot silently change.
type BatchSpec struct {
	DefinitionID      string
	PopulationScope   string
	Snapshot          population.Snapshot
	OperationTemplate string
	OperationVersion  string
	ChildFamily       string
	Exclusions        []string
	PartitionSize     int
	Limits            BatchLimits
	CompletionPolicy  string
}

// ChildIntent is one bounded deterministic child: ordinary governance,
// idempotency, transaction and repair semantics apply per child, and no
// batch kernel family is ever introduced.
type ChildIntent struct {
	ChildID   string
	SubjectID string
	Partition int
	Family    string
	Operation string
	Version   string
}

// ChildResult is the deterministic outcome of one child.
type ChildResult struct {
	ChildID   string
	SubjectID string
	Outcome   string
	Detail    string
}

// BatchCounts exactly partitions the frozen population.
type BatchCounts struct {
	Eligible   int
	Ineligible int
	Denied     int
	Unknown    int
	Succeeded  int
	Failed     int
	Repaired   int
}

// Batch is one compiled bulk change request with bounded children.
type Batch struct {
	BatchID    string
	Definition string
	Scope      string
	Snapshot   string
	Children   []ChildIntent
	Counts     BatchCounts
	Digest     string
}

func batchDigest(spec BatchSpec, children []ChildIntent) string {
	ids := make([]string, 0, len(children))
	for _, child := range children {
		ids = append(ids, child.ChildID+"="+child.SubjectID)
	}
	sort.Strings(ids)
	parts := []string{"intent-batch", spec.DefinitionID, spec.PopulationScope, spec.Snapshot.Digest, spec.OperationTemplate, spec.OperationVersion, spec.ChildFamily, spec.CompletionPolicy}
	parts = append(parts, ids...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CompileBatch binds the frozen snapshot into bounded children. Denied
// subjects are counted but never named: no child, no identity and no
// evidence line carries them.
func CompileBatch(spec BatchSpec) (Batch, error) {
	if strings.TrimSpace(spec.DefinitionID) == "" || strings.TrimSpace(spec.PopulationScope) == "" {
		return Batch{}, fmt.Errorf("intent: batch definition and population scope are required")
	}
	if strings.TrimSpace(spec.OperationTemplate) == "" || strings.TrimSpace(spec.OperationVersion) == "" {
		return Batch{}, fmt.Errorf("intent: batch operation template and version are required")
	}
	if strings.TrimSpace(spec.ChildFamily) == "" || spec.ChildFamily == "batch" || spec.ChildFamily == "bulk" {
		return Batch{}, fmt.Errorf("intent: batch children keep an ordinary family, never a batch kernel family")
	}
	if spec.Snapshot.Digest == "" {
		return Batch{}, fmt.Errorf("intent: batch compiles only from a frozen population snapshot, never a live query")
	}
	if spec.Snapshot.MembershipProtected {
		return Batch{}, fmt.Errorf("intent: batch cannot enumerate a membership-protected snapshot")
	}
	if spec.Limits.MaxSubjects <= 0 || spec.Limits.MaxCost <= 0 || spec.Limits.MaxRate <= 0 || spec.Limits.CostPerChild <= 0 {
		return Batch{}, fmt.Errorf("intent: batch blast-radius, cost and rate limits must be positive")
	}
	if spec.PartitionSize <= 0 {
		return Batch{}, fmt.Errorf("intent: batch partition size must be positive")
	}
	if strings.TrimSpace(spec.CompletionPolicy) == "" {
		return Batch{}, fmt.Errorf("intent: batch completion policy is required")
	}
	excluded := make(map[string]bool, len(spec.Exclusions))
	for _, id := range spec.Exclusions {
		if strings.TrimSpace(id) == "" {
			return Batch{}, fmt.Errorf("intent: batch exclusions must be non-empty subject ids")
		}
		excluded[id] = true
	}
	// REV-038-02: subjects arrive through a popscale session, not a direct
	// membership read, so the batch compiler inherits the scale-safe
	// engine's page bound, count-consistency validation and
	// non-distinguishing protected/empty responses. The session serves the
	// snapshot's own order once; the compiler still sorts its working copy,
	// so child identities and digests are unchanged for consistent
	// snapshots.
	subjects, err := popscaleSubjects(spec.Snapshot, spec.PartitionSize)
	if err != nil {
		return Batch{}, err
	}
	sort.Strings(subjects)
	batch := Batch{Definition: spec.DefinitionID, Scope: spec.PopulationScope, Snapshot: spec.Snapshot.Digest}
	sum := sha256.Sum256([]byte("intent-batch-id\x00" + batchDigest(spec, nil)))
	batch.BatchID = "sha256:" + hex.EncodeToString(sum[:])
	eligible := 0
	for index, subject := range subjects {
		switch {
		case excluded[subject]:
			batch.Counts.Ineligible++
		case strings.HasPrefix(subject, "denied:"):
			batch.Counts.Denied++
		case strings.HasPrefix(subject, "unknown:"):
			batch.Counts.Unknown++
		default:
			eligible++
			childSum := sha256.Sum256([]byte(strings.Join([]string{"intent-batch-child", batch.BatchID, subject, fmt.Sprint(index)}, "\x00")))
			batch.Children = append(batch.Children, ChildIntent{
				ChildID:   "sha256:" + hex.EncodeToString(childSum[:]),
				SubjectID: subject,
				Partition: eligible / spec.PartitionSize,
				Family:    spec.ChildFamily,
				Operation: spec.OperationTemplate,
				Version:   spec.OperationVersion,
			})
		}
	}
	batch.Counts.Eligible = eligible
	if eligible > spec.Limits.MaxSubjects {
		return Batch{}, fmt.Errorf("intent: batch of %d exceeds the declared blast radius of %d", eligible, spec.Limits.MaxSubjects)
	}
	if total := eligible * spec.Limits.CostPerChild; total > spec.Limits.MaxCost {
		return Batch{}, fmt.Errorf("intent: batch cost %d exceeds the declared limit of %d", total, spec.Limits.MaxCost)
	}
	batch.Digest = batchDigest(spec, batch.Children)
	return batch, nil
}

// ChildExecutor performs one child effect outside the compiler.
type ChildExecutor interface {
	Execute(child ChildIntent) ChildResult
}

// ApplyBatch runs every child exactly once through the executor. Prior
// results resume the batch: recorded subjects are skipped, never
// duplicated, and counts partition the whole frozen population.
func ApplyBatch(batch Batch, execute ChildExecutor, prior []ChildResult) (results []ChildResult, counts BatchCounts, err error) {
	if execute == nil {
		return nil, BatchCounts{}, fmt.Errorf("intent: batch execution requires a child executor")
	}
	if batch.Digest == "" || len(batch.Children) == 0 && batch.Counts.Eligible != 0 {
		return nil, BatchCounts{}, fmt.Errorf("intent: uncompiled batch cannot execute")
	}
	recorded := make(map[string]ChildResult, len(prior))
	for _, result := range prior {
		if _, dup := recorded[result.ChildID]; dup {
			return nil, BatchCounts{}, fmt.Errorf("intent: duplicate prior result for child %s", result.ChildID)
		}
		recorded[result.ChildID] = result
	}
	counts = BatchCounts{Eligible: batch.Counts.Eligible, Ineligible: batch.Counts.Ineligible, Denied: batch.Counts.Denied, Unknown: batch.Counts.Unknown}
	for _, child := range batch.Children {
		if previous, ok := recorded[child.ChildID]; ok {
			if previous.SubjectID != child.SubjectID {
				return nil, BatchCounts{}, fmt.Errorf("intent: prior result rebinds child %s to another subject", child.ChildID)
			}
			results = append(results, previous)
		} else {
			result := execute.Execute(child)
			result.ChildID = child.ChildID
			result.SubjectID = child.SubjectID
			results = append(results, result)
		}
	}
	for _, result := range results {
		switch result.Outcome {
		case BatchSucceeded:
			counts.Succeeded++
		case BatchFailed:
			counts.Failed++
		case BatchRepaired:
			counts.Repaired++
		default:
			return nil, BatchCounts{}, fmt.Errorf("intent: child %s reports outcome %q outside succeeded/failed/repaired", result.ChildID, result.Outcome)
		}
	}
	if len(results) != batch.Counts.Eligible || counts.Succeeded+counts.Failed+counts.Repaired != batch.Counts.Eligible {
		return nil, BatchCounts{}, fmt.Errorf("intent: batch results do not exactly partition the eligible population")
	}
	return results, counts, nil
}
