package chatui

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATMOD_003_Browser_HitsCountZero: the "What the filters caught" status
// line writes its count in every locale, zero included, with the singular and
// plural forms of the language. The client's Number callback writes an empty
// string for zero, so the status line must not depend on it. A count that has
// not been read yet writes no status line at all.
func TestTodo_CHATMOD_003_Browser_HitsCountZero(t *testing.T) {
	at := time.Date(2026, 10, 1, 14, 5, 0, 0, time.UTC)
	record := func(id int64) chatfilter.Record {
		return chatfilter.Record{ID: id, Tenant: "t", Channel: "room", At: at.Add(time.Duration(id) * time.Minute),
			Hit: chatfilter.Hit{RuleID: "r-codes", RuleName: "Client names", Version: "1.2.0", Action: "block", Target: ""}}
	}
	want := map[string]map[int]string{
		"en-US": {0: "0 matches", 1: "1 match", 3: "3 matches"},
		"de-DE": {0: "0 Treffer", 1: "1 Treffer", 3: "3 Treffer"},
		"ar":    {0: "لا مطابقات", 1: "مطابقة واحدة", 3: "٣ مطابقات"},
	}
	for _, locale := range chatmodLocales {
		// The client's own formatter: zero is the empty string, other counts are localized.
		clientNumber := func(n int) string {
			if n == 0 {
				return ""
			}
			return chatNumeral(locale, strconv.Itoa(n))
		}
		m := Model{State: StateReady, Locale: locale, SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t", IsTenantAdmin: true, Number: clientNumber,
			Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}}
		render := func(p ModHitsProps) string {
			p.Model, p.Now = m, at.Add(24*time.Hour)
			return chatremoveMarkup(t, ModHitsPanel(p))
		}
		for _, count := range []int{0, 1, 3} {
			records := make([]chatfilter.Record, 0, count)
			for i := 0; i < count; i++ {
				records = append(records, record(int64(i+1)))
			}
			markup := render(ModHitsProps{Records: records, Loaded: true, Load: func(bool) {}})
			status := regexp.MustCompile(`<p[^>]*role="status"[^>]*>\s*` + regexp.QuoteMeta(want[locale][count]) + `\s*</p>`)
			if !status.MatchString(markup) {
				t.Errorf("%s: %d matches: the status line does not read %q as a live status: %s", locale, count, want[locale][count], markup)
			}
		}
		// Before a read, and while one is in flight, no status line is written.
		for _, p := range []ModHitsProps{{Load: func(bool) {}}, {Loading: true, Load: func(bool) {}}} {
			if markup := render(p); regexp.MustCompile(`role="status"`).MatchString(markup) {
				t.Errorf("%s: a count was written before any read: %s", locale, markup)
			}
		}
	}
}
