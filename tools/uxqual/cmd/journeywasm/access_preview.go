package main

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

// accessPreviewScheduler is the part of the shared frontend task lane the
// role-access preview needs, narrowed so a test can drive it directly.
type accessPreviewScheduler interface {
	Submit(context.Context, taskmux.Spec, func(context.Context) error) (*taskmux.Handle, error)
}

// roleAccessPreviewRequest wires the visibility editor's preview button to
// JourneyService.PreviewRoleAccess (REV-093-01). The call runs on the shared
// bounded task lane, one in flight per role: a newer preview for the same
// role replaces the older one. It returns nil when the connected service
// has no preview call, which leaves the editor's unavailable notice in place.
func roleAccessPreviewRequest(ctx context.Context, tasks accessPreviewScheduler, service journeyclient.Service) productui.RoleAccessPreviewRequest {
	previews, ok := service.(journeyclient.AccessPreviewService)
	if !ok || previews == nil || tasks == nil {
		return nil
	}
	return func(policy productui.OrganizationVisibilityPolicy, done func(productui.RoleAccessPreview, error)) {
		if done == nil {
			return
		}
		_, err := tasks.Submit(ctx, taskmux.Spec{
			Key: "product:role-access-preview:" + policy.RoleID, Priority: taskmux.UserVisible, Duplicate: taskmux.ReplaceExisting,
		}, func(taskCtx context.Context) error {
			preview, previewErr := productclient.PreviewRoleAccess(taskCtx, previews, policy)
			done(preview, previewErr)
			return nil
		})
		if err != nil {
			done(productui.RoleAccessPreview{}, errors.Join(productclient.ErrAccessPreviewUnavailable, err))
		}
	}
}
