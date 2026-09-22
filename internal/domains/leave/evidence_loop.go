package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/workreview"
)

// Loop states: the closed review-loop vocabulary.
const (
	LoopAwaitingReview = "AWAITING_REVIEW"
	LoopMoreInfo       = "MORE_INFORMATION_REQUIRED"
	LoopResumed        = "RESUMED"
	LoopClosed         = "CLOSED"
)

// WorkerMessage is the one minimal-disclosure message: requirement and
// safe reason only, never evidence detail.
type WorkerMessage struct {
	MessageID     string
	RequirementID string
	Reason        string
}

// EvidenceRef is one governed evidence reference resuming the loop.
type EvidenceRef struct {
	Ref              string
	Quarantined      bool
	Classified       bool
	AuthorityCurrent bool
}

// ReviewLoop is the durable request-more-information loop state.
type ReviewLoop struct {
	LoopID       string
	TaskID       string
	ReviewerRole string
	State        string
	Message      WorkerMessage
	SignalID     string
	Evidence     []EvidenceRef
	ResumeCount  int
	Policy       string
	Digest       string
}

func loopDigest(loop ReviewLoop) string {
	refs := make([]string, 0, len(loop.Evidence))
	for _, ref := range loop.Evidence {
		refs = append(refs, ref.Ref)
	}
	parts := []string{"leave-review-loop", loop.LoopID, loop.TaskID, loop.ReviewerRole, loop.State, loop.Message.MessageID, loop.SignalID, strings.Join(refs, ","), fmt.Sprint(loop.ResumeCount), loop.Policy}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// OpenLoop binds one proposal-bound restricted task to its loop. Ordinary
// manager tasks never open evidence loops: the reviewer must be the
// LeaveAdministrator.
func OpenLoop(loopID, taskID, reviewerRole, policy string) (ReviewLoop, error) {
	if strings.TrimSpace(loopID) == "" || strings.TrimSpace(taskID) == "" {
		return ReviewLoop{}, fmt.Errorf("leave: loop and task identities are required")
	}
	if reviewerRole != "leave-administrator" {
		return ReviewLoop{}, fmt.Errorf("leave: evidence loops never assign ordinary manager tasks")
	}
	if strings.TrimSpace(policy) == "" {
		return ReviewLoop{}, fmt.Errorf("leave: evidence loops run under an explicit policy")
	}
	loop := ReviewLoop{LoopID: loopID, TaskID: taskID, ReviewerRole: reviewerRole, State: LoopAwaitingReview, Policy: policy}
	loop.Digest = loopDigest(loop)
	return loop, nil
}

// RequestMoreInfo handles one MORE_INFORMATION_REQUIRED finding: it
// creates exactly one minimal-disclosure worker message and one durable
// signal subscription. Repeated requests return the identical message,
// never duplicates.
func RequestMoreInfo(loop ReviewLoop, finding workreview.Finding) (ReviewLoop, error) {
	if finding.Verdict != workreview.ReviewMoreInfo {
		loop.State = LoopClosed
		loop.Digest = loopDigest(loop)
		return loop, nil
	}
	if loop.State != LoopAwaitingReview && loop.State != LoopMoreInfo {
		return ReviewLoop{}, fmt.Errorf("leave: information request expects an awaiting loop in %s", loop.State)
	}
	if loop.Message.MessageID != "" {
		loop.State = LoopMoreInfo
		loop.Digest = loopDigest(loop)
		return loop, nil
	}
	if strings.TrimSpace(finding.RequirementID) == "" || strings.TrimSpace(finding.Reason) == "" {
		return ReviewLoop{}, fmt.Errorf("leave: information request needs a requirement and a safe reason")
	}
	sum := sha256.Sum256([]byte("leave-worker-message\x00" + loop.LoopID + "\x00" + finding.RequirementID))
	loop.Message = WorkerMessage{
		MessageID:     "sha256:" + hex.EncodeToString(sum[:]),
		RequirementID: finding.RequirementID,
		Reason:        finding.Reason,
	}
	loop.SignalID = "signal:" + loop.LoopID
	loop.State = LoopMoreInfo
	loop.Digest = loopDigest(loop)
	return loop, nil
}

// ResumeEvidence resumes the loop once on a new governed evidence
// reference. Raw replies, unscanned uploads and stale authority refuse:
// only quarantined, classified, currently-authorized evidence resumes,
// and only once.
func ResumeEvidence(loop ReviewLoop, ref EvidenceRef) (ReviewLoop, error) {
	if loop.State != LoopMoreInfo {
		return ReviewLoop{}, fmt.Errorf("leave: resume expects an information-requested loop in %s", loop.State)
	}
	if loop.ResumeCount >= 1 {
		return ReviewLoop{}, fmt.Errorf("leave: evidence resumes the loop exactly once")
	}
	if strings.TrimSpace(ref.Ref) == "" {
		return ReviewLoop{}, fmt.Errorf("leave: raw replies never resume the workflow")
	}
	if !ref.Quarantined || !ref.Classified || !ref.AuthorityCurrent {
		return ReviewLoop{}, fmt.Errorf("leave: evidence bypassing scan, classification or current-authority checks never resumes")
	}
	loop.Evidence = append(loop.Evidence, ref)
	loop.ResumeCount++
	loop.State = LoopResumed
	loop.Digest = loopDigest(loop)
	return loop, nil
}

// Verify recomputes the loop seal.
func (loop ReviewLoop) Verify() error {
	if loop.Digest == "" || loopDigest(loop) != loop.Digest {
		return fmt.Errorf("leave: review loop seal is broken")
	}
	return nil
}
