package journeyclient

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// AccessPreviewService is the read-only role-access preview the Roles &
// access visibility editor asks for before an administrator saves
// (REV-093-01). It is a separate seam from [PreferenceService] so existing
// preference fakes need not grow a method they never call; the production
// client implements both.
type AccessPreviewService interface {
	PreviewRoleAccess(context.Context, *journeyv1.PreviewRoleAccessRequest) (*journeyv1.PreviewRoleAccessResponse, error)
}

func (s *grpcService) PreviewRoleAccess(ctx context.Context, in *journeyv1.PreviewRoleAccessRequest) (*journeyv1.PreviewRoleAccessResponse, error) {
	return s.client.PreviewRoleAccess(ctx, in)
}
