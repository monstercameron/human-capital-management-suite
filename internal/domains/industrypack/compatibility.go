package industrypack

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PACK-004: validate pack compatibility and dependency impact.
//
// A candidate pack version may be published only when every declared
// compatibility range admits the installed platform, country and customer
// versions; every dependency it pins is published at exactly that version and
// digest; it succeeds exactly the published version; no running workflow
// instance is pinned to a workflow definition the candidate drops or
// re-versions without a resolved migration; every changed
// configuration object or rule pack carries a resolved migration; and its
// content and experience bindings (PACK-002/PACK-003) are accepted -- so an
// unknown dependency or a mandatory country rule override blocks too.
//
// A block is PACK_004_REJECTED naming the first offending field, state and
// version, and it lists every impacted object, not just the first. Publication
// persists nothing unless the check passes.

// PublicationRejectionCode is the stable PACK-004 refusal code.
const PublicationRejectionCode = "PACK_004_REJECTED"

// Impact states.
const (
	ImpactIncompatibleVersion = "INCOMPATIBLE_VERSION"
	ImpactComponentMissing    = "COMPONENT_NOT_INSTALLED"
	ImpactMissingDependency   = "MISSING_DEPENDENCY"
	ImpactActiveWorkflowBreak = "ACTIVE_WORKFLOW_BREAK"
	ImpactUnresolvedMigration = "UNRESOLVED_MIGRATION"
	ImpactBindingRejected     = "BINDING_REJECTED"
	ImpactInvalidManifest     = "INVALID_MANIFEST"
	ImpactStaleParent         = "STALE_PARENT"
)

// ErrPublicationBlocked is the sentinel every PACK-004 block unwraps to.
var ErrPublicationBlocked = errors.New("industrypack: pack publication blocked")

// ImpactedObject is one exact object a publication would break.
type ImpactedObject struct {
	Field   string `json:"field"`
	Object  string `json:"object"`
	State   string `json:"state"`
	Version string `json:"version"`
	Detail  string `json:"detail"`
}

// PublicationBlock is the PACK-004 refusal.
type PublicationBlock struct {
	Code     string
	Field    string
	State    string
	Version  string
	Impacted []ImpactedObject
}

func (e *PublicationBlock) Error() string {
	return fmt.Sprintf("%s: %s %s@%s (%d impacted objects)", e.Code, e.Field, e.State, e.Version, len(e.Impacted))
}

// Unwrap exposes the sentinel.
func (e *PublicationBlock) Unwrap() error { return ErrPublicationBlocked }

// PublishedPack is a pack version available as a dependency.
type PublishedPack struct {
	ID      string
	Version string
	Digest  string
}

// ActiveWorkflow is a running instance pinned to a workflow definition.
type ActiveWorkflow struct {
	InstanceID   string
	DefinitionID string
	Version      string
}

// Migration moves pinned state of one object from one version to another.
type Migration struct {
	Kind        string
	ObjectID    string
	FromVersion string
	ToVersion   string
	Resolved    bool
}

// PublicationCheck is the input to [CheckPublication].
type PublicationCheck struct {
	Candidate IndustryPack
	// Prior is the currently published version, if any.
	Prior *IndustryPack
	// Installed maps a compatibility component (platform, country pack,
	// customer pack) to its installed version.
	Installed       map[string]string
	Published       []PublishedPack
	ActiveWorkflows []ActiveWorkflow
	Migrations      []Migration
	// Content and Experience are the candidate's PACK-002 and PACK-003
	// bindings; nil skips them.
	Content    *BindingSpec
	Experience *ExperienceBindingSpec
}

// PublicationReport is an accepted check.
type PublicationReport struct {
	PackID  string
	Version int
	Digest  string
	// Changed lists the objects whose pinned version changed and whose
	// migrations are resolved.
	Changed []ImpactedObject
}

// CheckPublication runs every PACK-004 gate and returns all impacted objects.
func CheckPublication(check PublicationCheck) (PublicationReport, error) {
	cand := check.Candidate
	version := strconv.Itoa(cand.Version)
	var impacted []ImpactedObject
	add := func(field, object, state, v, format string, args ...any) {
		impacted = append(impacted, ImpactedObject{Field: field, Object: object, State: state, Version: v, Detail: fmt.Sprintf(format, args...)})
	}
	if err := cand.Validate(); err != nil {
		add("manifest", cand.packID(), ImpactInvalidManifest, version, "%v", err)
		return PublicationReport{}, block(impacted)
	}

	for i, c := range cand.compatibility() {
		field := fmt.Sprintf("compatibility[%d]", i)
		installed, ok := check.Installed[c.Component]
		if !ok {
			add(field, c.Component, ImpactComponentMissing, "", "component %s is not installed", c.Component)
			continue
		}
		if !versionInRange(installed, c.MinimumVersion, c.MaximumVersion) {
			add(field, c.Component, ImpactIncompatibleVersion, installed, "installed %s is outside [%s, %s]", installed, c.MinimumVersion, c.MaximumVersion)
		}
	}

	published := map[string]PublishedPack{}
	for _, p := range check.Published {
		published[p.ID+"@"+p.Version] = p
	}
	for i, dep := range cand.Dependencies {
		p, ok := published[dep.Identity()+"@"+dep.Version]
		if !ok || dep.Digest != "" && dep.Digest != p.Digest {
			add(fmt.Sprintf("dependencies[%d]", i), dep.Identity(), ImpactMissingDependency, dep.Version, "dependency is not published at this version and digest")
		}
	}

	migrations := map[string]Migration{}
	for _, m := range check.Migrations {
		migrations[m.Kind+"|"+m.ObjectID+"|"+m.FromVersion+"|"+m.ToVersion] = m
	}
	var changed []ImpactedObject
	if prior := check.Prior; prior != nil {
		if priorDigest, _ := prior.Digest(); cand.ParentVersion != prior.Version || cand.ParentDigest != priorDigest {
			add("parent_digest", cand.packID(), ImpactStaleParent, strconv.Itoa(cand.ParentVersion), "the candidate does not succeed the published version %d", prior.Version)
		}
		for _, section := range []struct {
			field      string
			kind       string
			prior, cur []PinnedRef
		}{
			{"workflow_definition_refs", "workflow_definition", prior.WorkflowDefinitionRefs, cand.WorkflowDefinitionRefs},
			{"configuration_object_refs", "configuration_object", prior.ConfigurationObjectRefs, cand.ConfigurationObjectRefs},
			{"rule_pack_refs", "rule_pack", prior.RulePackRefs, cand.RulePackRefs},
		} {
			current := map[string]string{}
			for _, r := range section.cur {
				current[r.Identity()] = r.Version
			}
			for _, old := range sortedRefs(section.prior) {
				next, kept := current[old.Identity()]
				if kept && next == old.Version {
					continue
				}
				m, has := migrations[section.kind+"|"+old.Identity()+"|"+old.Version+"|"+next]
				resolved := has && m.Resolved
				if section.kind == "workflow_definition" {
					for _, w := range check.ActiveWorkflows {
						if w.DefinitionID == old.Identity() && w.Version == old.Version && !resolved {
							add(section.field, old.Identity()+"#"+w.InstanceID, ImpactActiveWorkflowBreak, old.Version,
								"running instance is pinned to %s@%s, which the candidate %s without a resolved migration", old.Identity(), old.Version, dropOrChange(kept, next))
						}
					}
				}
				obj := ImpactedObject{Field: section.field, Object: old.Identity(), Version: old.Version}
				switch {
				case section.kind != "workflow_definition" && !resolved:
					add(section.field, old.Identity(), ImpactUnresolvedMigration, old.Version, "the candidate %s without a resolved migration", dropOrChange(kept, next))
				case resolved || section.kind == "workflow_definition":
					obj.State, obj.Detail = "CHANGED", dropOrChange(kept, next)
					changed = append(changed, obj)
				}
			}
		}
	}

	if check.Content != nil {
		if _, err := Bind(*check.Content); err != nil {
			add("content", cand.packID(), ImpactBindingRejected, version, "%v", err)
		}
	}
	if check.Experience != nil {
		if _, err := BindExperience(*check.Experience); err != nil {
			var r *ExperienceRejection
			if errors.As(err, &r) {
				add(r.Field, cand.packID(), r.State, r.Version, "%s", r.Detail)
			} else {
				add("experience", cand.packID(), ImpactBindingRejected, version, "%v", err)
			}
		}
	}
	if len(impacted) > 0 {
		return PublicationReport{}, block(impacted)
	}
	digest, _ := cand.Digest()
	return PublicationReport{PackID: cand.packID(), Version: cand.Version, Digest: digest, Changed: changed}, nil
}

func dropOrChange(kept bool, next string) string {
	if kept {
		return "re-versions it to " + next
	}
	return "drops it"
}

func block(impacted []ImpactedObject) error {
	sort.SliceStable(impacted, func(i, j int) bool {
		if impacted[i].Field != impacted[j].Field {
			return impacted[i].Field < impacted[j].Field
		}
		return impacted[i].Object < impacted[j].Object
	})
	first := impacted[0]
	return &PublicationBlock{Code: PublicationRejectionCode, Field: first.Field, State: first.State, Version: first.Version, Impacted: impacted}
}

// versionInRange compares dotted numeric versions; an absent bound is open.
// A version that is not dotted numeric never satisfies a bound.
func versionInRange(v, lo, hi string) bool {
	if lo != "" {
		c, ok := compareVersions(v, lo)
		if !ok || c < 0 {
			return false
		}
	}
	if hi != "" {
		c, ok := compareVersions(v, hi)
		if !ok || c > 0 {
			return false
		}
	}
	return true
}

func compareVersions(a, b string) (int, bool) {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

func versionParts(v string) ([]int, bool) {
	fields := strings.Split(strings.TrimSpace(v), ".")
	out := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// PublicationStore persists an accepted publication.
type PublicationStore interface {
	SavePublication(ctx context.Context, tenantID string, r PublicationReport) (ActivationEffects, error)
}

// PublishChecked checks and, only when accepted, persists the publication.
func PublishChecked(ctx context.Context, store PublicationStore, tenantID string, check PublicationCheck) (PublicationReport, ActivationEffects, error) {
	if store == nil || strings.TrimSpace(tenantID) == "" {
		return PublicationReport{}, ActivationEffects{}, fmt.Errorf("%w: store and tenant are required", ErrPublicationBlocked)
	}
	report, err := CheckPublication(check)
	if err != nil {
		return PublicationReport{}, ActivationEffects{}, err
	}
	effects, err := store.SavePublication(ctx, tenantID, report)
	if err != nil {
		return PublicationReport{}, ActivationEffects{}, err
	}
	return report, effects, nil
}
