// Package preferences owns the server-side presentation preferences used by
// the product workspace. These values affect how an authenticated user sees
// the product; they never grant authority or alter an HCM business record.
package preferences

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrUnavailable     = errors.New("preferences: store unavailable")
	ErrVersionConflict = errors.New("preferences: stale version")
	ErrInvalid         = errors.New("preferences: invalid value")
)

const (
	TablePeople  = "people"
	TableHistory = "history"

	OrganizationVisibilityAll       = "ALL"
	OrganizationVisibilityOwnUnit   = "OWN_UNIT"
	OrganizationVisibilityAllowlist = "ALLOWLIST"
	OrganizationVisibilityDenylist  = "DENYLIST"
)

// TablePreferences is the user's last-used collection configuration. Query
// state remains in the URL when explicitly supplied so views stay shareable;
// this record supplies the default on the user's next visit.
type TablePreferences struct {
	PageSize  int               `json:"page_size"`
	Filters   map[string]string `json:"filters,omitempty"`
	Sort      string            `json:"sort,omitempty"`
	Direction string            `json:"direction,omitempty"`
}

// Accessibility is the device-independent accessibility selection for one
// authenticated principal.
type Accessibility struct {
	TextSize string `json:"text_size"`
	Contrast string `json:"contrast"`
	Motion   string `json:"motion"`
	Links    string `json:"links"`
}

// User is the complete replaceable preference document for one principal.
// Version is an optimistic concurrency coordinate and is not serialized into
// the JSON payload stored beside it.
type User struct {
	Version          int64                       `json:"-"`
	Locale           string                      `json:"locale,omitempty"`
	Accessibility    Accessibility               `json:"accessibility"`
	NavCollapsed     bool                        `json:"nav_collapsed"`
	NavigationGroups map[string]bool             `json:"navigation_groups,omitempty"`
	FavoritePages    []string                    `json:"favorite_pages,omitempty"`
	Tables           map[string]TablePreferences `json:"tables,omitempty"`
	WorkflowUses     map[string]int64            `json:"workflow_uses,omitempty"`
	// Density is this principal's own layout density. Empty inherits the
	// organization's Theme.Density; see NormalizeUserDensity (REV-092-01).
	Density string `json:"density,omitempty"`
}

// Theme is organization-wide customer branding. It is intentionally separate
// from User: everyone admitted to the same organization scope shares the
// brand, while locale, navigation, table, and accessibility choices belong to
// one authenticated principal.
type Theme struct {
	BrandName          string            `json:"brand_name"`
	BrandMark          string            `json:"brand_mark"`
	BrandLogoURL       string            `json:"brand_logo_url,omitempty"`
	ColorMode          string            `json:"color_mode"`
	Palette            string            `json:"palette"`
	Shape              string            `json:"shape"`
	Density            string            `json:"density"`
	Glyphs             string            `json:"glyphs"`
	Typeface           string            `json:"typeface"`
	Navigation         string            `json:"navigation"`
	Motion             string            `json:"motion"`
	TokenOverrides     map[string]string `json:"token_overrides,omitempty"`
	DarkTokenOverrides map[string]string `json:"dark_token_overrides,omitempty"`
}

type TenantTheme struct {
	Version             int64  `json:"-"`
	OrganizationScopeID string `json:"-"`
	Theme
}

// OrganizationVisibility is the organization-wide directory boundary chosen
// by an administrator. It controls which organization units an ordinary user
// may receive from ListWorkers; administrators retain the full population so
// they can safely review and change the policy.
type OrganizationVisibility struct {
	Version             int64    `json:"-"`
	OrganizationScopeID string   `json:"-"`
	Mode                string   `json:"mode"`
	OrganizationUnits   []string `json:"organization_units,omitempty"`
}

type Snapshot struct {
	User                   User
	Theme                  TenantTheme
	OrganizationVisibility OrganizationVisibility
}

// Store is the persistence port. Tenant, organization scope, and principal are
// always derived from the admitted request context by the transport adapter.
// The organization coordinate applies only to shared appearance; user
// preferences remain keyed to the principal.
type Store interface {
	Load(context.Context, values.TenantId, string, string) (Snapshot, error)
	SaveUser(context.Context, values.TenantId, string, User) (User, error)
	SaveTheme(context.Context, values.TenantId, string, string, TenantTheme) (TenantTheme, error)
	SaveOrganizationVisibility(context.Context, values.TenantId, string, string, OrganizationVisibility) (OrganizationVisibility, error)
	RecordWorkflowUse(context.Context, values.TenantId, string, string) (User, error)
}

func NormalizeOrganizationVisibility(value OrganizationVisibility) OrganizationVisibility {
	switch strings.ToUpper(strings.TrimSpace(value.Mode)) {
	case OrganizationVisibilityOwnUnit, OrganizationVisibilityAllowlist, OrganizationVisibilityDenylist:
		value.Mode = strings.ToUpper(strings.TrimSpace(value.Mode))
	default:
		value.Mode = OrganizationVisibilityAll
	}
	seen := make(map[string]bool, len(value.OrganizationUnits))
	units := make([]string, 0, len(value.OrganizationUnits))
	for _, unit := range value.OrganizationUnits {
		unit = strings.TrimSpace(unit)
		key := strings.ToLower(unit)
		if unit == "" || seen[key] {
			continue
		}
		seen[key] = true
		units = append(units, unit)
	}
	value.OrganizationUnits = units
	return value
}

// ValidateOrganizationVisibility distinguishes an absent default from a
// malformed stored or submitted policy. Callers must validate before
// normalizing untrusted input so an unknown mode cannot silently widen the
// directory boundary to ALL.
func ValidateOrganizationVisibility(value OrganizationVisibility) error {
	switch strings.ToUpper(strings.TrimSpace(value.Mode)) {
	case OrganizationVisibilityAll, OrganizationVisibilityOwnUnit, OrganizationVisibilityAllowlist, OrganizationVisibilityDenylist:
		return nil
	default:
		return ErrInvalid
	}
}

func NormalizePageSize(value int) int {
	switch value {
	case 10, 20, 50, 100:
		return value
	default:
		return 20
	}
}

func NormalizeUser(value User) User {
	value.Locale = strings.TrimSpace(value.Locale)
	if value.NavigationGroups == nil {
		value.NavigationGroups = map[string]bool{}
	}
	if value.Tables == nil {
		value.Tables = map[string]TablePreferences{}
	}
	for key, table := range value.Tables {
		table.PageSize = NormalizePageSize(table.PageSize)
		if table.Filters == nil {
			table.Filters = map[string]string{}
		}
		value.Tables[key] = table
	}
	if _, ok := value.Tables[TablePeople]; !ok {
		value.Tables[TablePeople] = TablePreferences{PageSize: 20, Filters: map[string]string{}}
	}
	if _, ok := value.Tables[TableHistory]; !ok {
		value.Tables[TableHistory] = TablePreferences{PageSize: 20, Filters: map[string]string{}}
	}
	if value.WorkflowUses == nil {
		value.WorkflowUses = map[string]int64{}
	}
	value.Density = NormalizeUserDensity(value.Density)
	return value
}

func DefaultSnapshot() Snapshot {
	return Snapshot{User: NormalizeUser(User{}), Theme: TenantTheme{Theme: Theme{
		BrandName: "Human Capital Management Suite", BrandMark: "H", ColorMode: "system", Palette: "evergreen",
		Shape: "balanced", Density: "comfortable", Glyphs: "rounded-line", Typeface: "humanist",
		Navigation: "light", Motion: "calm",
	}}, OrganizationVisibility: OrganizationVisibility{Mode: OrganizationVisibilityAll}}
}
