package i18npresentation

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_I18N_006(t *testing.T) {
	definition, version, revisions := fixture(t)
	if err := Validate(definition, version, revisions); err != nil {
		t.Fatal(err)
	}
	got, locale, err := Render(version, revisions, "manager_change.task.instruction", "de-DE", map[string]any{"worker": "Ada"}, time.Now())
	if err != nil || locale != "de-DE" || got != "Für Ada prüfen" {
		t.Fatalf("render = %q, %q, %v", got, locale, err)
	}
}

func TestTodo_I18N_006_Integration(t *testing.T) {
	definition, version, revisions := fixture(t)
	version.Fallbacks = map[string][]string{"ar": {"de-DE"}}
	key := "manager_change.workflow.recovery"
	version.Terms[termIndex(version.Terms, key)].AllowFallback = true
	version.CanonicalDigest = version.Digest()
	arabicID := version.CatalogRevisionIDs["ar"]
	arabic := revisions[arabicID]
	filtered := make([]i18n.Translation, 0, len(arabic.Translations)-1)
	for _, translation := range arabic.Translations {
		if translation.Key != key {
			filtered = append(filtered, translation)
		}
	}
	arabic.Translations = filtered
	arabic.CanonicalDigest = i18n.DigestRevision(arabic)
	arabic.Digest = arabic.CanonicalDigest
	revisions[arabicID] = arabic
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		key := "manager_change.workflow.name"
		if locale == "ar" {
			key = "manager_change.workflow.recovery"
		}
		text, used, err := Render(version, revisions, key, locale, nil, time.Now())
		wantLocale := locale
		if locale == "ar" {
			wantLocale = "de-DE"
		}
		if err != nil || used != wantLocale || text == "" {
			t.Fatalf("locale %s rendered %q from %s: %v", locale, text, used, err)
		}
	}
	if err := Validate(definition, version, revisions); err != nil {
		t.Fatalf("published workflow presentation rejected: %v", err)
	}
}

func TestTodo_I18N_006_Security(t *testing.T) {
	definition, version, revisions := fixture(t)
	before, _ := json.Marshal(definition)
	term := findFixtureTerm(&version, "manager_change.task.instruction")
	term.MeaningID = "manager_change.route.approve"
	if err := Validate(definition, version, revisions); !errors.Is(err, ErrInvalidPresentation) {
		t.Fatalf("meaning mutation error = %v", err)
	}
	_, version, revisions = fixture(t)
	_, _, err := Render(version, revisions, "manager_change.task.instruction", "en-US", map[string]any{"worker": 5}, time.Now())
	if !errors.Is(err, ErrParameterMismatch) {
		t.Fatalf("wrong parameter type error = %v", err)
	}
	after, _ := json.Marshal(definition)
	if string(before) != string(after) {
		t.Fatal("presentation validation mutated the executable definition")
	}
}

func TestTodo_I18N_006_Golden(t *testing.T) {
	_, version, revisions := fixture(t)
	text, locale, err := Render(version, revisions, "manager_change.task.instruction", "de-DE", map[string]any{"worker": "Ada"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	shape, ok := findTerm(version.Terms, "manager_change.task.instruction")
	if !ok {
		t.Fatal("instruction term missing")
	}
	got, _ := json.Marshal(struct {
		Locale         string      `json:"locale"`
		Text           string      `json:"text"`
		PlanDigest     string      `json:"plan_digest"`
		PresentationID string      `json:"presentation_id"`
		Parameters     []Parameter `json:"parameters"`
	}{locale, text, version.ExecutablePlanDigest, version.ID, ParameterShape(shape)})
	want := `{"locale":"de-DE","text":"Für Ada prüfen","plan_digest":"sha256:plan-a","presentation_id":"manager-change-presentation-3","parameters":[{"name":"worker","type":"TEXT"}]}`
	if string(got) != want {
		t.Fatalf("golden = %s", got)
	}
}

func fixture(t *testing.T) (workflow.Definition, Version, map[string]i18n.CatalogRevision) {
	t.Helper()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	definition := workflow.Definition{WorkflowID: "manager-change", Version: 3, Name: "Manager Change", Nodes: []workflow.Node{
		{ID: "review", Type: workflow.StepTask}, {ID: "decision", Type: workflow.StepDecision},
	}}
	terms := []Term{
		{Key: "manager_change.workflow.name", MeaningID: "manager_change.workflow.name", Role: "WORKFLOW_NAME"},
		{Key: "manager_change.workflow.accessible_name", MeaningID: "manager_change.workflow.accessible_name", Role: "ACCESSIBLE_NAME", Accessible: true},
		{Key: "manager_change.workflow.refusal", MeaningID: "manager_change.workflow.refusal", Role: "REFUSAL"},
		{Key: "manager_change.workflow.next_action", MeaningID: "manager_change.workflow.next_action", Role: "NEXT_ACTION"},
		{Key: "manager_change.workflow.recovery", MeaningID: "manager_change.workflow.recovery", Role: "RECOVERY"},
		{Key: "manager_change.task.instruction", MeaningID: "manager_change.task.instruction", Role: "INSTRUCTION", NodeID: "review", Parameters: []Parameter{{Name: "worker", Type: ParameterText}}},
		{Key: "manager_change.decision.choice", MeaningID: "manager_change.decision.choice", Role: "DECISION", NodeID: "decision"},
	}
	version := Version{ID: "manager-change-presentation-3", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version,
		ExecutablePlanDigest: "sha256:plan-a", RequiredLocales: []string{"en-US", "de-DE", "ar"},
		CatalogRevisionIDs: map[string]string{"en-US": "cat-en-3", "de-DE": "cat-de-3", "ar": "cat-ar-3"}, Terms: terms}
	version.CanonicalDigest = version.Digest()
	texts := map[string]map[string]string{
		"en-US": {"manager_change.workflow.name": "Manager change", "manager_change.workflow.accessible_name": "Manager change workflow", "manager_change.workflow.refusal": "Request refused", "manager_change.workflow.next_action": "Contact HR", "manager_change.workflow.recovery": "Try again later", "manager_change.task.instruction": "Review {worker}", "manager_change.decision.choice": "Approve"},
		"de-DE": {"manager_change.workflow.name": "Managerwechsel", "manager_change.workflow.accessible_name": "Workflow Managerwechsel", "manager_change.workflow.refusal": "Anfrage abgelehnt", "manager_change.workflow.next_action": "HR kontaktieren", "manager_change.workflow.recovery": "Später erneut versuchen", "manager_change.task.instruction": "Für {worker} prüfen", "manager_change.decision.choice": "Genehmigen"},
		"ar":    {"manager_change.workflow.name": "تغيير المدير", "manager_change.workflow.accessible_name": "مسار تغيير المدير", "manager_change.workflow.refusal": "تم رفض الطلب", "manager_change.workflow.next_action": "تواصل مع الموارد البشرية", "manager_change.workflow.recovery": "حاول مرة أخرى لاحقًا", "manager_change.task.instruction": "راجع {worker}", "manager_change.decision.choice": "موافقة"},
	}
	revisions := map[string]i18n.CatalogRevision{}
	for locale, id := range version.CatalogRevisionIDs {
		translations := make([]i18n.Translation, 0, len(terms))
		for _, term := range terms {
			translations = append(translations, i18n.Translation{Key: term.Key, Text: texts[locale][term.Key], MeaningID: term.MeaningID, Source: "customer-authored", Classification: "GENERAL", EffectiveFrom: at})
		}
		revision := i18n.CatalogRevision{ID: id, Locale: locale, Version: "3", CreatedAt: at, EffectiveFrom: at, Translations: translations}
		revision.CanonicalDigest = i18n.DigestRevision(revision)
		revision.Digest = revision.CanonicalDigest
		revisions[id] = revision
	}
	return definition, version, revisions
}

func findFixtureTerm(version *Version, key string) *Term {
	for i := range version.Terms {
		if version.Terms[i].Key == key {
			return &version.Terms[i]
		}
	}
	return nil
}

func termIndex(terms []Term, key string) int {
	for i := range terms {
		if terms[i].Key == key {
			return i
		}
	}
	return -1
}
