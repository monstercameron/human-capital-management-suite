package main

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// membershipBarrierRefused reports whether err is the service refusing to add a
// person because an information barrier keeps their team out of the
// conversation (issue 3783). The add-people dialog says so in plain words
// instead of "you do not have access", which would send the person to their
// administrator for access they cannot be given.
func membershipBarrierRefused(err error) bool {
	reported, ok := status.FromError(err)
	if !ok || reported.Code() != codes.PermissionDenied {
		return false
	}
	for _, d := range reported.Details() {
		if detail, ok := d.(*commonv1.ErrorDetail); ok && detail.GetReasonRef() == chat.ReasonInformationBarrier {
			return true
		}
	}
	return false
}

// addMembersFailureMessage is the line the add-people dialog shows when adding
// failed for at least one person. A barrier refusal is explained; anything else
// keeps the generic failure line.
func addMembersFailureMessage(locale string, lastErr error, partial bool) string {
	if membershipBarrierRefused(lastErr) {
		line := chatui.BarrierRefusalNotice(locale)
		if partial {
			line += " " + chatui.ChatBarrierText(locale, "refused_partial")
		}
		return line
	}
	subject := "add everyone you picked"
	if partial {
		subject = "add everyone you picked (some were added)"
	}
	return chatLoadFailureMessage(subject, lastErr)
}
