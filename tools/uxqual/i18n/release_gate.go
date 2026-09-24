package i18n

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	expcatalog "github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
)

var (
	ErrInvalidReleaseManifest = errors.New("i18n release: invalid manifest")
	ErrReleaseBlocked         = errors.New("i18n release: admission blocked")
)

// ReleaseManifest is the versioned statement of which locales and journeys a
// product release promises. Requirements cover rendered UI states and any
// notification or document that carries the same user task.
type ReleaseManifest struct {
	Product           string
	SupportedLocales  []string
	Journeys          []JourneyRequirement
	OptionalFallbacks []FallbackAllowance
}

type JourneyRequirement struct {
	ID          string
	States      []string
	Messages    []ContentRequirement
	Derivatives []DerivativeRequirement
}

type ContentRequirement struct {
	Key, MeaningID string
	Kind           MessageKind
	Params         []Parameter
}

type DerivativeRequirement struct {
	Kind, Key, MeaningID string // kind must be "notification" or "document"
	Legal                bool
	Params               []Parameter
}

// FallbackAllowance grants a single optional key to use a named source locale.
// Acknowledged must be true so the accepted fallback is always reported.
type FallbackAllowance struct {
	Locale, Key, SourceLocale, Reason string
	Acknowledged                      bool
}

// ReleaseLocale contains the immutable active catalog and rendered evidence
// for one locale. Artifacts are keyed by semantic key and must match both the
// catalog translation and manifest contract.
type ReleaseLocale struct {
	Revision  expcatalog.CatalogRevision
	Direction string
	Artifacts []ReleaseArtifact
}

type ReleaseArtifact struct {
	Key, MeaningID, Text, Direction string
	NextAction                      string
	AccessibleName                  string
	Kind                            MessageKind
	Params                          []Parameter
	Legal                           bool
	Reviewer, LegalReviewStatus     string
	FallbackFrom                    string
	KeyboardReachable               bool
}

type ReleaseInput struct {
	Manifest ReleaseManifest
	Locales  map[string]ReleaseLocale
	AsOf     time.Time
}

type ReleaseDecision struct {
	Accepted    bool
	Failures    []string
	Diagnostics []string
}

// AdmitRelease checks all required journey states and their communication
// derivatives against active catalog revisions. It is deterministic and has
// no side effects, so publication code can require Accepted before release.
func AdmitRelease(input ReleaseInput) ReleaseDecision {
	decision := ReleaseDecision{}
	if err := input.Manifest.Validate(); err != nil {
		decision.Failures = []string{err.Error()}
		return decision
	}
	localeSet := map[string]bool{}
	for _, locale := range input.Manifest.SupportedLocales {
		localeSet[locale] = true
	}
	fallbacks := map[string]FallbackAllowance{}
	for _, allowed := range input.Manifest.OptionalFallbacks {
		fallbacks[allowed.Locale+"\x00"+allowed.Key] = allowed
	}

	for _, locale := range input.Manifest.SupportedLocales {
		active, ok := input.Locales[locale]
		if !ok {
			decision.Failures = append(decision.Failures, locale+": active catalog evidence is missing")
			continue
		}
		if active.Revision.Locale != locale || active.Revision.ID == "" || !active.Revision.VerifyDigest() || active.Revision.Validate() != nil || !active.Revision.Effective(input.AsOf) {
			decision.Failures = append(decision.Failures, locale+": active catalog revision identity or digest is invalid")
			continue
		}
		if active.Direction != expectedDirection(locale) {
			decision.Failures = append(decision.Failures, locale+": wrong text direction")
		}
		artifacts := make(map[string]ReleaseArtifact, len(active.Artifacts))
		for _, artifact := range active.Artifacts {
			if _, exists := artifacts[artifact.Key]; exists {
				decision.Failures = append(decision.Failures, locale+": duplicate rendered key "+artifact.Key)
			}
			artifacts[artifact.Key] = artifact
		}
		for _, journey := range input.Manifest.Journeys {
			for _, state := range journey.States {
				key := stateKey(journey.ID, state)
				decision.check(locale, active, artifacts, fallbacks, ContentRequirement{Key: key, MeaningID: key, Kind: kindForState(state)}, false)
			}
			for _, required := range journey.Messages {
				decision.check(locale, active, artifacts, fallbacks, required, false)
			}
			for _, required := range journey.Derivatives {
				decision.check(locale, active, artifacts, fallbacks, ContentRequirement{Key: required.Key, MeaningID: required.MeaningID, Kind: Success, Params: required.Params}, required.Legal)
			}
		}
	}
	// A declared fallback must actually be present in its target locale and
	// source locale. Extra or unacknowledged fallback markers are rejected.
	for _, locale := range input.Manifest.SupportedLocales {
		active, ok := input.Locales[locale]
		if !ok {
			continue
		}
		for _, artifact := range active.Artifacts {
			if artifact.FallbackFrom == "" {
				continue
			}
			allowance, ok := fallbacks[locale+"\x00"+artifact.Key]
			if !ok || !allowance.Acknowledged || allowance.SourceLocale != artifact.FallbackFrom || !localeSet[artifact.FallbackFrom] {
				decision.Failures = append(decision.Failures, locale+": undeclared or unacknowledged fallback for "+artifact.Key)
				continue
			}
			if allowance.Reason == "" {
				decision.Failures = append(decision.Failures, locale+": fallback reason missing for "+artifact.Key)
				continue
			}
			declaredByCatalog := false
			for _, source := range active.Revision.Fallbacks[locale] {
				if source == allowance.SourceLocale {
					declaredByCatalog = true
				}
			}
			source, sourceOK := input.Locales[allowance.SourceLocale]
			sourceArtifact, sourceArtifactOK := findArtifact(source.Artifacts, artifact.Key)
			sourceTranslation, translationOK := source.Revision.EffectiveTranslation(artifact.Key, source.Revision.EffectiveFrom)
			if !declaredByCatalog || !sourceOK || !sourceArtifactOK || !translationOK || sourceArtifact.Text != sourceTranslation.Text || artifact.Text != sourceArtifact.Text || artifact.MeaningID != sourceArtifact.MeaningID || sourceArtifact.MeaningID != sourceTranslation.MeaningID {
				decision.Failures = append(decision.Failures, locale+": fallback source evidence mismatch for "+artifact.Key)
				continue
			}
			decision.Diagnostics = append(decision.Diagnostics, fmt.Sprintf("%s: optional key %s uses acknowledged %s fallback (%s)", locale, artifact.Key, allowance.SourceLocale, allowance.Reason))
		}
	}
	sort.Strings(decision.Failures)
	sort.Strings(decision.Diagnostics)
	decision.Accepted = len(decision.Failures) == 0
	return decision
}

func (d *ReleaseDecision) check(locale string, active ReleaseLocale, artifacts map[string]ReleaseArtifact, fallbacks map[string]FallbackAllowance, required ContentRequirement, legal bool) {
	artifact, ok := artifacts[required.Key]
	if !ok {
		d.Failures = append(d.Failures, locale+": missing required key "+required.Key)
		return
	}
	if artifact.FallbackFrom != "" {
		// Fallbacks can only cover optional artifacts. Required journey and
		// derivative keys always need a translation in the declared locale.
		if legal || required.Kind != Helper {
			d.Failures = append(d.Failures, locale+": required content falls back for "+required.Key)
			return
		}
		if _, allowed := fallbacks[locale+"\x00"+required.Key]; !allowed {
			d.Failures = append(d.Failures, locale+": fallback is outside the manifest for "+required.Key)
			return
		}
	}
	translation, translated := active.Revision.EffectiveTranslation(required.Key, active.Revision.EffectiveFrom)
	if artifact.FallbackFrom != "" {
		return
	}
	if !translated || translation.Text != artifact.Text || translation.MeaningID != required.MeaningID || artifact.MeaningID != required.MeaningID {
		d.Failures = append(d.Failures, locale+": catalog/render meaning mismatch for "+required.Key)
		return
	}
	if artifact.Direction != expectedDirection(locale) || artifact.Direction != active.Direction {
		d.Failures = append(d.Failures, locale+": rendered direction mismatch for "+required.Key)
	}
	if strings.TrimSpace(artifact.AccessibleName) == "" {
		d.Failures = append(d.Failures, locale+": missing accessible name for "+required.Key)
	}
	if !sameParams(artifact.Params, required.Params) {
		d.Failures = append(d.Failures, locale+": parameter meaning changed for "+required.Key)
	}
	msg := Message{Key: required.Key, MeaningID: required.MeaningID, Text: artifact.Text, Kind: required.Kind, Params: artifact.Params}
	msg.NextAction = artifact.NextAction
	if err := msg.Validate(); err != nil {
		d.Failures = append(d.Failures, locale+": invalid rendered content for "+required.Key+": "+err.Error())
	}
	if legal || translation.Legal || strings.EqualFold(translation.Classification, "LEGAL") || strings.EqualFold(translation.Classification, "LEGAL_TEXT") {
		if !(artifact.Legal || translation.Legal) || artifact.Reviewer == "" || !strings.EqualFold(artifact.LegalReviewStatus, "APPROVED") || translation.Reviewer == "" || !strings.EqualFold(translation.LegalReviewStatus, "APPROVED") {
			d.Failures = append(d.Failures, locale+": unreviewed legal text "+required.Key)
		}
	}
}

func (m ReleaseManifest) Validate() error {
	if strings.TrimSpace(m.Product) == "" || len(m.SupportedLocales) == 0 || len(m.Journeys) == 0 {
		return fmt.Errorf("%w: product, locales, and journeys are required", ErrInvalidReleaseManifest)
	}
	seenLocales := map[string]bool{}
	for _, locale := range m.SupportedLocales {
		if strings.TrimSpace(locale) == "" || seenLocales[locale] {
			return fmt.Errorf("%w: empty or duplicate locale", ErrInvalidReleaseManifest)
		}
		seenLocales[locale] = true
	}
	seenJourney := map[string]bool{}
	seenContent := map[string]bool{}
	for _, journey := range m.Journeys {
		if journey.ID == "" || seenJourney[journey.ID] {
			return fmt.Errorf("%w: empty or duplicate journey", ErrInvalidReleaseManifest)
		}
		seenJourney[journey.ID] = true
		if len(journey.States) != 7 {
			return fmt.Errorf("%w: journey %s must declare seven required states", ErrInvalidReleaseManifest, journey.ID)
		}
		states := map[string]bool{}
		for _, state := range journey.States {
			if !requiredStates[state] || states[state] {
				return fmt.Errorf("%w: invalid or duplicate state %s", ErrInvalidReleaseManifest, state)
			}
			states[state] = true
		}
		for _, state := range requiredJourneyStates {
			if !states[state] {
				return fmt.Errorf("%w: journey %s omits state %s", ErrInvalidReleaseManifest, journey.ID, state)
			}
		}
		if len(journey.Messages) == 0 && len(journey.Derivatives) == 0 {
			return fmt.Errorf("%w: journey %s has no rendered content requirements", ErrInvalidReleaseManifest, journey.ID)
		}
		for _, content := range journey.Messages {
			if err := validateContentRequirement(content); err != nil {
				return err
			}
			if seenContent[content.Key] {
				return fmt.Errorf("%w: duplicate required key %s", ErrInvalidReleaseManifest, content.Key)
			}
			seenContent[content.Key] = true
		}
		for _, content := range journey.Derivatives {
			if content.Kind != "notification" && content.Kind != "document" {
				return fmt.Errorf("%w: unsupported derivative kind %s", ErrInvalidReleaseManifest, content.Kind)
			}
			if err := validateContentRequirement(ContentRequirement{Key: content.Key, MeaningID: content.MeaningID, Kind: Status, Params: content.Params}); err != nil {
				return err
			}
			if seenContent[content.Key] {
				return fmt.Errorf("%w: duplicate required key %s", ErrInvalidReleaseManifest, content.Key)
			}
			seenContent[content.Key] = true
		}
		for _, state := range journey.States {
			key := stateKey(journey.ID, state)
			if seenContent[key] {
				return fmt.Errorf("%w: duplicate required key %s", ErrInvalidReleaseManifest, key)
			}
			seenContent[key] = true
		}
	}
	for _, f := range m.OptionalFallbacks {
		if !seenLocales[f.Locale] || !seenLocales[f.SourceLocale] || f.Key == "" || f.Reason == "" || !f.Acknowledged {
			return fmt.Errorf("%w: invalid optional fallback", ErrInvalidReleaseManifest)
		}
		for _, journey := range m.Journeys {
			for _, item := range journey.Messages {
				if item.Key == f.Key {
					return fmt.Errorf("%w: required journey content cannot be optional fallback", ErrInvalidReleaseManifest)
				}
			}
			for _, item := range journey.Derivatives {
				if item.Key == f.Key {
					return fmt.Errorf("%w: required derivative cannot be optional fallback", ErrInvalidReleaseManifest)
				}
			}
			for _, state := range journey.States {
				if stateKey(journey.ID, state) == f.Key {
					return fmt.Errorf("%w: required state cannot be optional fallback", ErrInvalidReleaseManifest)
				}
			}
		}
	}
	return nil
}

func validateContentRequirement(c ContentRequirement) error {
	if c.Key == "" || c.MeaningID == "" || c.Kind == "" {
		return fmt.Errorf("%w: incomplete content requirement", ErrInvalidReleaseManifest)
	}
	return nil
}

var requiredJourneyStates = []string{"ready", "empty", "loading", "validation", "refusal", "success", "recovery"}
var requiredStates = map[string]bool{"ready": true, "empty": true, "loading": true, "validation": true, "refusal": true, "success": true, "recovery": true}

func stateKey(journey, state string) string { return journey + "." + state }
func kindForState(state string) MessageKind {
	switch state {
	case "ready":
		return Heading
	case "empty":
		return Empty
	case "loading":
		return Status
	case "validation":
		return Error
	case "refusal":
		return Refusal
	case "success":
		return Success
	default:
		return Recovery
	}
}
func expectedDirection(locale string) string {
	primary := strings.ToLower(strings.Split(strings.ReplaceAll(locale, "_", "-"), "-")[0])
	switch primary {
	case "ar", "he", "fa", "ur", "ps", "sd", "ug", "yi":
		return "rtl"
	default:
		return "ltr"
	}
}

func findArtifact(artifacts []ReleaseArtifact, key string) (ReleaseArtifact, bool) {
	for _, artifact := range artifacts {
		if artifact.Key == key {
			return artifact, true
		}
	}
	return ReleaseArtifact{}, false
}
