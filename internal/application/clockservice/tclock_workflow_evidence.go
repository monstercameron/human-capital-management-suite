package clockservice

import (
	"context"

	"github.com/google/uuid"
)

// PunchNodeEvidence is the durable engine attempt linked to one time effect.
// The reader obtains these facts from the engine's tenant-scoped records.
type PunchNodeEvidence struct {
	TenantID, InstanceID                         uuid.UUID
	NodeID, ObservationID, SessionID, PlanDigest string
	TraceID, OutputDigest, CompletedState        string
	Attempt                                      int
	InstanceVersion                              int64
}

// PunchNodeEvidenceReader proves the completed engine attempt for an observation.
type PunchNodeEvidenceReader interface {
	LoadPunchNodeEvidence(context.Context, uuid.UUID, uuid.UUID, string) (PunchNodeEvidence, bool, error)
}

// PunchCommitEvidenceDigest addresses the exact observation and session effect.
func PunchCommitEvidenceDigest(observation, session string) string {
	return punchDigest("hcmnext.time.workflow.commit/v1", observation, session)
}

func validPunchNodeEvidence(e PunchNodeEvidence, b TimeClockRunBinding, work PunchWork) bool {
	wantNode := "commit_punch"
	if work.Observation.EventType == "OUT" && b.WorkflowID == "hcmnext.workflows.time.clock_in_out" {
		wantNode = "commit_clock_out"
	}
	return e.TenantID == b.TenantID && e.InstanceID == b.InstanceID &&
		e.NodeID == wantNode && e.CompletedState == "SUCCEEDED" &&
		e.ObservationID == work.Observation.ID && e.SessionID == work.Session.ID &&
		e.PlanDigest == b.PlanDigest && e.TraceID != "" && e.Attempt > 0 &&
		e.InstanceVersion > 0 && e.OutputDigest == PunchCommitEvidenceDigest(e.ObservationID, e.SessionID)
}
