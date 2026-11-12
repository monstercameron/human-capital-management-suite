package main

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// channelNameRefused reports whether err is the service refusing a new
// channel's name (CHATBUG-072). The create dialog answers it with the naming
// rule under the name box and stays open, rather than with a failure notice.
func channelNameRefused(err error) bool {
	return refusedWithReason(err, chat.ReasonChannelName)
}

// channelNameTakenRefused reports whether err is the service refusing a public
// channel name another live channel holds (ISSUE-3706).
func channelNameTakenRefused(err error) bool {
	return refusedWithReason(err, chat.ReasonChannelNameTaken)
}

func refusedWithReason(err error, reason string) bool {
	reported, ok := status.FromError(err)
	if !ok || reported.Code() != codes.InvalidArgument {
		return false
	}
	for _, d := range reported.Details() {
		if detail, ok := d.(*commonv1.ErrorDetail); ok && detail.GetReasonRef() == reason {
			return true
		}
	}
	return false
}
