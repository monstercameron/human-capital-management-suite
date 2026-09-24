// Package actiondiscovery resolves governed action descriptors for every
// presentation surface from one immutable catalog.
package actiondiscovery

import (
	"errors"
	"fmt"
	"strings"
)

type Scope string
type Effect string
type Risk string
type Reason string

const (
	Universal                Scope  = "universal"
	Contextual               Scope  = "contextual"
	EffectNonMaterial        Effect = "non_material"
	EffectRead               Effect = "read"
	EffectCreate             Effect = "create"
	EffectUpdate             Effect = "update"
	EffectDelete             Effect = "delete"
	EffectExecute            Effect = "execute"
	RiskLow                  Risk   = "low"
	RiskMedium               Risk   = "medium"
	RiskHigh                 Risk   = "high"
	RiskCritical             Risk   = "critical"
	ReasonUnauthorized       Reason = "unauthorized"
	ReasonUnsupportedSubject Reason = "unsupported_subject"
	ReasonMissingContext     Reason = "missing_context"
	ReasonMissingCapability  Reason = "missing_capability"
	ReasonUnpublishedIntent  Reason = "unpublished_intent"
	ReasonDisabled           Reason = "disabled"
	ReasonStaleVersion       Reason = "stale_version"
	ReasonNotAvailable       Reason = "not_available"
)

var ErrInvalidAction = errors.New("action discovery: invalid action definition")

type Route struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Version string `json:"version"`
}
type Versions struct {
	Feature    string `json:"feature"`
	Intent     string `json:"intent"`
	Capability string `json:"capability"`
}
type Input struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
}
type Definition struct {
	SemanticID            string   `json:"semanticId"`
	Label                 string   `json:"label"`
	Description           string   `json:"description,omitempty"`
	Scope                 Scope    `json:"scope"`
	FeatureID             string   `json:"featureId"`
	IntentID              string   `json:"intentId"`
	CapabilityID          string   `json:"capabilityId"`
	Versions              Versions `json:"versions"`
	Route                 Route    `json:"route"`
	PermittedSubjectTypes []string `json:"permittedSubjectTypes"`
	RequiredInputs        []Input  `json:"requiredInputs,omitempty"`
	Risk                  Risk     `json:"risk"`
	Effect                Effect   `json:"effect"`
	SimulationAvailable   bool     `json:"simulationAvailable"`
	Enabled               *bool    `json:"enabled,omitempty"`
	Published             *bool    `json:"published,omitempty"`
	CapabilityAvailable   *bool    `json:"capabilityAvailable,omitempty"`
}
type Context struct {
	SubjectType string `json:"subjectType,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
	ContextType string `json:"contextType,omitempty"`
	ContextID   string `json:"contextId,omitempty"`
}
type Authorization struct {
	Allowed     bool
	Reason      Reason
	Explanation string
}
type Authorizer func(Definition, Context) Authorization
type Request struct {
	Context   Context
	Scope     *Scope
	Authorize Authorizer
}
type Availability struct {
	Available   bool   `json:"available"`
	Reason      Reason `json:"reason,omitempty"`
	Explanation string `json:"explanation,omitempty"`
}
type Discovered struct {
	Definition
	Available              bool         `json:"available"`
	Availability           Availability `json:"availability"`
	UnavailableReason      Reason       `json:"unavailableReason,omitempty"`
	UnavailableExplanation string       `json:"unavailableExplanation,omitempty"`
}

type Registry struct{ actions []Definition }

func New(definitions []Definition) (*Registry, error) {
	seen := make(map[string]struct{}, len(definitions))
	copyDefs := make([]Definition, len(definitions))
	for i, action := range definitions {
		if err := validate(action); err != nil {
			return nil, err
		}
		if _, exists := seen[action.SemanticID]; exists {
			return nil, fmt.Errorf("%w: duplicate semantic action ID %q", ErrInvalidAction, action.SemanticID)
		}
		seen[action.SemanticID] = struct{}{}
		copyDefs[i] = cloneDefinition(action)
	}
	return &Registry{actions: copyDefs}, nil
}

func (r *Registry) List() []Definition {
	if r == nil {
		return nil
	}
	out := make([]Definition, len(r.actions))
	for i, action := range r.actions {
		out[i] = cloneDefinition(action)
	}
	return out
}
func (r *Registry) Get(id string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	for _, action := range r.actions {
		if action.SemanticID == id {
			return cloneDefinition(action), true
		}
	}
	return Definition{}, false
}
func (r *Registry) Discover(req Request) []Discovered {
	if r == nil {
		return nil
	}
	out := make([]Discovered, 0, len(r.actions))
	for _, action := range r.actions {
		if req.Scope != nil && action.Scope != *req.Scope {
			continue
		}
		out = append(out, resolve(cloneDefinition(action), req))
	}
	return out
}

func CreateSemanticID(featureID, intentID, capabilityID string) string {
	return stableID(featureID) + ":" + stableID(intentID) + ":" + stableID(capabilityID)
}
func stableID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	separator := false
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)
		if valid {
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			separator = false
			b.WriteRune(r)
		} else {
			separator = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func validate(a Definition) error {
	if a.SemanticID == "" || a.SemanticID != stableID(a.SemanticID) {
		return fmt.Errorf("%w: invalid semantic action ID %q", ErrInvalidAction, a.SemanticID)
	}
	for name, value := range map[string]string{"feature_id": a.FeatureID, "intent_id": a.IntentID, "capability_id": a.CapabilityID, "feature_version": a.Versions.Feature, "intent_version": a.Versions.Intent, "capability_version": a.Versions.Capability, "route_method": a.Route.Method, "route_path": a.Route.Path, "route_version": a.Route.Version} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is empty", ErrInvalidAction, name)
		}
	}
	if a.Scope != Universal && a.Scope != Contextual {
		return fmt.Errorf("%w: invalid scope %q", ErrInvalidAction, a.Scope)
	}
	if len(a.PermittedSubjectTypes) == 0 {
		return fmt.Errorf("%w: no permitted subject types", ErrInvalidAction)
	}
	if !validRoutePath(a.Route.Path) {
		return fmt.Errorf("%w: invalid route path %q", ErrInvalidAction, a.Route.Path)
	}
	return nil
}
func validRoutePath(path string) bool {
	if !strings.HasPrefix(path, "/") || path == "/" {
		return false
	}
	for _, r := range path {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._~!$&'()*+,;=:@/-{}", r)) {
			return false
		}
	}
	return true
}
func cloneDefinition(a Definition) Definition {
	a.PermittedSubjectTypes = append([]string(nil), a.PermittedSubjectTypes...)
	a.RequiredInputs = append([]Input(nil), a.RequiredInputs...)
	if a.Enabled != nil {
		v := *a.Enabled
		a.Enabled = &v
	}
	if a.Published != nil {
		v := *a.Published
		a.Published = &v
	}
	if a.CapabilityAvailable != nil {
		v := *a.CapabilityAvailable
		a.CapabilityAvailable = &v
	}
	return a
}
func resolve(a Definition, req Request) Discovered {
	var reason Reason
	if a.Enabled != nil && !*a.Enabled {
		reason = ReasonDisabled
	} else if a.Published != nil && !*a.Published {
		reason = ReasonUnpublishedIntent
	} else if a.CapabilityAvailable != nil && !*a.CapabilityAvailable {
		reason = ReasonMissingCapability
	} else if !contains(a.PermittedSubjectTypes, req.Context.SubjectType) {
		reason = ReasonUnsupportedSubject
	} else if a.Scope == Contextual && (req.Context.ContextType == "" || req.Context.ContextID == "") {
		reason = ReasonMissingContext
	}
	var explanation string
	if reason == "" && req.Authorize != nil {
		decision := req.Authorize(a, req.Context)
		if !decision.Allowed {
			reason = safeReason(decision.Reason)
			explanation = safeExplanation(reason)
		}
	}
	available := reason == ""
	out := Discovered{Definition: a, Available: available, Availability: Availability{Available: available}}
	if reason != "" {
		out.Availability.Reason = reason
		out.UnavailableReason = reason
		out.Availability.Explanation = explanation
		out.UnavailableExplanation = explanation
	}
	return out
}
func contains(items []string, item string) bool {
	for _, value := range items {
		if value == item {
			return true
		}
	}
	return false
}
func safeReason(r Reason) Reason {
	switch r {
	case ReasonUnsupportedSubject, ReasonMissingContext, ReasonMissingCapability, ReasonUnpublishedIntent, ReasonDisabled, ReasonStaleVersion, ReasonNotAvailable:
		return r
	default:
		return ReasonUnauthorized
	}
}
func safeExplanation(r Reason) string {
	switch r {
	case ReasonUnsupportedSubject:
		return "This action is not available for this subject."
	case ReasonMissingContext:
		return "This action requires additional context."
	case ReasonMissingCapability:
		return "This action is not currently enabled."
	case ReasonUnpublishedIntent:
		return "This action is not currently published."
	case ReasonDisabled:
		return "This action is currently disabled."
	case ReasonStaleVersion:
		return "This action requires a newer version."
	case ReasonNotAvailable:
		return "This action is not currently available."
	default:
		return "This action is not available for the current authorization."
	}
}
