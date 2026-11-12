package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXBLIND_072_HistoryStageLabel proves workflow history stage cells use
// the journey badge and filter-group wording instead of raw stage enums and
// internal status-group keys (live audit: "JOURNEY.STAGE_PROPOSED · problem").
func TestTodo_UXBLIND_072_HistoryStageLabel(t *testing.T) {
	cases := []struct {
		locale string
		stage  string
		want   string
	}{
		{"en-US", "PROPOSED", "Ready to start approval · In review"},
		{"en-US", "REJECTED", "Rejected · Closed"},
		{"en-US", "BLOCKED", "Blocked · Needs attention"},
	}
	for _, tc := range cases {
		got := workflowHistoryStageLabel(ResolveProductLocale(tc.locale), tc.stage)
		if got != tc.want {
			t.Errorf("%s stage %s = %q, want %q", tc.locale, tc.stage, got, tc.want)
		}
		for _, raw := range []string{"JOURNEY.", "STAGE_", "PROPOSED", "REJECTED", "in_progress", "problem", "closed"} {
			if strings.Contains(got, raw) {
				t.Errorf("%s stage %s leaks raw token %q in %q", tc.locale, tc.stage, raw, got)
			}
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		copy := ResolveProductLocale(locale)
		for _, entry := range JourneyStatusTaxonomy() {
			got := workflowHistoryStageLabel(copy, entry.Stage)
			if strings.Contains(got, "JOURNEY.") || strings.Contains(got, "_") || strings.Contains(got, entry.Stage) {
				t.Errorf("%s stage %s leaks raw key in %q", locale, entry.Stage, got)
			}
		}
	}
}
