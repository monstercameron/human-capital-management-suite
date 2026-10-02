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
	reported, ok := status.FromError(err)
	if !ok || reported.Code() != codes.InvalidArgument {
		return false
	}
	for _, d := range reported.Details() {
		if detail, ok := d.(*commonv1.ErrorDetail); ok && detail.GetReasonRef() == chat.ReasonChannelName {
			return true
		}
	}
	return false
}
