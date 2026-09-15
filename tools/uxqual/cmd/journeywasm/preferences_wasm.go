//go:build js && wasm

package main

import (
	"context"
	"errors"
	"sync"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/protobuf/proto"
)

// serverPreferenceController is a one-lane actor for preference writes.
// Keeping preference writes serialized prevents CAS conflicts without ever
// blocking route loading or user input on network latency.
type serverPreferenceController struct {
	ctx                    context.Context
	service                journeyclient.PreferenceService
	jobs                   chan func()
	mu                     sync.Mutex
	user                   *journeyv1.UserPreferences
	theme                  *journeyv1.CustomerTheme
	organizationVisibility *journeyv1.OrganizationVisibilityPolicy
	lastUse                string
}

func newServerPreferenceController(ctx context.Context, service journeyclient.Service) *serverPreferenceController {
	controller := &serverPreferenceController{ctx: ctx, jobs: make(chan func(), 64), user: &journeyv1.UserPreferences{}, theme: &journeyv1.CustomerTheme{}}
	controller.service, _ = service.(journeyclient.PreferenceService)
	go func() {
		for job := range controller.jobs {
			job()
		}
	}()
	return controller
}

func (c *serverPreferenceController) enqueue(job func()) {
	if c == nil || c.service == nil {
		return
	}
	select {
	case c.jobs <- job:
	default:
		go func() { c.jobs <- job }()
	}
}

func (c *serverPreferenceController) Adopt(view productui.View) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stored := view.StoredPreferences
	if stored.Version >= c.user.GetVersion() {
		tables := make(map[string]*journeyv1.TablePreferences, len(stored.Tables))
		for key, table := range stored.Tables {
			tables[key] = &journeyv1.TablePreferences{PageSize: int32(table.PageSize), Filters: table.Filters, Sort: table.Sort, Direction: table.Direction}
		}
		groups := make(map[string]bool, len(stored.NavigationGroups))
		for page, open := range stored.NavigationGroups {
			groups[string(page)] = open
		}
		favorites := make([]string, 0, len(stored.FavoritePages))
		for _, page := range stored.FavoritePages {
			favorites = append(favorites, string(page))
		}
		access := stored.Accessibility
		c.user = &journeyv1.UserPreferences{Version: stored.Version, Locale: stored.Locale, NavCollapsed: stored.NavCollapsed, NavigationGroups: groups, FavoritePages: favorites, Tables: tables, WorkflowUses: stored.WorkflowUses,
			Accessibility: &journeyv1.AccessibilityPreferences{TextSize: access.TextSize, Contrast: access.Contrast, Motion: access.Motion, Links: access.Links}}
	}
	if view.AppearanceVersion >= c.theme.GetVersion() {
		c.theme = themeToProto(view.Appearance, view.AppearanceVersion)
	}
	if view.OrganizationVisibility.Version >= c.organizationVisibility.GetVersion() {
		c.organizationVisibility = &journeyv1.OrganizationVisibilityPolicy{Version: view.OrganizationVisibility.Version, Mode: view.OrganizationVisibility.Mode, OrganizationUnits: append([]string(nil), view.OrganizationVisibility.OrganizationUnits...)}
	}
}

func (c *serverPreferenceController) SaveOrganizationVisibility(policy productui.OrganizationVisibilityPolicy, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("organization visibility service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		c.mu.Lock()
		request := &journeyv1.OrganizationVisibilityPolicy{Version: c.organizationVisibility.GetVersion(), Mode: policy.Mode, OrganizationUnits: append([]string(nil), policy.OrganizationUnits...)}
		c.mu.Unlock()
		response, err := c.service.SaveOrganizationVisibility(c.ctx, &journeyv1.SaveOrganizationVisibilityRequest{Policy: request})
		if err == nil {
			c.mu.Lock()
			c.organizationVisibility = response.GetPolicy()
			c.mu.Unlock()
		}
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveAccessRole(role productui.AccessRole, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("role access service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		_, err := c.service.SaveAccessRole(c.ctx, &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{Version: role.Version, RoleId: role.ID, Name: role.Name, Description: role.Description, System: role.System, Active: role.Active}})
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveWorkerRoleAssignment(assignment productui.WorkerRoleAssignment, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("role access service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		_, err := c.service.SaveWorkerRoleAssignment(c.ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{Version: assignment.Version, WorkerRef: assignment.WorkerRef, RoleIds: append([]string(nil), assignment.RoleIDs...)}})
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveRoleVisibility(policy productui.OrganizationVisibilityPolicy, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("role access service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		_, err := c.service.SaveRoleOrganizationVisibility(c.ctx, &journeyv1.SaveRoleOrganizationVisibilityRequest{Policy: &journeyv1.RoleOrganizationVisibilityPolicy{Version: policy.Version, RoleId: policy.RoleID, Mode: policy.Mode, OrganizationUnits: append([]string(nil), policy.OrganizationUnits...)}})
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveRolePagePermission(permission productui.RolePagePermission, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("role access service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		_, err := c.service.SaveRolePagePermission(c.ctx, &journeyv1.SaveRolePagePermissionRequest{Permission: &journeyv1.RolePagePermission{
			Version: permission.Version, RoleId: permission.RoleID, PageId: string(permission.Page),
			CanView: permission.View, CanCreate: permission.Create, CanUpdate: permission.Update, CanDelete: permission.Delete,
		}})
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveRoleFeaturePermission(permission productui.RoleFeaturePermission, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("role access service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		_, err := c.service.SaveRoleFeaturePermission(c.ctx, &journeyv1.SaveRoleFeaturePermissionRequest{Permission: &journeyv1.RoleFeaturePermission{
			Version: permission.Version, RoleId: permission.RoleID, PageId: string(permission.Page), FeatureId: string(permission.Feature),
			CanView: permission.View, CanCreate: permission.Create, CanUpdate: permission.Update, CanDelete: permission.Delete,
		}})
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveTheme(theme productui.CustomerTheme, done func(error)) {
	c.enqueue(func() {
		c.mu.Lock()
		request := themeToProto(theme, c.theme.GetVersion())
		c.mu.Unlock()
		response, err := c.service.SaveTenantAppearance(c.ctx, &journeyv1.SaveTenantAppearanceRequest{Theme: request})
		if err == nil {
			c.mu.Lock()
			c.theme = response.GetTheme()
			c.mu.Unlock()
		}
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveWorkerIDPolicy(policy productui.WorkerIDPolicy, done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("worker ID policy service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		request := &journeyv1.WorkerIDPolicy{Version: policy.Version, Prefix: policy.Prefix, Suffix: policy.Suffix, Separator: policy.Separator, SequenceDigits: int32(policy.SequenceDigits), StartAt: policy.StartAt, NextSequence: policy.NextSequence, IncrementBy: policy.IncrementBy, ZeroPad: policy.ZeroPad, YearFormat: policy.YearFormat, IncludeUnitCode: policy.IncludeUnitCode, CheckDigit: policy.CheckDigit, ExcludedRanges: policy.ExcludedRanges, IssuedCount: policy.IssuedCount}
		_, err := c.service.SaveWorkerIDPolicy(c.ctx, &journeyv1.SaveWorkerIDPolicyRequest{Policy: request})
		if done != nil {
			done(err)
		}
	})
}

func (c *serverPreferenceController) SaveAccessibility(value productui.AccessibilityPreferences, done func(error)) {
	c.saveUser(func(user *journeyv1.UserPreferences) {
		user.Accessibility = &journeyv1.AccessibilityPreferences{TextSize: value.TextSize, Contrast: value.Contrast, Motion: value.Motion, Links: value.Links}
	}, done)
}

func (c *serverPreferenceController) SaveNavigationGroups(groups map[string]bool) {
	c.saveUser(func(user *journeyv1.UserPreferences) { user.NavigationGroups = groups }, nil)
}

func (c *serverPreferenceController) PersistView(view productui.View) {
	c.saveUser(func(user *journeyv1.UserPreferences) {
		user.Locale, user.NavCollapsed = view.Locale.Resolved, view.NavCollapsed
		user.FavoritePages = user.FavoritePages[:0]
		for _, page := range view.FavoritePages {
			user.FavoritePages = append(user.FavoritePages, string(page))
		}
		if user.Tables == nil {
			user.Tables = map[string]*journeyv1.TablePreferences{}
		}
		if view.Page == productui.PagePeople {
			user.Tables["people"] = &journeyv1.TablePreferences{PageSize: int32(view.PeoplePageSize), Filters: map[string]string{"query": view.Query, "team": view.PeopleTeam, "location": view.PeopleLocation}, Sort: view.PeopleSort, Direction: view.PeopleDirection}
		}
		if view.Page == productui.PageHistory || view.Page == productui.PagePerson {
			user.Tables["history"] = &journeyv1.TablePreferences{PageSize: int32(view.HistoryPageSize), Filters: map[string]string{"query": view.HistoryQuery, "outcome": view.HistoryOutcome, "person": view.HistoryPerson, "year": view.HistoryYear}, Sort: view.HistorySort, Direction: view.HistoryDirection}
		}
		// UXAUDIT-017: My Work's tab filter is a one-field table preference,
		// the same retention mechanism UXAUDIT-008 gave People and History.
		if view.Page == productui.PageWork {
			user.Tables["work"] = &journeyv1.TablePreferences{Filters: map[string]string{"filter": view.WorkFilter}}
		}
	}, nil)
}

func (c *serverPreferenceController) RecordWorkflowUse(workflow, worker string) {
	key := workflow + "\x00" + worker
	c.mu.Lock()
	if key == c.lastUse {
		c.mu.Unlock()
		return
	}
	c.lastUse = key
	c.mu.Unlock()
	c.enqueue(func() {
		response, err := c.service.RecordWorkflowUse(c.ctx, &journeyv1.RecordWorkflowUseRequest{WorkflowId: workflow})
		if err == nil {
			c.mu.Lock()
			c.user = response.GetUser()
			c.mu.Unlock()
		}
	})
}

func (c *serverPreferenceController) ResetWorkflowUseMarker() {
	c.mu.Lock()
	c.lastUse = ""
	c.mu.Unlock()
}

func (c *serverPreferenceController) saveUser(mutate func(*journeyv1.UserPreferences), done func(error)) {
	if c == nil || c.service == nil {
		if done != nil {
			done(errors.New("preference service unavailable"))
		}
		return
	}
	c.enqueue(func() {
		c.mu.Lock()
		before := proto.Clone(c.user).(*journeyv1.UserPreferences)
		candidate := proto.Clone(c.user).(*journeyv1.UserPreferences)
		mutate(candidate)
		c.mu.Unlock()
		if proto.Equal(before, candidate) {
			if done != nil {
				done(nil)
			}
			return
		}
		response, err := c.service.SaveUserPreferences(c.ctx, &journeyv1.SaveUserPreferencesRequest{User: candidate})
		if err == nil {
			c.mu.Lock()
			c.user = response.GetUser()
			c.mu.Unlock()
		}
		if done != nil {
			done(err)
		}
	})
}

func themeToProto(theme productui.CustomerTheme, version int64) *journeyv1.CustomerTheme {
	theme = productui.NormalizeCustomerTheme(theme)
	return &journeyv1.CustomerTheme{Version: version, BrandName: theme.BrandName, BrandMark: theme.BrandMark, BrandLogoUrl: theme.BrandLogoURL, ColorMode: theme.ColorMode, Palette: theme.Palette, Shape: theme.Shape, Density: theme.Density, Glyphs: theme.Glyphs, Typeface: theme.Typeface, Navigation: theme.Navigation, Motion: theme.Motion, TokenOverrides: theme.TokenOverrides, DarkTokenOverrides: theme.DarkTokenOverrides}
}
