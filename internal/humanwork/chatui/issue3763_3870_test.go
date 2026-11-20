package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Issue 3763: in Arabic a middle dot after Arabic-Indic digits reads as an
// extra zero (15 looks like 150). Arabic counts are written in parentheses so
// no dot ever touches the number.
func TestIssue3763_ArabicCountDoesNotEndInAMiddleDot(t *testing.T) {
	m := Model{Locale: "ar"}
	for _, tc := range []struct {
		label string
		count int
		want  string
	}{{"الأعضاء", 15, "الأعضاء (١٥)"}, {"مثبّتة", 1, "مثبّتة (١)"}} {
		got := countedLabel(m, tc.label, tc.count)
		if got != tc.want || strings.Contains(got, "·") {
			t.Errorf("countedLabel(%q, %d) = %q, want %q with no middle dot", tc.label, tc.count, got, tc.want)
		}
	}
	for locale, want := range map[string]string{"en-US": "Members · 15", "de-DE": "Mitglieder · 15"} {
		if got := countedLabel(Model{Locale: locale}, map[string]string{"en-US": "Members", "de-DE": "Mitglieder"}[locale], 15); got != want {
			t.Errorf("%s label = %q, want %q", locale, got, want)
		}
	}
}

func TestIssue3763_ArabicDetailsHeadingsCarryNoDotBesideDigits(t *testing.T) {
	m := chatux005Model("manager")
	m.Locale = "ar"
	m.Text = nil
	panel := chatux005Panel(t, m, localUI{})
	if bad := regexp.MustCompile(`·\s*[٠-٩]`).FindString(panel); bad != "" {
		t.Errorf("an Arabic heading still has a dot before its number: %q", bad)
	}
	if !regexp.MustCompile(`\([٠-٩]+\)`).MatchString(panel) {
		t.Errorf("Arabic headings carry no parenthesised count: %s", panel)
	}
}

// Issue 3870: the Conversation details header is sticky; a section jump
// (scrollIntoView block start) must park the heading below it.
func TestIssue3870_DetailsScrollParksSectionsBelowTheStickyHeader(t *testing.T) {
	header := regexp.MustCompile(`\.side-heading\{[^}]*height:(\d+)px[^}]*position:sticky`).FindStringSubmatch(Stylesheet)
	if header == nil {
		t.Fatal("the sticky side heading rule is gone")
	}
	height, _ := strconv.Atoi(header[1])
	padding := regexp.MustCompile(`\.chat-side\{scroll-padding-top:(\d+)px\}`).FindStringSubmatch(Stylesheet)
	if padding == nil {
		t.Fatal("the side column has no scroll-padding-top, so a section jump lands behind the sticky header")
	}
	if got, _ := strconv.Atoi(padding[1]); got < height+8 {
		t.Errorf("scroll-padding-top %dpx leaves the heading under the %dpx sticky header", got, height)
	}
	for _, section := range []string{`id="chat-details-pinned"`, `id="chat-details-members"`} {
		if !strings.Contains(chatux005Panel(t, chatux005Model("manager"), localUI{}), section) {
			t.Errorf("the panel has no %s to jump to", section)
		}
	}
}
