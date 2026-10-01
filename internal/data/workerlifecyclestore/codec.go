package workerlifecyclestore

import (
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Optional domain references use null on disk. values.EntityRef's strict text
// encoding intentionally rejects the zero value; zero is not an entity.
type planPayload struct {
	workerlifecycle.WorkerLifecyclePlan
	ApprovalDecision *values.EntityRef
}
type requestPayload struct {
	workerlifecycle.ResolutionRequest
	Plan planPayload
}
type childPayload struct {
	workerlifecycle.TrackedChild
	ObservationRef *values.EntityRef
}
type trackerPayload struct {
	workerlifecycle.OnboardingTracker
	Children []childPayload
}
type snapshotPayload struct {
	Snapshot
	Request requestPayload
	Tracker trackerPayload
}

func encode(snapshot Snapshot) ([]byte, error) {
	out := snapshotPayload{Snapshot: snapshot, Request: requestPayload{ResolutionRequest: snapshot.Request, Plan: planPayload{WorkerLifecyclePlan: snapshot.Request.Plan}}, Tracker: trackerPayload{OnboardingTracker: snapshot.Tracker}}
	if snapshot.Request.Plan.ApprovalDecision != (values.EntityRef{}) {
		ref := snapshot.Request.Plan.ApprovalDecision
		out.Request.Plan.ApprovalDecision = &ref
	}
	if snapshot.Tracker.Children != nil {
		out.Tracker.Children = make([]childPayload, 0, len(snapshot.Tracker.Children))
	}
	for _, child := range snapshot.Tracker.Children {
		c := childPayload{TrackedChild: child}
		if child.ObservationRef != (values.EntityRef{}) {
			ref := child.ObservationRef
			c.ObservationRef = &ref
		}
		out.Tracker.Children = append(out.Tracker.Children, c)
	}
	return json.Marshal(out)
}

func decode(raw []byte, snapshot *Snapshot) error {
	var in snapshotPayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	*snapshot = in.Snapshot
	snapshot.Request = in.Request.ResolutionRequest
	snapshot.Request.Plan = in.Request.Plan.WorkerLifecyclePlan
	if in.Request.Plan.ApprovalDecision != nil {
		snapshot.Request.Plan.ApprovalDecision = *in.Request.Plan.ApprovalDecision
	}
	snapshot.Tracker = in.Tracker.OnboardingTracker
	if in.Tracker.Children != nil {
		snapshot.Tracker.Children = make([]workerlifecycle.TrackedChild, 0, len(in.Tracker.Children))
	}
	for _, child := range in.Tracker.Children {
		c := child.TrackedChild
		if child.ObservationRef != nil {
			c.ObservationRef = *child.ObservationRef
		}
		snapshot.Tracker.Children = append(snapshot.Tracker.Children, c)
	}
	return nil
}
