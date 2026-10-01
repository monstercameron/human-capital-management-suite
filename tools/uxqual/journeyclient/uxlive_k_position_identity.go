package journeyclient

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// targetPositionIdentity is the client edge of the authorized position
// projection. The selected revision reference is retained for submission by
// the server, but only the human title and stable role code cross into the
// default card scan.
func targetPositionIdentity(j *journeyv1.Journey) journey.PositionIdentity {
	if j == nil || strings.TrimSpace(j.GetTarget().GetPositionId()) == "" {
		return journey.PositionIdentity{}
	}
	code := strings.TrimSpace(j.GetTarget().GetJobCode())
	title := strings.TrimSpace(JobTitle(code))
	if title == "" || strings.EqualFold(title, code) {
		return journey.PositionIdentity{Code: code, State: journey.PositionIdentityUnresolved}
	}
	return journey.PositionIdentity{Title: title, Code: code, State: journey.PositionIdentityResolved}
}

func targetPositionIdentityFromReview(j *journeyv1.Journey, title string) journey.PositionIdentity {
	if j == nil || strings.TrimSpace(j.GetTarget().GetPositionId()) == "" {
		return journey.PositionIdentity{}
	}
	code := strings.TrimSpace(j.GetTarget().GetJobCode())
	if title = strings.TrimSpace(title); title != "" {
		return journey.PositionIdentity{Title: title, Code: code, State: journey.PositionIdentityResolved}
	}
	return journey.PositionIdentity{Code: code, State: journey.PositionIdentityUnresolved}
}
