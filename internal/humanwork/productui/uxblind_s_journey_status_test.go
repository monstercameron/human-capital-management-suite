package productui

import "testing"

func TestTodo_UXBLIND_072(t *testing.T) {
	entries := JourneyStatusTaxonomy()
	if len(entries) == 0 {
		t.Fatal("journey status taxonomy is empty")
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.Stage] {
			t.Fatalf("duplicate journey stage %q", entry.Stage)
		}
		seen[entry.Stage] = true
		if got := JourneyStatusForStage(entry.Stage); got != entry {
			t.Errorf("stage %q lookup = %+v, want %+v", entry.Stage, got, entry)
		}
	}
	if JourneyHomeFilter(JourneyHomeGroupInProgress) != JourneyListStatusOpen ||
		JourneyHomeFilter(JourneyHomeGroupProblem) != JourneyListStatusIssue ||
		JourneyHomeFilter(JourneyHomeGroupClosed) != JourneyListStatusClosed {
		t.Fatal("Home group links are not backed by the journey status vocabulary")
	}
}

func TestTodo_UXBLIND_072_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := ResolveProductLocale(locale)
		for _, entry := range JourneyStatusTaxonomy() {
			if got := copy.Text(entry.BadgeKey); got == "" || got == entry.BadgeKey {
				t.Errorf("%s badge %s is not localized: %q", locale, entry.Stage, got)
			}
		}
	}
}

func TestTodo_UXBLIND_072_Property(t *testing.T) {
	filters := map[string]bool{}
	for _, filter := range JourneyListStatuses() {
		filters[filter] = true
	}
	home := map[string]bool{
		JourneyHomeGroupInProgress: true,
		JourneyHomeGroupProblem:    true,
		JourneyHomeGroupClosed:     true,
	}
	seenBadges := map[string]string{}
	for _, entry := range JourneyStatusTaxonomy() {
		if !filters[entry.Filter] {
			t.Fatalf("badge %q maps to an unavailable filter %q", entry.BadgeKey, entry.Filter)
		}
		if !home[entry.HomeGroup] {
			t.Fatalf("badge %q maps to unknown Home group %q", entry.BadgeKey, entry.HomeGroup)
		}
		if prior, exists := seenBadges[entry.BadgeKey]; exists && prior != entry.Stage {
			t.Fatalf("badge %q maps to multiple stages: %s and %s", entry.BadgeKey, prior, entry.Stage)
		}
		seenBadges[entry.BadgeKey] = entry.Stage
	}
	if len(seenBadges) != len(JourneyStatusTaxonomy()) {
		t.Fatal("each journey stage must have exactly one badge mapping")
	}
}
