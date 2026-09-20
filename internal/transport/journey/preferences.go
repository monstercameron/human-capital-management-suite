package journey

import (
	"context"
	"errors"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s *server) preferenceStore(principal *trust.Principal, requestID string) (preferences.Store, *envelope.Error) {
	if s.deps.Preferences == nil {
		return nil, envelope.New(envelope.CodeUnavailable, "journey.preferences.store_unconfigured", "product preferences are not configured").
			WithCorrelation(requestID).WithEvidence(evidence(principal))
	}
	return s.deps.Preferences, nil
}

func preferenceError(err error, principal *trust.Principal, requestID, operation string) error {
	code, reason := envelope.CodeUnspecified, "store_failed"
	switch {
	case errors.Is(err, preferences.ErrVersionConflict):
		code, reason = envelope.CodeAborted, "version_conflict"
	case errors.Is(err, preferences.ErrInvalid):
		code, reason = envelope.CodeInvalidArgument, "invalid"
	case errors.Is(err, preferences.ErrUnavailable):
		code, reason = envelope.CodeUnavailable, "store_unavailable"
	}
	return envelope.New(code, "journey.preferences."+operation+"."+reason, "the product preference operation could not be completed").
		WithCorrelation(requestID).WithEvidence(evidence(principal))
}

func (s *server) GetProductPreferences(ctx context.Context, _ *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	store, depErr := s.preferenceStore(principal, inv.RequestID())
	if depErr != nil {
		return nil, depErr
	}
	snapshot, err := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "load")
	}
	return &journeyv1.GetProductPreferencesResponse{User: toUserPreferences(snapshot.User), Theme: toCustomerTheme(snapshot.Theme), OrganizationVisibility: toOrganizationVisibility(snapshot.OrganizationVisibility)}, nil
}

func (s *server) SaveUserPreferences(ctx context.Context, req *journeyv1.SaveUserPreferencesRequest) (*journeyv1.SaveUserPreferencesResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "settings", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	store, depErr := s.preferenceStore(principal, inv.RequestID())
	if depErr != nil {
		return nil, depErr
	}
	value, err := store.SaveUser(ctx, principal.Tenant(), principal.Subject(), fromUserPreferences(req.GetUser()))
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "save_user")
	}
	return &journeyv1.SaveUserPreferencesResponse{User: toUserPreferences(value)}, nil
}

func (s *server) SaveTenantAppearance(ctx context.Context, req *journeyv1.SaveTenantAppearanceRequest) (*journeyv1.SaveTenantAppearanceResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "appearance", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	if !s.hasEffectiveRole(ctx, principal, "comp_admin") {
		return nil, envelope.New(envelope.CodePermissionDenied, "journey.preferences.appearance.role_required", "organization appearance requires the compensation administrator role").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
	}
	if req == nil || req.GetTheme() == nil {
		return nil, preferenceError(preferences.ErrInvalid, principal, inv.RequestID(), "save_theme")
	}
	if err := productui.ValidateCustomerTheme(productui.CustomerTheme{Palette: req.GetTheme().GetPalette(), TokenOverrides: req.GetTheme().GetTokenOverrides(), DarkTokenOverrides: req.GetTheme().GetDarkTokenOverrides()}); err != nil {
		return nil, preferenceError(preferences.ErrInvalid, principal, inv.RequestID(), "save_theme")
	}
	store, depErr := s.preferenceStore(principal, inv.RequestID())
	if depErr != nil {
		return nil, depErr
	}
	value, err := store.SaveTheme(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), fromCustomerTheme(req.GetTheme()))
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "save_theme")
	}
	return &journeyv1.SaveTenantAppearanceResponse{Theme: toCustomerTheme(value)}, nil
}

func (s *server) SaveOrganizationVisibility(ctx context.Context, req *journeyv1.SaveOrganizationVisibilityRequest) (*journeyv1.SaveOrganizationVisibilityResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "organization-visibility", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	if !s.hasEffectiveRole(ctx, principal, "comp_admin") {
		return nil, envelope.New(envelope.CodePermissionDenied, "journey.preferences.organization_visibility.role_required", "organization visibility requires the compensation administrator role").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
	}
	if req == nil || req.GetPolicy() == nil {
		return nil, preferenceError(preferences.ErrInvalid, principal, inv.RequestID(), "save_organization_visibility")
	}
	value := fromOrganizationVisibility(req.GetPolicy())
	if err := preferences.ValidateOrganizationVisibility(value); err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "save_organization_visibility")
	}
	store, depErr := s.preferenceStore(principal, inv.RequestID())
	if depErr != nil {
		return nil, depErr
	}
	value, err := store.SaveOrganizationVisibility(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), value)
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "save_organization_visibility")
	}
	return &journeyv1.SaveOrganizationVisibilityResponse{Policy: toOrganizationVisibility(value)}, nil
}

func (s *server) RecordWorkflowUse(ctx context.Context, req *journeyv1.RecordWorkflowUseRequest) (*journeyv1.RecordWorkflowUseResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if strings.TrimSpace(req.GetWorkflowId()) == "" {
		return nil, envelope.New(envelope.CodeInvalidArgument, "journey.preferences.workflow_id.required", "workflow_id is required").WithCorrelation(inv.RequestID()).WithEvidence(evidence(principal))
	}
	store, depErr := s.preferenceStore(principal, inv.RequestID())
	if depErr != nil {
		return nil, depErr
	}
	value, err := store.RecordWorkflowUse(ctx, principal.Tenant(), principal.Subject(), req.GetWorkflowId())
	if err != nil {
		return nil, preferenceError(err, principal, inv.RequestID(), "record_workflow_use")
	}
	return &journeyv1.RecordWorkflowUseResponse{User: toUserPreferences(value)}, nil
}

func toUserPreferences(value preferences.User) *journeyv1.UserPreferences {
	tables := make(map[string]*journeyv1.TablePreferences, len(value.Tables))
	for key, table := range value.Tables {
		tables[key] = &journeyv1.TablePreferences{PageSize: int32(table.PageSize), Filters: table.Filters, Sort: table.Sort, Direction: table.Direction}
	}
	return &journeyv1.UserPreferences{Version: value.Version, Locale: value.Locale, NavCollapsed: value.NavCollapsed,
		Accessibility:    &journeyv1.AccessibilityPreferences{TextSize: value.Accessibility.TextSize, Contrast: value.Accessibility.Contrast, Motion: value.Accessibility.Motion, Links: value.Accessibility.Links},
		NavigationGroups: value.NavigationGroups, FavoritePages: value.FavoritePages, Tables: tables, WorkflowUses: value.WorkflowUses,
		Density: value.Density}
}

func fromUserPreferences(value *journeyv1.UserPreferences) preferences.User {
	if value == nil {
		return preferences.NormalizeUser(preferences.User{})
	}
	tables := make(map[string]preferences.TablePreferences, len(value.GetTables()))
	for key, table := range value.GetTables() {
		if table != nil {
			tables[key] = preferences.TablePreferences{PageSize: int(table.GetPageSize()), Filters: table.GetFilters(), Sort: table.GetSort(), Direction: table.GetDirection()}
		}
	}
	access := value.GetAccessibility()
	result := preferences.User{Version: value.GetVersion(), Locale: value.GetLocale(), NavCollapsed: value.GetNavCollapsed(), NavigationGroups: value.GetNavigationGroups(), FavoritePages: value.GetFavoritePages(), Tables: tables, WorkflowUses: value.GetWorkflowUses(), Density: value.GetDensity()}
	if access != nil {
		result.Accessibility = preferences.Accessibility{TextSize: access.GetTextSize(), Contrast: access.GetContrast(), Motion: access.GetMotion(), Links: access.GetLinks()}
	}
	return preferences.NormalizeUser(result)
}

func toCustomerTheme(value preferences.TenantTheme) *journeyv1.CustomerTheme {
	t := value.Theme
	return &journeyv1.CustomerTheme{Version: value.Version, BrandName: t.BrandName, BrandMark: t.BrandMark, BrandLogoUrl: t.BrandLogoURL, ColorMode: t.ColorMode, Palette: t.Palette, Shape: t.Shape, Density: t.Density, Glyphs: t.Glyphs, Typeface: t.Typeface, Navigation: t.Navigation, Motion: t.Motion, TokenOverrides: t.TokenOverrides, DarkTokenOverrides: t.DarkTokenOverrides}
}

func fromCustomerTheme(value *journeyv1.CustomerTheme) preferences.TenantTheme {
	if value == nil {
		return preferences.DefaultSnapshot().Theme
	}
	return preferences.TenantTheme{Version: value.GetVersion(), Theme: preferences.Theme{BrandName: value.GetBrandName(), BrandMark: value.GetBrandMark(), BrandLogoURL: value.GetBrandLogoUrl(), ColorMode: value.GetColorMode(), Palette: value.GetPalette(), Shape: value.GetShape(), Density: value.GetDensity(), Glyphs: value.GetGlyphs(), Typeface: value.GetTypeface(), Navigation: value.GetNavigation(), Motion: value.GetMotion(), TokenOverrides: value.GetTokenOverrides(), DarkTokenOverrides: value.GetDarkTokenOverrides()}}
}

func toOrganizationVisibility(value preferences.OrganizationVisibility) *journeyv1.OrganizationVisibilityPolicy {
	value = preferences.NormalizeOrganizationVisibility(value)
	return &journeyv1.OrganizationVisibilityPolicy{Version: value.Version, Mode: value.Mode, OrganizationUnits: append([]string(nil), value.OrganizationUnits...)}
}

func fromOrganizationVisibility(value *journeyv1.OrganizationVisibilityPolicy) preferences.OrganizationVisibility {
	if value == nil {
		return preferences.OrganizationVisibility{}
	}
	return preferences.OrganizationVisibility{Version: value.GetVersion(), Mode: value.GetMode(), OrganizationUnits: append([]string(nil), value.GetOrganizationUnits()...)}
}
