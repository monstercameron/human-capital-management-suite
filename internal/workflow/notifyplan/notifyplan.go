// Package notifyplan declares which in-app notices a workflow step produces.
//
// It is the one statement of that fact. The execution composition publishes
// exactly these notices, and the workflow editor shows exactly these to an
// author, so what the editor promises and what a run does cannot drift. The
// package is pure and imports nothing from the repository so the browser
// client can carry it.
package notifyplan

import "strings"

// Audience names who receives a notice.
type Audience string

const (
	// AudienceAssignee is the person the step's work is routed to.
	AudienceAssignee Audience = "ASSIGNEE"
	// AudienceRequester is the person who started the request.
	AudienceRequester Audience = "REQUESTER"
)

// Moment names when a notice is published.
type Moment string

const (
	// MomentRouted is the commit that routes the step's work to its owner.
	MomentRouted Moment = "ROUTED"
	// MomentFinished is the commit of the run's terminal write.
	MomentFinished Moment = "FINISHED"
)

// Purposes are the message_intent purposes the notices are stored under.
const (
	PurposeApproval = "APPROVAL"
	PurposeTask     = "TASK"
	PurposeUpdate   = "WORKFLOW_UPDATE"
)

// Statuses a requester's update notice is served with. They are the wire
// values between the journey service and the product client, declared here so
// neither side keeps its own copy.
const (
	// StatusSentForReview reports a past event: the request was routed to
	// someone. It stays true after the request moves on.
	StatusSentForReview = "SENT_FOR_REVIEW"
	// The finished statuses say how the request ended.
	StatusFinishedApproved = "FINISHED_APPROVED"
	StatusFinishedDeclined = "FINISHED_DECLINED"
	StatusFinishedFailed   = "FINISHED_FAILED"
	StatusFinished         = "FINISHED"
)

// Notice is one notice a step produces.
type Notice struct {
	Audience Audience
	Moment   Moment
	Purpose  string
}

// ForStep returns the notices a step of the given kernel type produces, in the
// order they are published. Step types that route no human work and end no run
// produce none. The result is a fresh slice.
func ForStep(stepType string) []Notice {
	switch strings.ToUpper(strings.TrimSpace(stepType)) {
	case "APPROVAL":
		return []Notice{
			{Audience: AudienceAssignee, Moment: MomentRouted, Purpose: PurposeApproval},
			{Audience: AudienceRequester, Moment: MomentRouted, Purpose: PurposeUpdate},
		}
	case "TASK":
		return []Notice{
			{Audience: AudienceAssignee, Moment: MomentRouted, Purpose: PurposeTask},
			{Audience: AudienceRequester, Moment: MomentRouted, Purpose: PurposeUpdate},
		}
	case "END":
		return []Notice{{Audience: AudienceRequester, Moment: MomentFinished, Purpose: PurposeUpdate}}
	default:
		return nil
	}
}

// AssigneePurpose is the purpose of the notice sent to the owner of routed
// work: APPROVAL for an approval step and TASK for every other kind.
func AssigneePurpose(approval bool) string {
	if approval {
		return PurposeApproval
	}
	return PurposeTask
}
