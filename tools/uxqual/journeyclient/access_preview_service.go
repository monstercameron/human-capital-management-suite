package journeyclient

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func (s *grpcService) PreviewRoleAccess(ctx context.Context, in *journeyv1.PreviewRoleAccessRequest) (*journeyv1.PreviewRoleAccessResponse, error) {
	return s.client.PreviewRoleAccess(ctx, in)
}
