package productui

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// TestObjectIdentityComposesTypedSlots is UXLIVE-032's shared identity
// header: the accessible name is composed from the typed slots in each
// locale's template, and an empty slot leaves no dangling separator.
func TestObjectIdentityComposesTypedSlots(t *testing.T) {
	identity := ObjectIdentity{Primary: "Promotion for Amara", ReferenceLabel: "Request", Reference: "8CF888", Status: "Awaiting approval"}
	english := ResolveProductLocale("en-US")
	if got := identity.AccessibleName(english); got != "Promotion for Amara, Request 8CF888, Awaiting approval" {
		t.Fatalf("en-US accessible name = %q", got)
	}
	if got := identity.ReferenceText(english); got != "Request 8CF888" {
		t.Fatalf("reference text = %q", got)
	}
	if got := identity.AccessibleName(ResolveProductLocale("ar")); got != "Promotion for Amara، Request 8CF888، Awaiting approval" {
		t.Fatalf("ar accessible name = %q", got)
	}
	for name, tc := range map[string]struct {
		identity ObjectIdentity
		want     string
	}{
		"no reference": {ObjectIdentity{Primary: "P", ReferenceLabel: "Request", Status: "Open"}, "P, Open"},
		"no status":    {ObjectIdentity{Primary: "P", ReferenceLabel: "Request", Reference: "R1"}, "P, Request R1"},
		"no label":     {ObjectIdentity{Primary: "P", Reference: "R1"}, "P, R1"},
		"primary only": {ObjectIdentity{Primary: "P"}, "P"},
	} {
		if got := tc.identity.AccessibleName(english); got != tc.want {
			t.Errorf("%s: accessible name = %q, want %q", name, got, tc.want)
		}
	}
	if (ObjectIdentity{Reference: " "}).ReferenceText(english) != "" {
		t.Error("a blank reference produced reference text")
	}
}

// TestJourneyListFilterVocabularyAndAddress is UXLIVE-031's route-state
// contract in the product router: the tracker's keys belong to the Journeys
// profile, are strictly validated, and have exactly one spelling.
func TestJourneyListFilterVocabularyAndAddress(t *testing.T) {
	profile, _, ok := PageProfiles(PageJourneys)
	if !ok {
		t.Fatal("Journeys has no route profile")
	}
	keys := strings.Join(profile.QueryKeys(), ",")
	for _, key := range JourneyListRouteKeys() {
		if !strings.Contains(keys, key) {
			t.Fatalf("Journeys does not admit %s: %s", key, keys)
		}
	}
	people, _, _ := PageProfiles(PagePeople)
	if strings.Contains(strings.Join(people.QueryKeys(), ","), JourneyListQueryKey) {
		t.Fatal("the tracker's keys leaked onto People")
	}
	for raw, want := range map[string]bool{
		"journey_status=review&journey_sort=oldest&journey_group=none&journey_from=2026-09-01": true,
		"journey_status=everything": false, "journey_sort=newest": false, "journey_group=team": false, "journey_to=2026-02-30": false,
	} {
		values, _ := url.ParseQuery(raw)
		if got := profile.ValidControlledValues(values); got != want {
			t.Errorf("ValidControlledValues(%q) = %v, want %v", raw, got, want)
		}
	}
	request := PageRequest{Page: PageJourneys, JourneyList: JourneyListFilter{Query: "  Amara   O ", Status: "REVIEW", Sort: "recent", Group: "person", From: "2026-09-30", To: "2026-09-01"}}
	values := profile.CanonicalValues(request, map[string]bool{})
	if got := values.Encode(); got != "journey_from=2026-09-01&journey_q=Amara+O&journey_status=review&journey_to=2026-09-30" {
		t.Fatalf("canonical tracker state = %q", got)
	}
	view := ApplyRequest(NewView(PageJourneys, "t", "p", "s"), request)
	address := url.Values{}
	profile.AddressValues(address, view)
	if address.Get(JourneyListQueryKey) != "Amara O" || address.Get(JourneyListSortKey) != "" {
		t.Fatalf("generated links lost or misspelled the tracker state: %v", address)
	}
	view.JourneyID = "int-1"
	detail := url.Values{}
	profile.AddressValues(detail, view)
	if detail.Get(JourneyListQueryKey) != "" {
		t.Fatalf("a journey detail address carries the list filter: %v", detail)
	}
}

// TestJourneyListFilterPrimitives covers the search and date primitives the
// tracker shares with Workflow History's matching.
func TestJourneyListFilterPrimitives(t *testing.T) {
	if !MatchesJourneyListQuery("amara 8cf", "Amara Okafor", "8CF888") || MatchesJourneyListQuery("amara omar", "Amara Okafor", "8CF888") || !MatchesJourneyListQuery("  ", "x") {
		t.Fatal("search must require every term, ignore case, and treat blank as everything")
	}
	for _, tc := range []struct {
		date, from, to string
		want           bool
	}{
		{"2026-09-10", "2026-09-01", "2026-09-30", true},
		{"2026-09-30", "2026-09-01", "2026-09-30", true},
		{"2026-10-01", "2026-09-01", "2026-09-30", false},
		{"2026-08-31", "2026-09-01", "", false},
		{"", "2026-09-01", "", false},
		{"", "", "", true},
	} {
		if got := JourneyListDateInRange(tc.date, tc.from, tc.to); got != tc.want {
			t.Errorf("JourneyListDateInRange(%q, %q, %q) = %v", tc.date, tc.from, tc.to, got)
		}
	}
	long := NormalizeJourneyListFilter(JourneyListFilter{Query: strings.Repeat("é", 200)})
	if len([]rune(long.Query)) != journeyListQueryMaxRunes {
		t.Fatalf("an unbounded search reached the address: %d runes", len([]rune(long.Query)))
	}
	if (JourneyListFilter{Sort: "oldest"}).Narrowing() || !(JourneyListFilter{To: "2026-01-01"}).Narrowing() || !(JourneyListFilter{Group: "person"}).IsZero() {
		t.Fatal("sort and grouping must not count as narrowing, and defaults must be zero")
	}
}

// TestJourneyTrackerCopyIsCompleteInEveryLocale keeps the new product copy
// translated rather than silently falling back to English, with the same
// placeholders in every locale.
func TestJourneyTrackerCopyIsCompleteInEveryLocale(t *testing.T) {
	placeholders := regexp.MustCompile(`\{[a-z]+\}`)
	english := productMessages[DefaultProductLocale]
	for _, locale := range []string{"de-DE", "ar"} {
		translated := journeyTrackerTranslations(locale)
		for key, text := range translated {
			source, ok := english[key]
			if !ok {
				t.Fatalf("%s key %q has no English source", locale, key)
			}
			if strings.Join(placeholders.FindAllString(source.Text, -1), ",") != strings.Join(placeholders.FindAllString(text, -1), ",") {
				t.Errorf("%s %q placeholders differ: %q vs %q", locale, key, text, source.Text)
			}
		}
		for _, key := range []string{"journey.filter_result", "journey.card_title", "people.workflows_quiet", "work.more_filters", "identity.name_reference_status"} {
			if _, ok := translated[key]; !ok {
				t.Errorf("%s is missing %q", locale, key)
			}
			for _, missing := range MissingProductTranslations(locale) {
				if missing == key {
					t.Errorf("%s falls back to English for %q", locale, key)
				}
			}
		}
		if len(translated) != 33 {
			t.Errorf("%s translates %d tracker keys, want all 33", locale, len(translated))
		}
	}
	if journeyTrackerTranslations("en-US") != nil {
		t.Error("the English source must live in the reviewed catalog, not in the translation hook")
	}
}
