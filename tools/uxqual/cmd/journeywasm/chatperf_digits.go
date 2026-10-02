package main

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// CHATBUG-014: every clock and every count on the page is written in the
// locale's numerals, and the ten numerals were asked of the number formatter
// again for each one: ten formatter calls per message time and per badge, on
// every render. Measured on the review machine that was about 100 ms of a load.
// A locale's numerals do not change, so they are worked out once.

// chatperfDigitTable is the ten digits as one locale writes them.
type chatperfDigitTable struct {
	digits [10]string
	// changed is false when the locale writes the Latin digits themselves, so
	// a string of them needs no rewriting.
	changed bool
}

var chatperfDigitTables sync.Map // LocaleContext.Resolved -> *chatperfDigitTable

// chatperfDigitsFor returns the digit table of a locale.
func chatperfDigitsFor(locale productui.LocaleContext) *chatperfDigitTable {
	if locale.Resolved == "" || locale.CatalogVersion == "" {
		// The formatter resolves such a context from what was requested; the
		// table is kept under the locale it resolves to.
		locale = productui.ResolveProductLocale(locale.Requested)
	}
	if cached, ok := chatperfDigitTables.Load(locale.Resolved); ok {
		return cached.(*chatperfDigitTable)
	}
	table := &chatperfDigitTable{}
	for i := range table.digits {
		latin := string(rune('0' + i))
		table.digits[i] = locale.FormatNumber(latin, 0)
		if table.digits[i] != latin {
			table.changed = true
		}
	}
	chatperfDigitTables.Store(locale.Resolved, table)
	return table
}
