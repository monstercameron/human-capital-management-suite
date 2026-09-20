package productclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// ErrAccessPreviewUnavailable reports that no preview service is connected.
var ErrAccessPreviewUnavailable = errors.New("productclient: access preview service unavailable")

// AccessPreviewFromResponse qualifies a PreviewRoleAccess answer through the
// AccessPreview contract before anything renders it (REV-093-01). The
// answer must be about the role that was asked for, name its role sources,
// report a resolved scope, account explicitly for every unit it hides, and,
// for an administrator role, keep the unrestricted scope unchanged. A
// response that fails any of these is refused rather than shown.
func AccessPreviewFromResponse(requestedRole string, response *journeyv1.PreviewRoleAccessResponse) (AccessPreview, error) {
	if response == nil {
		return AccessPreview{}, fmt.Errorf("%w: empty response", ErrAccessPreviewInvalid)
	}
	if !strings.EqualFold(strings.TrimSpace(response.GetRoleId()), strings.TrimSpace(requestedRole)) {
		return AccessPreview{}, fmt.Errorf("%w: preview answers role %q, asked for %q", ErrAccessPreviewInvalid, response.GetRoleId(), requestedRole)
	}
	name := strings.TrimSpace(response.GetRoleName())
	if name == "" {
		name = response.GetRoleId()
	}
	preview := AccessPreview{
		RoleName:       name,
		ExplicitRoles:  append([]string(nil), response.GetExplicitRoles()...),
		InheritedRoles: append([]string(nil), response.GetInheritedRoles()...),
		EffectiveScope: effectiveScopeText(response),
		CurrentUnits:   append([]string(nil), response.GetCurrent().GetOrganizationUnits()...),
		ProposedUnits:  append([]string(nil), response.GetProposed().GetOrganizationUnits()...),
		RemovedUnits:   append([]string(nil), response.GetRemovedUnits()...),
		Unrestricted:   response.GetAdministratorOverride(),
		WithheldNote:   withheldText(response),
		NextAction:     nextActionText(response),
	}
	if err := preview.Validate(); err != nil {
		return AccessPreview{}, err
	}
	return preview, nil
}

func effectiveScopeText(response *journeyv1.PreviewRoleAccessResponse) string {
	proposed := response.GetProposed()
	switch {
	case response.GetAdministratorOverride():
		return "Every organization unit (administrator override)"
	case proposed.GetRelative():
		return "Each person's own organization unit"
	case strings.EqualFold(proposed.GetMode(), "ALL"):
		return "Every organization unit"
	case len(proposed.GetOrganizationUnits()) == 0:
		return "No organization units"
	default:
		return namedList(proposed.GetOrganizationUnits(), "none")
	}
}

func withheldText(response *journeyv1.PreviewRoleAccessResponse) string {
	note := "Everyone always sees their own record, and people managers also see their reporting line."
	if count := response.GetOverriddenHolderCount(); count > 0 && !response.GetAdministratorOverride() {
		note += " Administrators among the people with this role (" + strconv.Itoa(int(count)) + ") see every unit."
	}
	return note
}

func nextActionText(response *journeyv1.PreviewRoleAccessResponse) string {
	switch {
	case response.GetAdministratorOverride():
		return "Saving does not narrow this role; administrators always see every unit."
	case len(response.GetAddedUnits())+len(response.GetRemovedUnits()) > 0:
		return "Review the proposed scope, then save to apply it."
	default:
		return ""
	}
}

// RoleAccessPreviewView projects a qualified preview and its server answer
// onto the visibility editor's display type.
func RoleAccessPreviewView(response *journeyv1.PreviewRoleAccessResponse) productui.RoleAccessPreview {
	return productui.RoleAccessPreview{
		RoleID: response.GetRoleId(), RoleName: response.GetRoleName(),
		ExplicitRoles:         append([]string(nil), response.GetExplicitRoles()...),
		InheritedRoles:        append([]string(nil), response.GetInheritedRoles()...),
		AdministratorOverride: response.GetAdministratorOverride(),
		CurrentMode:           response.GetCurrent().GetMode(),
		CurrentUnits:          append([]string(nil), response.GetCurrent().GetOrganizationUnits()...),
		CurrentRelative:       response.GetCurrent().GetRelative(),
		ProposedMode:          response.GetProposed().GetMode(),
		ProposedUnits:         append([]string(nil), response.GetProposed().GetOrganizationUnits()...),
		ProposedRelative:      response.GetProposed().GetRelative(),
		AddedUnits:            append([]string(nil), response.GetAddedUnits()...),
		RemovedUnits:          append([]string(nil), response.GetRemovedUnits()...),
		HolderCount:           int(response.GetHolderCount()),
		OverriddenHolderCount: int(response.GetOverriddenHolderCount()),
	}
}

// PreviewRoleAccess asks the server to resolve what policy would reveal and
// returns it only after the AccessPreview contract accepts it. The browser
// supplies the draft; the server decides every unit in the answer.
func PreviewRoleAccess(ctx context.Context, service journeyclient.AccessPreviewService, policy productui.OrganizationVisibilityPolicy) (productui.RoleAccessPreview, error) {
	if service == nil {
		return productui.RoleAccessPreview{}, ErrAccessPreviewUnavailable
	}
	response, err := service.PreviewRoleAccess(ctx, &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{
		Version: policy.Version, RoleId: policy.RoleID, Mode: policy.Mode, OrganizationUnits: append([]string(nil), policy.OrganizationUnits...),
	}})
	if err != nil {
		return productui.RoleAccessPreview{}, err
	}
	if _, err := AccessPreviewFromResponse(policy.RoleID, response); err != nil {
		return productui.RoleAccessPreview{}, err
	}
	return RoleAccessPreviewView(response), nil
}
