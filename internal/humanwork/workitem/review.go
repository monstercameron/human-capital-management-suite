package workitem

import (
	"fmt"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/workreview"
)

// Finding is the sealed restricted-review result the workflow may see.
// The vocabulary lives in internal/engines/workreview (below the domain
// layer) so domains and workflow can consume findings without importing
// this store-owning package upward; this alias keeps every existing
// importer compiling unchanged.
type Finding = workreview.Finding

// Review verdicts: aliases for the closed typed-finding vocabulary owned
// by internal/engines/workreview.
const (
	ReviewSufficient   = workreview.ReviewSufficient
	ReviewInsufficient = workreview.ReviewInsufficient
	ReviewMoreInfo     = workreview.ReviewMoreInfo
	ReviewUnknown      = workreview.ReviewUnknown
)

// ReviewTask is one restricted evidence-review task.
type ReviewTask struct {
	TaskID              string
	PolicyID            string
	RequirementID       string
	RequirementVersion  string
	ArtifactID          string
	ArtifactVersion     string
	ArtifactCurrent     string
	ArtifactQuarantined bool
	Scope               []string
	ReviewerRole        string
	ExpiresTick         int64
}

// Reviewer completes one restricted review behind the compartment policy.
// The reviewer needs an explicit grant; the verdict stays inside the
// typed vocabulary with a safe reason; stale or quarantined artifacts
// refuse; duplicate completions return the identical finding instead of
// diverging.
type Reviewer struct {
	mu        sync.Mutex
	policies  map[string]AccessPolicy
	completed map[string]Finding
}

// NewReviewer starts an empty reviewer.
func NewReviewer() *Reviewer {
	return &Reviewer{policies: make(map[string]AccessPolicy), completed: make(map[string]Finding)}
}

// RegisterPolicy publishes one compartment policy.
func (reviewer *Reviewer) RegisterPolicy(policy AccessPolicy) error {
	if reviewer == nil {
		return fmt.Errorf("workitem: nil reviewer")
	}
	if strings.TrimSpace(policy.PolicyID) == "" {
		return fmt.Errorf("workitem: policy id is required")
	}
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.policies[policy.PolicyID] = policy
	return nil
}

// Complete finishes one review task. Sensitive notes never leave the
// compartment: only the typed finding returns.
func (reviewer *Reviewer) Complete(task ReviewTask, verdict, reason, evidenceReceipt string, nowTick int64) (Finding, error) {
	if reviewer == nil {
		return Finding{}, fmt.Errorf("workitem: nil reviewer")
	}
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	if prior, done := reviewer.completed[task.TaskID]; done {
		if prior.Verdict != verdict || prior.Reason != reason {
			return Finding{}, fmt.Errorf("workitem: duplicate completion of %s diverges", task.TaskID)
		}
		return prior, nil
	}
	policy, ok := reviewer.policies[task.PolicyID]
	if !ok {
		return Finding{}, fmt.Errorf("workitem: unknown policy %s", task.PolicyID)
	}
	decision, err := Authorize(policy, task.ReviewerRole, "view-artifact:medical-document", nowTick)
	if err != nil || !decision.Permitted {
		return Finding{}, fmt.Errorf("workitem: reviewer lacks an evidence grant")
	}
	if nowTick > task.ExpiresTick {
		return Finding{}, fmt.Errorf("workitem: review task %s expired", task.TaskID)
	}
	if !workreview.ValidVerdict(verdict) {
		return Finding{}, fmt.Errorf("workitem: verdict %q is not a typed finding", verdict)
	}
	if !workreview.ValidReason(reason) {
		return Finding{}, fmt.Errorf("workitem: reason %q exposes free-form detail", reason)
	}
	if task.ArtifactVersion != task.ArtifactCurrent || !task.ArtifactQuarantined {
		return Finding{}, fmt.Errorf("workitem: review relies on a stale or unquarantined artifact")
	}
	if strings.TrimSpace(task.RequirementID) == "" || strings.TrimSpace(task.RequirementVersion) == "" {
		return Finding{}, fmt.Errorf("workitem: review binds a requirement and version")
	}
	if strings.TrimSpace(evidenceReceipt) == "" {
		return Finding{}, fmt.Errorf("workitem: review needs an evidence receipt")
	}
	finding := Finding{
		TaskID: task.TaskID, Verdict: verdict,
		RequirementID: task.RequirementID, RequirementVersion: task.RequirementVersion,
		ArtifactID: task.ArtifactID, ArtifactVersion: task.ArtifactVersion,
		Scope: append([]string(nil), task.Scope...), Reason: reason,
		ExpiresTick: task.ExpiresTick, EvidenceReceipt: evidenceReceipt,
	}
	finding = workreview.Seal(finding)
	reviewer.completed[task.TaskID] = finding
	return finding, nil
}
