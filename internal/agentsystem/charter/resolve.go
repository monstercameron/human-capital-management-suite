package agentcharter

import (
	"fmt"
)

// Resolve composes the mandatory platform, tenant, entity, agent,
// installation, and run layers (plus an optional pack). Allows intersect,
// denies accumulate at every layer, and the request is bound to stable IDs
// supplied by the caller's current organization authorization decision.
func Resolve(input Input) (Result, error) {
	if err := validateInput(input); err != nil {
		return Result{}, err
	}
	result := Result{
		EffectiveAt:    input.Context.EffectiveAt,
		Organizations:  emptyResolved[OrganizationID](),
		Jurisdictions:  emptyResolved[JurisdictionID](),
		Sources:        emptyResolved[CapabilityID](),
		Tools:          emptyResolved[CapabilityID](),
		DataCategories: emptyResolved[DataCategory](),
		Decisions:      emptyResolved[DecisionID](),
	}
	for _, layer := range input.Layers {
		source := layer.source()
		result.Organizations = compose(result.Organizations, layer.Settings.Organizations, source)
		result.Jurisdictions = compose(result.Jurisdictions, layer.Settings.Jurisdictions, source)
		result.Sources = compose(result.Sources, layer.Settings.Sources, source)
		result.Tools = compose(result.Tools, layer.Settings.Tools, source)
		result.DataCategories = compose(result.DataCategories, layer.Settings.DataCategories, source)
		result.Decisions = compose(result.Decisions, layer.Settings.Decisions, source)
		if !result.EscalationOwner.Set && layer.Settings.EscalationOwner.Set {
			result.EscalationOwner = ResolvedValue[PrincipalID]{Set: true, Value: layer.Settings.EscalationOwner.Value, Source: sourcePtr(source)}
		}
		if !result.SuccessMeasures.Set && layer.Settings.SuccessMeasures.Set {
			result.SuccessMeasures = ResolvedValue[string]{Set: true, Value: layer.Settings.SuccessMeasures.Value, Source: sourcePtr(source)}
		}
	}
	result.Organizations.Allowed = intersection(result.Organizations.Allowed, organizationIDs(input.Context.Organizations))
	result.Organizations.Allowed = subtract(result.Organizations.Allowed, result.Organizations.Denied)
	if len(result.Organizations.Allowed) == 0 {
		return Result{}, fmt.Errorf("%w: current organization scope has no effective entities", ErrInvalid)
	}
	return result, nil
}

func validateInput(input Input) error {
	ctx := input.Context
	if !validText(ctx.TenantID) || !validText(ctx.RunID) || ctx.EffectiveAt.IsZero() || len(ctx.Organizations) == 0 {
		return fmt.Errorf("%w: tenant, run, effective time, and authorized organization IDs are required", ErrInvalid)
	}
	seenOrganizations := make(map[OrganizationID]struct{}, len(ctx.Organizations))
	for _, organization := range ctx.Organizations {
		if !validText(string(organization.ID)) || organization.EffectiveFrom.IsZero() || (!organization.EffectiveUntil.IsZero() && !organization.EffectiveUntil.After(organization.EffectiveFrom)) {
			return fmt.Errorf("%w: organization scope requires stable IDs and valid effective intervals", ErrInvalid)
		}
		if ctx.EffectiveAt.Before(organization.EffectiveFrom) || (!organization.EffectiveUntil.IsZero() && !ctx.EffectiveAt.Before(organization.EffectiveUntil)) {
			return fmt.Errorf("%w: organization %q is not effective at requested time", ErrInvalid, organization.ID)
		}
		if _, exists := seenOrganizations[organization.ID]; exists {
			return fmt.Errorf("%w: authorized organization IDs contain duplicates", ErrInvalid)
		}
		seenOrganizations[organization.ID] = struct{}{}
	}
	var expected []LayerKind
	for _, kind := range resolutionOrder() {
		if kind == LayerPack {
			continue
		}
		expected = append(expected, kind)
	}
	if len(input.Layers) != len(expected) && len(input.Layers) != len(expected)+1 {
		return fmt.Errorf("%w: expected required layers and at most one pack", ErrInvalid)
	}
	index := 0
	seen := make(map[LayerKind]bool, len(input.Layers))
	for _, kind := range resolutionOrder() {
		if kind == LayerPack && (index >= len(input.Layers) || input.Layers[index].Kind != LayerPack) {
			continue
		}
		if index >= len(input.Layers) || input.Layers[index].Kind != kind {
			return fmt.Errorf("%w: layers must follow platform, tenant, entity, optional pack, agent, installation, run order", ErrInvalid)
		}
		layer := input.Layers[index]
		if seen[kind] {
			return fmt.Errorf("%w: duplicate %s layer", ErrInvalid, kind)
		}
		seen[kind] = true
		if err := validateLayer(layer, ctx.EffectiveAt); err != nil {
			return err
		}
		switch kind {
		case LayerPlatform:
			if layer.ID != "platform" {
				return fmt.Errorf("%w: platform layer must use the platform stable ID", ErrInvalid)
			}
		case LayerTenant:
			if layer.ID != ctx.TenantID {
				return fmt.Errorf("%w: tenant layer does not match authorized tenant ID", ErrInvalid)
			}
		case LayerEntity:
			if len(ctx.Organizations) != 1 || layer.ID != string(ctx.Organizations[0].ID) {
				return fmt.Errorf("%w: entity layer must match the single authorized stable organization ID", ErrInvalid)
			}
		case LayerRun:
			if layer.ID != ctx.RunID {
				return fmt.Errorf("%w: run layer does not match request run ID", ErrInvalid)
			}
		}
		index++
	}
	if index != len(input.Layers) {
		return fmt.Errorf("%w: unexpected layer", ErrInvalid)
	}
	return nil
}

func compose[T ~string](current ResolvedConstraint[T], rule Constraint[T], source Source) ResolvedConstraint[T] {
	if !rule.Specified {
		return current
	}
	// A nil Allow means that this layer supplies no allow ceiling. A
	// non-nil empty Allow is an explicit empty ceiling and must remain
	// distinguishable from a deny-only rule.
	if rule.Allow != nil {
		allowed := ordered(rule.Allow)
		if len(current.Sources) == 0 {
			current.Allowed = allowed
		} else {
			current.Allowed = intersection(current.Allowed, allowed)
		}
	}
	current.Denied = ordered(append(current.Denied, rule.Deny...))
	current.Allowed = subtract(current.Allowed, current.Denied)
	current.Sources = append(current.Sources, source)
	return current
}

func sourcePtr(source Source) *Source { return &source }

func emptyResolved[T ~string]() ResolvedConstraint[T] {
	return ResolvedConstraint[T]{Allowed: []T{}, Denied: []T{}, Sources: []Source{}}
}

func organizationIDs(refs []OrganizationRef) []OrganizationID {
	ids := make([]OrganizationID, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	return ordered(ids)
}

func intersection[T ~string](left, right []T) []T {
	set := make(map[T]struct{}, len(right))
	for _, item := range right {
		set[item] = struct{}{}
	}
	result := make([]T, 0, len(left))
	for _, item := range left {
		if _, ok := set[item]; ok {
			result = append(result, item)
		}
	}
	return ordered(result)
}

func subtract[T ~string](values, denied []T) []T {
	blocked := make(map[T]struct{}, len(denied))
	for _, item := range denied {
		blocked[item] = struct{}{}
	}
	result := make([]T, 0, len(values))
	for _, item := range values {
		if _, ok := blocked[item]; !ok {
			result = append(result, item)
		}
	}
	return ordered(result)
}
