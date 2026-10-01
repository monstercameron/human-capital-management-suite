package agentcharter

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrInvalid identifies an invalid layered charter or resolution request.
var ErrInvalid = errors.New("agent charter: invalid input")

// LayerKind identifies one precedence level in charter resolution.
type LayerKind string

const (
	LayerPlatform     LayerKind = "PLATFORM"
	LayerTenant       LayerKind = "TENANT"
	LayerEntity       LayerKind = "ENTITY"
	LayerPack         LayerKind = "PACK"
	LayerAgent        LayerKind = "AGENT"
	LayerInstallation LayerKind = "INSTALLATION"
	LayerRun          LayerKind = "RUN"
)

// Stable identifier types prevent organization names from being used as
// selectors in place of IDs supplied by the organization authorization layer.
type OrganizationID string
type JurisdictionID string
type CapabilityID string
type DataCategory string
type DecisionID string
type PrincipalID string

// OrganizationRef is a stable organization ID and its validity interval.
// Display names are never accepted as organization selectors.
type OrganizationRef struct {
	ID             OrganizationID
	EffectiveFrom  time.Time
	EffectiveUntil time.Time
}

// Constraint is an optional allow ceiling plus unconditional explicit denies.
// Specified distinguishes an omitted policy from a deliberately empty ceiling.
type Constraint[T ~string] struct {
	Specified bool
	Allow     []T
	Deny      []T
}

// Override supplies a default value. Higher-precedence layers win when set.
type Override[T ~string] struct {
	Set   bool
	Value T
}

// Settings is the typed, provider-neutral portion of a business charter.
type Settings struct {
	Organizations   Constraint[OrganizationID]
	Jurisdictions   Constraint[JurisdictionID]
	Sources         Constraint[CapabilityID]
	Tools           Constraint[CapabilityID]
	DataCategories  Constraint[DataCategory]
	Decisions       Constraint[DecisionID]
	EscalationOwner Override[PrincipalID]
	SuccessMeasures Override[string]
}

// Layer is one immutable, effective-dated policy or charter record.
type Layer struct {
	Kind           LayerKind
	ID             string
	Version        uint64
	EffectiveFrom  time.Time
	EffectiveUntil time.Time
	Settings       Settings
}

// Context binds resolution to organization IDs already selected by current
// organization authorization. Display names and name lookup are intentionally
// absent; this package never resolves an organization by matching text.
type Context struct {
	TenantID      string
	Organizations []OrganizationRef
	RunID         string
	EffectiveAt   time.Time
}

// Input contains the ordered policy chain. Pack is optional; every other layer
// is mandatory and must occur exactly once.
type Input struct {
	Context Context
	Layers  []Layer
}

// Source identifies the immutable record that contributed a setting.
type Source struct {
	Layer   LayerKind `json:"layer"`
	ID      string    `json:"id"`
	Version uint64    `json:"version"`
}

// ResolvedConstraint is the effective ceiling after allow intersection and
// deny union. Sources lists every layer that contributed to this setting.
type ResolvedConstraint[T ~string] struct {
	Allowed []T      `json:"allowed"`
	Denied  []T      `json:"denied"`
	Sources []Source `json:"sources"`
}

// ResolvedValue is the winning non-authority default and its origin.
type ResolvedValue[T ~string] struct {
	Set    bool    `json:"set"`
	Value  T       `json:"value,omitempty"`
	Source *Source `json:"source,omitempty"`
}

// Result explains the source chain for every effective setting.
type Result struct {
	EffectiveAt     time.Time                          `json:"effective_at"`
	Organizations   ResolvedConstraint[OrganizationID] `json:"organizations"`
	Jurisdictions   ResolvedConstraint[JurisdictionID] `json:"jurisdictions"`
	Sources         ResolvedConstraint[CapabilityID]   `json:"sources"`
	Tools           ResolvedConstraint[CapabilityID]   `json:"tools"`
	DataCategories  ResolvedConstraint[DataCategory]   `json:"data_categories"`
	Decisions       ResolvedConstraint[DecisionID]     `json:"decisions"`
	EscalationOwner ResolvedValue[PrincipalID]         `json:"escalation_owner"`
	SuccessMeasures ResolvedValue[string]              `json:"success_measures"`
}

func (l Layer) source() Source { return Source{Layer: l.Kind, ID: l.ID, Version: l.Version} }

func resolutionOrder() []LayerKind {
	return []LayerKind{LayerPlatform, LayerTenant, LayerEntity, LayerPack, LayerAgent, LayerInstallation, LayerRun}
}

func validText(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.TrimSpace(value) == value
}

func validateConstraint[T ~string](name string, value Constraint[T]) error {
	if !value.Specified && (len(value.Allow) > 0 || len(value.Deny) > 0) {
		return fmt.Errorf("%w: %s has values but is not specified", ErrInvalid, name)
	}
	for _, group := range []struct {
		label  string
		values []T
	}{{"allow", value.Allow}, {"deny", value.Deny}} {
		label, values := group.label, group.values
		seen := make(map[T]struct{}, len(values))
		for _, item := range values {
			if !validText(string(item)) {
				return fmt.Errorf("%w: %s.%s contains an empty or noncanonical ID", ErrInvalid, name, label)
			}
			if _, ok := seen[item]; ok {
				return fmt.Errorf("%w: %s.%s contains duplicate IDs", ErrInvalid, name, label)
			}
			seen[item] = struct{}{}
		}
	}
	return nil
}

func validateLayer(layer Layer, at time.Time) error {
	if layer.Kind == "" || !validText(layer.ID) || layer.Version == 0 || layer.EffectiveFrom.IsZero() {
		return fmt.Errorf("%w: layer kind, stable ID, positive version, and effective start are required", ErrInvalid)
	}
	if !layer.EffectiveUntil.IsZero() && !layer.EffectiveUntil.After(layer.EffectiveFrom) {
		return fmt.Errorf("%w: %s effective interval is empty", ErrInvalid, layer.Kind)
	}
	if at.Before(layer.EffectiveFrom) || (!layer.EffectiveUntil.IsZero() && !at.Before(layer.EffectiveUntil)) {
		return fmt.Errorf("%w: %s layer is not effective at requested time", ErrInvalid, layer.Kind)
	}
	checks := []struct {
		name  string
		check func() error
	}{
		{"organizations", func() error { return validateConstraint("organizations", layer.Settings.Organizations) }},
		{"jurisdictions", func() error { return validateConstraint("jurisdictions", layer.Settings.Jurisdictions) }},
		{"sources", func() error { return validateConstraint("sources", layer.Settings.Sources) }},
		{"tools", func() error { return validateConstraint("tools", layer.Settings.Tools) }},
		{"data_categories", func() error { return validateConstraint("data_categories", layer.Settings.DataCategories) }},
		{"decisions", func() error { return validateConstraint("decisions", layer.Settings.Decisions) }},
	}
	for _, check := range checks {
		if err := check.check(); err != nil {
			return err
		}
	}
	if layer.Settings.EscalationOwner.Set && !validText(string(layer.Settings.EscalationOwner.Value)) {
		return fmt.Errorf("%w: escalation owner must be a stable ID", ErrInvalid)
	}
	if layer.Settings.SuccessMeasures.Set && !validText(layer.Settings.SuccessMeasures.Value) {
		return fmt.Errorf("%w: success measures must be nonempty when specified", ErrInvalid)
	}
	return nil
}

func ordered[T ~string](values []T) []T {
	result := append([]T{}, values...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	if len(result) < 2 {
		return result
	}
	unique := result[:1]
	for _, item := range result[1:] {
		if item != unique[len(unique)-1] {
			unique = append(unique, item)
		}
	}
	return unique
}
