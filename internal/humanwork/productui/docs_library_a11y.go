package productui

import "strings"

// docsLibraryA11yCopy holds the list's screen-reader announcements. It is
// merged into docsLibraryCopy at start-up, so docsText finds it like any
// other Docs string.
var docsLibraryA11yCopy = map[string]map[string]string{
	"en-US": {
		"results_n": "{n} documents match “{q}”", "results_n_one": "1 document matches “{q}”", "results_none": "No documents match “{q}”",
	},
	"de-DE": {
		"results_n": "{n} Dokumente passen zu „{q}“", "results_n_one": "1 Dokument passt zu „{q}“", "results_none": "Keine Dokumente passen zu „{q}“",
	},
	"ar": {
		"results_n": "{n} مستندات تطابق «{q}»", "results_n_one": "مستند واحد يطابق «{q}»", "results_none": "لا توجد مستندات تطابق «{q}»",
	},
}

func init() {
	for locale, entries := range docsLibraryA11yCopy {
		if docsLibraryCopy[locale] == nil {
			docsLibraryCopy[locale] = map[string]string{}
		}
		for key, value := range entries {
			if _, taken := docsLibraryCopy[locale][key]; !taken {
				docsLibraryCopy[locale][key] = value
			}
		}
	}
}

// docsResultsAnnouncement is what the list's polite status region says once
// a search has settled: how many documents match the query. It is empty
// while no query is active and while the next result set is loading, so
// each settled answer is a change the screen reader announces.
func docsResultsAnnouncement(view View, route docsLibraryRoute) string {
	query := strings.TrimSpace(route.Query)
	if query == "" || view.Refreshing && view.RefreshingRegion == RefreshRegionDocuments {
		return ""
	}
	locale := view.Locale.Resolved
	total := max(view.DocumentTotal, len(view.Documents))
	text := docsText(locale, "results_none")
	if total > 0 {
		text = docsCount(locale, "results_n", total)
	}
	return strings.ReplaceAll(text, "{q}", query)
}

// docsAllOwnedByViewer reports a page whose every row the viewer owns, where
// the owner column would only repeat "You".
func docsAllOwnedByViewer(view View) bool {
	viewer := docsViewer(view)
	if viewer == "" || len(view.Documents) == 0 {
		return false
	}
	for _, document := range view.Documents {
		if document.OwnerID != viewer {
			return false
		}
	}
	return true
}
