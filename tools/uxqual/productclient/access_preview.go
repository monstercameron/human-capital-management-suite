package productclient

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrAccessPreviewInvalid rejects an effective-access preview that cannot
// explain what a role reveals (UXSCAN-008). Every rejection names the
// RED it closes: an unreported scope, an assignment without effective-role
// context, a silent replacement, or an additive change with no next step.
var ErrAccessPreviewInvalid = errors.New("productclient: invalid access preview")

// AccessPreview is the server-resolved effective-access summary an
// administrator reviews before changing role visibility. It carries only
// role, unit and scope names the server already resolved: it never carries
// worker records and it never decides access, so the browser cannot widen
// disclosure or substitute its own verdict for enforcement.
type AccessPreview struct {
	RoleName string
	// ExplicitRoles names directly assigned roles that grant visibility.
	// InheritedRoles names transitively granted roles (credential
	// inheritance). At least one source is required whenever any unit is
	// visible: an assignment without effective-role context is the
	// UXSCAN-008 RED.
	ExplicitRoles  []string
	InheritedRoles []string
	// EffectiveScope is the resolved visibility scope. It is always
	// required: "Not reported" while predicting access is the UXSCAN-008
	// RED on the Organization page.
	EffectiveScope string
	// CurrentUnits is what the role reveals today. ProposedUnits is what
	// it would reveal after the change and must be additive: a proposed
	// set that silently drops a visible unit is a replacement, which this
	// contract does not explain.
	CurrentUnits  []string
	ProposedUnits []string
	// WithheldNote accounts for facts that stay hidden, when any do.
	WithheldNote string
	// NextAction is the task-specific next step. It is required whenever
	// the change reveals new units, so an additive edit stays deliberate.
	NextAction string
}

// Validate enforces the preview contract before anything is explained.
func (p AccessPreview) Validate() error {
	if strings.TrimSpace(p.RoleName) == "" {
		return fmt.Errorf("%w: role name is required", ErrAccessPreviewInvalid)
	}
	if strings.TrimSpace(p.EffectiveScope) == "" ||
		strings.EqualFold(strings.TrimSpace(p.EffectiveScope), "not reported") {
		return fmt.Errorf("%w: effective scope must be server-resolved", ErrAccessPreviewInvalid)
	}
	if len(p.CurrentUnits)+len(p.ProposedUnits) > 0 &&
		len(p.ExplicitRoles)+len(p.InheritedRoles) == 0 {
		return fmt.Errorf("%w: visible units need an explicit or inherited role source", ErrAccessPreviewInvalid)
	}
	current := unitSet(p.CurrentUnits)
	proposed := unitSet(p.ProposedUnits)
	for unit := range current {
		if !proposed[unit] {
			return fmt.Errorf("%w: proposed scope drops visible unit %q", ErrAccessPreviewInvalid, unit)
		}
	}
	additions := false
	for unit := range proposed {
		if !current[unit] {
			additions = true
			break
		}
	}
	if additions && strings.TrimSpace(p.NextAction) == "" {
		return fmt.Errorf("%w: an additive change needs a next action", ErrAccessPreviewInvalid)
	}
	return nil
}

// Explain renders the preview in task-oriented words: explicit grants,
// inherited grants, effective scope, current versus proposed visibility,
// withheld facts and the next step. The output is deterministic across
// input ordering and carries no glyph, color or verdict, so assistive
// technology receives the same explanation as the visual page.
func (p AccessPreview) Explain() string {
	proposed := sortedUnits(p.ProposedUnits)
	var lines []string
	switch len(proposed) {
	case 0:
		lines = append(lines, fmt.Sprintf("Role %q would reveal no organization units.", p.RoleName))
	case 1:
		lines = append(lines, fmt.Sprintf("Role %q would reveal 1 organization unit.", p.RoleName))
	default:
		lines = append(lines, fmt.Sprintf("Role %q would reveal %d organization units.", p.RoleName, len(proposed)))
	}
	lines = append(lines, "Explicitly granted: "+namedList(p.ExplicitRoles, "none")+".")
	lines = append(lines, "Inherited: "+namedList(p.InheritedRoles, "none")+".")
	lines = append(lines, "Effective scope: "+strings.TrimSpace(p.EffectiveScope)+".")
	lines = append(lines, "Currently visible: "+namedList(p.CurrentUnits, "none")+".")
	lines = append(lines, "Newly visible: "+namedList(difference(p.ProposedUnits, p.CurrentUnits), "none")+".")
	if note := strings.TrimSpace(p.WithheldNote); note != "" {
		lines = append(lines, "Withheld: "+note)
	}
	if action := strings.TrimSpace(p.NextAction); action != "" {
		lines = append(lines, "Next: "+action)
	}
	return strings.Join(lines, "\n") + "\n"
}

func unitSet(units []string) map[string]bool {
	set := make(map[string]bool, len(units))
	for _, unit := range units {
		if key := strings.TrimSpace(unit); key != "" {
			set[key] = true
		}
	}
	return set
}

func sortedUnits(units []string) []string {
	set := unitSet(units)
	out := make([]string, 0, len(set))
	for unit := range set {
		out = append(out, unit)
	}
	sort.Strings(out)
	return out
}

func difference(proposed, current []string) []string {
	have := unitSet(current)
	var out []string
	for _, unit := range sortedUnits(proposed) {
		if !have[unit] {
			out = append(out, unit)
		}
	}
	return out
}

func namedList(values []string, empty string) string {
	sorted := sortedUnits(values)
	if len(sorted) == 0 {
		return empty
	}
	return strings.Join(sorted, ", ")
}
