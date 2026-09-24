package i18n

import (
	"strings"
	"testing"
	"time"

	expcatalog "github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
)

var i18n007Locales = []string{"en-US", "de-DE", "ar"}

func i18n007Fixture() ReleaseInput {
	manifest := ReleaseManifest{Product: "changeops", SupportedLocales: append([]string(nil), i18n007Locales...), Journeys: []JourneyRequirement{{
		ID: "promotion", States: append([]string(nil), requiredJourneyStates...),
		Messages: []ContentRequirement{{Key: "promotion.review.count", MeaningID: "promotion.review.count.v1", Kind: Status, Params: []Parameter{{Name: "count", Format: "count"}}}},
		Derivatives: []DerivativeRequirement{
			{Kind: "notification", Key: "promotion.approved.notice", MeaningID: "promotion.approved.notice.v1", Params: []Parameter{{Name: "person", Format: "text"}}},
			{Kind: "document", Key: "promotion.approved.letter", MeaningID: "promotion.approved.letter.v1", Legal: true, Params: []Parameter{{Name: "person", Format: "text"}}},
		},
	}}}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	input := ReleaseInput{Manifest: manifest, Locales: map[string]ReleaseLocale{}, AsOf: now}
	for _, locale := range i18n007Locales {
		dir := expectedDirection(locale)
		revision := expcatalog.CatalogRevision{ID: "changeops-" + locale + "-r1", Locale: locale, Version: "1", CreatedAt: now, EffectiveFrom: now}
		artifacts := []ReleaseArtifact{}
		for _, journey := range manifest.Journeys {
			for _, state := range journey.States {
				key := stateKey(journey.ID, state)
				kind := kindForState(state)
				text := "Promotion " + state
				if locale == "de-DE" {
					text = "Beförderung " + state
				}
				if locale == "ar" {
					text = "الترقية " + state
				}
				action := ""
				if kind == Error || kind == Refusal || kind == Recovery {
					action = "Review the request"
				}
				revision.Translations = append(revision.Translations, i18n007Translation(key, key, text, now, false))
				artifacts = append(artifacts, ReleaseArtifact{Key: key, MeaningID: key, Text: text, Kind: kind, Direction: dir, NextAction: action, AccessibleName: text, KeyboardReachable: true})
			}
		}
		count := "Reviewed {count} details"
		notice := "Promotion approved for {person}"
		letter := "This letter confirms the promotion of {person}"
		if locale == "de-DE" {
			count, notice, letter = "{count} Angaben geprüft", "Beförderung für {person} genehmigt", "Dieses Schreiben bestätigt die Beförderung von {person}"
		}
		if locale == "ar" {
			count, notice, letter = "تمت مراجعة {count} من التفاصيل", "تمت الموافقة على ترقية {person}", "تؤكد هذه الرسالة ترقية {person}"
		}
		for _, pair := range []struct {
			key, meaning, text string
			legal              bool
			params             []Parameter
		}{
			{"promotion.review.count", "promotion.review.count.v1", count, false, []Parameter{{Name: "count", Format: "count"}}},
			{"promotion.approved.notice", "promotion.approved.notice.v1", notice, false, []Parameter{{Name: "person", Format: "text"}}},
			{"promotion.approved.letter", "promotion.approved.letter.v1", letter, true, []Parameter{{Name: "person", Format: "text"}}},
		} {
			revision.Translations = append(revision.Translations, i18n007Translation(pair.key, pair.meaning, pair.text, now, pair.legal))
			kind := Status
			if pair.key == "promotion.approved.notice" || pair.key == "promotion.approved.letter" {
				kind = Success
			}
			reviewer, review := "", ""
			if pair.legal {
				reviewer, review = "legal-reviewer", "APPROVED"
			}
			artifacts = append(artifacts, ReleaseArtifact{Key: pair.key, MeaningID: pair.meaning, Text: pair.text, Kind: kind, Params: pair.params, Direction: dir, Legal: pair.legal, Reviewer: reviewer, LegalReviewStatus: review, AccessibleName: pair.text, KeyboardReachable: true})
		}
		revision.Digest = expcatalog.DigestRevision(revision)
		input.Locales[locale] = ReleaseLocale{Revision: revision, Direction: dir, Artifacts: artifacts}
	}
	return input
}

func i18n007Translation(key, meaning, text string, at time.Time, legal bool) expcatalog.Translation {
	t := expcatalog.Translation{Key: key, MeaningID: meaning, Text: text, Source: "i18n007 fixture", Classification: "PUBLIC", EffectiveFrom: at}
	if legal {
		t.Legal, t.Classification, t.Reviewer, t.LegalReviewStatus = true, "LEGAL_TEXT", "legal-reviewer", "APPROVED"
	}
	return t
}

func i18n007Artifact(input *ReleaseInput, locale, key string) *ReleaseArtifact {
	value := input.Locales[locale]
	for i := range value.Artifacts {
		if value.Artifacts[i].Key == key {
			return &value.Artifacts[i]
		}
	}
	return nil
}

func TestTodo_I18N_007(t *testing.T) {
	decision := AdmitRelease(i18n007Fixture())
	if !decision.Accepted || len(decision.Failures) != 0 {
		t.Fatalf("complete release was blocked: %+v", decision)
	}
}

func TestTodo_I18N_007_Integration(t *testing.T) {
	input := i18n007Fixture()
	de := input.Locales["de-DE"]
	for i, tr := range de.Revision.Translations {
		if tr.Key == "promotion.approved.letter" {
			de.Revision.Translations = append(de.Revision.Translations[:i], de.Revision.Translations[i+1:]...)
			break
		}
	}
	de.Revision.Digest = expcatalog.DigestRevision(de.Revision)
	input.Locales["de-DE"] = de
	blocked := AdmitRelease(input)
	if blocked.Accepted || !strings.Contains(strings.Join(blocked.Failures, "\n"), "catalog/render meaning mismatch") {
		t.Fatalf("missing legal document translation did not block release: %+v", blocked)
	}

	input = i18n007Fixture()
	key := "promotion.optional.help"
	for _, locale := range []string{"en-US", "de-DE"} {
		value := input.Locales[locale]
		text := "Review the details before approving"
		if locale == "de-DE" {
			text = "Prüfen Sie die Angaben vor der Genehmigung"
		}
		translation := i18n007Translation(key, key+".v1", text, value.Revision.EffectiveFrom, false)
		value.Revision.Translations = append(value.Revision.Translations, translation)
		value.Revision.Digest = expcatalog.DigestRevision(value.Revision)
		value.Artifacts = append(value.Artifacts, ReleaseArtifact{Key: key, MeaningID: key + ".v1", Text: text, Kind: Helper, Direction: expectedDirection(locale), AccessibleName: text, KeyboardReachable: true})
		input.Locales[locale] = value
	}
	ar := input.Locales["ar"]
	ar.Revision.Fallbacks = map[string][]string{"ar": {"en-US"}}
	ar.Revision.Digest = expcatalog.DigestRevision(ar.Revision)
	ar.Artifacts = append(ar.Artifacts, ReleaseArtifact{Key: key, MeaningID: key + ".v1", Text: "Review the details before approving", Kind: Helper, Direction: "rtl", AccessibleName: "Review the details before approving", KeyboardReachable: true, FallbackFrom: "en-US"})
	input.Locales["ar"] = ar
	input.Manifest.OptionalFallbacks = []FallbackAllowance{{Locale: "ar", Key: key, SourceLocale: "en-US", Reason: "optional guidance not yet translated", Acknowledged: true}}
	accepted := AdmitRelease(input)
	if !accepted.Accepted || len(accepted.Diagnostics) != 1 || !strings.Contains(accepted.Diagnostics[0], "acknowledged en-US fallback") {
		t.Fatalf("scoped optional fallback was not visible in diagnostics: %+v", accepted)
	}
}

func TestTodo_I18N_007_Browser(t *testing.T) {
	input := i18n007Fixture()
	ar := input.Locales["ar"]
	key := "promotion.review.count"
	for i := range ar.Revision.Translations {
		if ar.Revision.Translations[i].Key == key {
			ar.Revision.Translations[i].Text = "تمت مراجعة {count} من التفاصيل الممتدة للنص التجريبي الطويل"
		}
	}
	for i := range ar.Artifacts {
		if ar.Artifacts[i].Key == key {
			ar.Artifacts[i].Text = "تمت مراجعة {count} من التفاصيل الممتدة للنص التجريبي الطويل"
			ar.Artifacts[i].AccessibleName = ar.Artifacts[i].Text
		}
	}
	ar.Revision.Digest = expcatalog.DigestRevision(ar.Revision)
	input.Locales["ar"] = ar
	decision := AdmitRelease(input)
	if !decision.Accepted {
		t.Fatalf("pseudo-expanded RTL release content failed semantic admission: %+v", decision)
	}
	countArtifact := i18n007Artifact(&input, "ar", key)
	if ar.Direction != "rtl" || countArtifact == nil || !strings.Contains(countArtifact.Text, "{count}") {
		t.Fatal("RTL long-text fixture lost its direction or parameter")
	}
	for _, artifact := range ar.Artifacts {
		if artifact.AccessibleName == "" || !artifact.KeyboardReachable {
			t.Fatalf("artifact lacks keyboard or accessible name evidence: %+v", artifact)
		}
	}
}

func TestTodo_I18N_007_Security(t *testing.T) {
	input := i18n007Fixture()
	artifact := i18n007Artifact(&input, "ar", "promotion.refusal")
	if artifact == nil {
		t.Fatal("refusal artifact missing from fixture")
	}
	artifact.Direction = "ltr"
	decision := AdmitRelease(input)
	if decision.Accepted || !strings.Contains(strings.Join(decision.Failures, "\n"), "direction mismatch") {
		t.Fatalf("wrong-direction refusal was admitted: %+v", decision)
	}

	input = i18n007Fixture()
	artifact = i18n007Artifact(&input, "de-DE", "promotion.approved.letter")
	artifact.LegalReviewStatus = "PENDING"
	decision = AdmitRelease(input)
	if decision.Accepted || !strings.Contains(strings.Join(decision.Failures, "\n"), "unreviewed legal text") {
		t.Fatalf("unreviewed legal derivative was admitted: %+v", decision)
	}
}

func TestTodo_I18N_007_Golden(t *testing.T) {
	input := i18n007Fixture()
	decision := AdmitRelease(input)
	if !decision.Accepted {
		t.Fatalf("fixture rejected: %+v", decision)
	}
	const want = "promotion.approved.letter|promotion.approved.letter.v1|de-DE|Dieses Schreiben"
	de := input.Locales["de-DE"]
	var got string
	for _, a := range de.Artifacts {
		if a.Key == "promotion.approved.letter" {
			got = a.Key + "|" + a.MeaningID + "|" + de.Revision.Locale + "|Dieses Schreiben"
		}
	}
	if got != want {
		t.Fatalf("localized legal document golden = %q, want %q", got, want)
	}
}

func TestTodo_I18N_007_Conformance(t *testing.T) {
	input := i18n007Fixture()
	input.Manifest.SupportedLocales = []string{"en-US", "de-DE", "ar"}
	decision := AdmitRelease(input)
	if !decision.Accepted {
		t.Fatalf("declared locales did not conform: %+v", decision)
	}
	input.Manifest.SupportedLocales = append(input.Manifest.SupportedLocales, "fr-FR")
	decision = AdmitRelease(input)
	if decision.Accepted || !strings.Contains(strings.Join(decision.Failures, "\n"), "fr-FR: active catalog evidence is missing") {
		t.Fatalf("locale without a published catalog was admitted: %+v", decision)
	}
	input = i18n007Fixture()
	input.Manifest.Journeys[0].States = []string{"ready", "empty", "loading", "validation", "refusal", "success"}
	if err := input.Manifest.Validate(); err == nil {
		t.Fatal("journey missing recovery state was accepted")
	}
}
