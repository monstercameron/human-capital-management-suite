// Package i18npresentation binds reviewed translations to a workflow's
// presentation identity without adding presentation text to its executable
// definition or plan.
package i18npresentation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrInvalidPresentation = errors.New("workflow presentation: invalid presentation")
	ErrMissingTranslation  = errors.New("workflow presentation: missing required translation")
	ErrParameterMismatch   = errors.New("workflow presentation: parameter shape mismatch")
	ErrUntrustedContent    = errors.New("workflow presentation: translation changed executable authority")
)

// ParameterType is the closed set of values presentation copy may interpolate.
type ParameterType string

const (
	ParameterText   ParameterType = "TEXT"
	ParameterNumber ParameterType = "NUMBER"
	ParameterDate   ParameterType = "DATE"
)

// Parameter declares one named value that may appear in a localized message.
type Parameter struct {
	Name string        `json:"name"`
	Type ParameterType `json:"type"`
}

// Term binds one stable semantic key to a workflow presentation surface.
// MeaningID must equal Key across every locale; translators cannot redefine it.
type Term struct {
	Key           string      `json:"key"`
	MeaningID     string      `json:"meaning_id"`
	Role          string      `json:"role"`
	NodeID        string      `json:"node_id,omitempty"`
	Accessible    bool        `json:"accessible"`
	AllowFallback bool        `json:"allow_fallback,omitempty"`
	Parameters    []Parameter `json:"parameters,omitempty"`
}

// Version pins presentation metadata independently from the executable plan.
type Version struct {
	ID                   string              `json:"id"`
	WorkflowID           string              `json:"workflow_id"`
	WorkflowVersion      uint32              `json:"workflow_version"`
	ExecutablePlanDigest string              `json:"executable_plan_digest"`
	RequiredLocales      []string            `json:"required_locales"`
	CatalogRevisionIDs   map[string]string   `json:"catalog_revision_ids"`
	Fallbacks            map[string][]string `json:"fallbacks,omitempty"`
	Terms                []Term              `json:"terms"`
	CanonicalDigest      string              `json:"canonical_digest"`
}

type digestView struct {
	ID                   string              `json:"id"`
	WorkflowID           string              `json:"workflow_id"`
	WorkflowVersion      uint32              `json:"workflow_version"`
	ExecutablePlanDigest string              `json:"executable_plan_digest"`
	RequiredLocales      []string            `json:"required_locales"`
	CatalogRevisionIDs   map[string]string   `json:"catalog_revision_ids"`
	Fallbacks            map[string][]string `json:"fallbacks,omitempty"`
	Terms                []Term              `json:"terms"`
}

// Digest pins the presentation revision independently from the executable plan.
func (v Version) Digest() string {
	b, _ := json.Marshal(digestView{v.ID, v.WorkflowID, v.WorkflowVersion, v.ExecutablePlanDigest, v.RequiredLocales, v.CatalogRevisionIDs, v.Fallbacks, v.Terms})
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
var placeholderPattern = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// Validate checks completeness, revision integrity, fallback policy, and the
// binding to a specific executable plan. Revisions are detached immutable
// values loaded by the publication boundary using CatalogRevisionIDs.
func Validate(def workflow.Definition, version Version, revisions map[string]i18n.CatalogRevision) error {
	if version.ID == "" || def.WorkflowID == "" || version.WorkflowID != def.WorkflowID ||
		version.WorkflowVersion == 0 || version.WorkflowVersion != def.Version ||
		version.ExecutablePlanDigest == "" || len(version.RequiredLocales) == 0 || len(version.Terms) == 0 ||
		version.CanonicalDigest == "" || version.CanonicalDigest != version.Digest() {
		return ErrInvalidPresentation
	}
	locales, err := i18n.NewLocaleContext(version.RequiredLocales[0], version.RequiredLocales, version.Fallbacks)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPresentation, err)
	}
	terms := make(map[string]Term, len(version.Terms))
	for _, term := range version.Terms {
		if !keyPattern.MatchString(term.Key) || term.MeaningID != term.Key || term.Role == "" || term.Key != strings.TrimSpace(term.Key) {
			return fmt.Errorf("%w: term %q", ErrInvalidPresentation, term.Key)
		}
		if term.NodeID != "" && !definitionHasNode(def, term.NodeID) {
			return fmt.Errorf("%w: unknown node %q", ErrInvalidPresentation, term.NodeID)
		}
		if _, exists := terms[term.Key]; exists {
			return fmt.Errorf("%w: duplicate key %q", ErrInvalidPresentation, term.Key)
		}
		if err := validateParameters(term.Parameters); err != nil {
			return err
		}
		terms[term.Key] = term
	}
	if err := validateRequiredSurfaces(def, terms); err != nil {
		return err
	}
	for _, locale := range locales.Supported {
		id := version.CatalogRevisionIDs[locale]
		revision, ok := revisions[id]
		if id == "" || !ok || revision.ID != id || revision.Locale != locale || !revision.VerifyDigest() {
			return fmt.Errorf("%w: invalid catalog pin for %s", ErrInvalidPresentation, locale)
		}
		if err := revision.Validate(); err != nil {
			return fmt.Errorf("%w: catalog %s: %v", ErrInvalidPresentation, locale, err)
		}
		fallbackPath, _ := locales.FallbackPath(locale)
		for key, term := range terms {
			translation, found := revision.EffectiveTranslation(key, time.Now())
			if !found {
				if !term.AllowFallback || !hasPinnedFallback(version, revisions, fallbackPath, key, term, time.Now()) {
					return fmt.Errorf("%w: %s in %s", ErrMissingTranslation, key, locale)
				}
				continue
			}
			meaning := translation.MeaningID
			if meaning == "" {
				meaning = translation.Key
			}
			if meaning != term.MeaningID {
				return fmt.Errorf("%w: meaning changed for %s", ErrUntrustedContent, key)
			}
			if err := validatePlaceholderShape(translation.Text, term.Parameters); err != nil {
				return fmt.Errorf("%w: %s in %s: %v", ErrParameterMismatch, key, locale, err)
			}
		}
	}
	return nil
}

func hasPinnedFallback(version Version, revisions map[string]i18n.CatalogRevision, path []string, key string, term Term, at time.Time) bool {
	for _, locale := range path[1:] {
		id := version.CatalogRevisionIDs[locale]
		revision, ok := revisions[id]
		if !ok || revision.ID != id || revision.Locale != locale || !revision.VerifyDigest() {
			continue
		}
		translation, ok := revision.EffectiveTranslation(key, at)
		if !ok {
			continue
		}
		meaning := translation.MeaningID
		if meaning == "" {
			meaning = translation.Key
		}
		return meaning == term.MeaningID && validatePlaceholderShape(translation.Text, term.Parameters) == nil
	}
	return false
}

// Render returns a localized message from the pinned revision. Only declared,
// correctly typed values can be interpolated. Fallbacks come from the pinned
// presentation policy and never select an unpinned catalog revision.
func Render(version Version, revisions map[string]i18n.CatalogRevision, key, locale string, values map[string]any, at time.Time) (string, string, error) {
	if version.CanonicalDigest == "" || version.CanonicalDigest != version.Digest() {
		return "", "", ErrInvalidPresentation
	}
	term, ok := findTerm(version.Terms, key)
	if !ok {
		return "", "", ErrMissingTranslation
	}
	context, err := i18n.NewLocaleContext(locale, version.RequiredLocales, version.Fallbacks)
	if err != nil {
		return "", "", err
	}
	if err := validateValues(term.Parameters, values); err != nil {
		return "", "", err
	}
	path, err := context.FallbackPath(locale)
	if err != nil {
		return "", "", err
	}
	for _, candidate := range path {
		id := version.CatalogRevisionIDs[candidate]
		revision, exists := revisions[id]
		if !exists || revision.ID != id || revision.Locale != candidate || !revision.VerifyDigest() {
			continue
		}
		translation, found := revision.EffectiveTranslation(key, at)
		if !found {
			continue
		}
		meaning := translation.MeaningID
		if meaning == "" {
			meaning = translation.Key
		}
		if meaning != term.MeaningID || validatePlaceholderShape(translation.Text, term.Parameters) != nil {
			return "", "", ErrUntrustedContent
		}
		text := placeholderPattern.ReplaceAllStringFunc(translation.Text, func(token string) string {
			name := placeholderPattern.FindStringSubmatch(token)[1]
			return fmt.Sprint(values[name])
		})
		return text, candidate, nil
	}
	return "", "", ErrMissingTranslation
}

func validateRequiredSurfaces(def workflow.Definition, terms map[string]Term) error {
	roles := map[string]bool{}
	for _, term := range terms {
		roles[term.Role] = true
	}
	for _, role := range []string{"WORKFLOW_NAME", "ACCESSIBLE_NAME", "REFUSAL", "NEXT_ACTION", "RECOVERY"} {
		if !roles[role] {
			return fmt.Errorf("%w: missing surface role %s", ErrInvalidPresentation, role)
		}
	}
	for _, node := range def.Nodes {
		if node.Type == workflow.StepTask && !nodeHasRole(terms, node.ID, "INSTRUCTION") {
			return fmt.Errorf("%w: task %s lacks instruction", ErrInvalidPresentation, node.ID)
		}
		if node.Type == workflow.StepDecision && !nodeHasRole(terms, node.ID, "DECISION") {
			return fmt.Errorf("%w: decision %s lacks localized choices", ErrInvalidPresentation, node.ID)
		}
	}
	if !hasAccessibleName(terms) {
		return fmt.Errorf("%w: accessible name is not marked accessible", ErrInvalidPresentation)
	}
	return nil
}

func hasAccessibleName(terms map[string]Term) bool {
	for _, term := range terms {
		if term.Role == "ACCESSIBLE_NAME" && term.Accessible {
			return true
		}
	}
	return false
}
func nodeHasRole(terms map[string]Term, nodeID, role string) bool {
	for _, term := range terms {
		if term.NodeID == nodeID && term.Role == role {
			return true
		}
	}
	return false
}
func definitionHasNode(def workflow.Definition, nodeID string) bool {
	for _, node := range def.Nodes {
		if node.ID == nodeID {
			return true
		}
	}
	return false
}
func findTerm(terms []Term, key string) (Term, bool) {
	for _, term := range terms {
		if term.Key == key {
			return term, true
		}
	}
	return Term{}, false
}
func validateParameters(parameters []Parameter) error {
	seen := map[string]bool{}
	for _, parameter := range parameters {
		if parameter.Name == "" || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(parameter.Name) || seen[parameter.Name] {
			return fmt.Errorf("%w: invalid parameter", ErrInvalidPresentation)
		}
		if parameter.Type != ParameterText && parameter.Type != ParameterNumber && parameter.Type != ParameterDate {
			return fmt.Errorf("%w: unsupported parameter type %q", ErrInvalidPresentation, parameter.Type)
		}
		seen[parameter.Name] = true
	}
	return nil
}
func validatePlaceholderShape(text string, parameters []Parameter) error {
	declared := map[string]bool{}
	for _, parameter := range parameters {
		declared[parameter.Name] = true
	}
	found := map[string]bool{}
	for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		found[match[1]] = true
	}
	if len(found) != len(declared) {
		return ErrParameterMismatch
	}
	for name := range found {
		if !declared[name] {
			return ErrParameterMismatch
		}
	}
	return nil
}
func validateValues(parameters []Parameter, values map[string]any) error {
	if len(parameters) != len(values) {
		return ErrParameterMismatch
	}
	for _, parameter := range parameters {
		value, ok := values[parameter.Name]
		if !ok {
			return ErrParameterMismatch
		}
		switch parameter.Type {
		case ParameterText:
			if _, ok := value.(string); !ok {
				return ErrParameterMismatch
			}
		case ParameterNumber:
			switch value.(type) {
			case int, int32, int64, uint, uint32, uint64, float32, float64:
			default:
				return ErrParameterMismatch
			}
		case ParameterDate:
			if _, ok := value.(time.Time); !ok {
				return ErrParameterMismatch
			}
		}
	}
	return nil
}

// ParameterShape returns a stable, sorted representation for golden checks.
func ParameterShape(term Term) []Parameter {
	parameters := append([]Parameter(nil), term.Parameters...)
	sort.Slice(parameters, func(i, j int) bool { return parameters[i].Name < parameters[j].Name })
	return parameters
}
