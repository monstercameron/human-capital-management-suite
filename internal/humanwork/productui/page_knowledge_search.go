package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func knowledgeSearchPage(view View) ui.Node {
	var results []KnowledgeSearchResult
	searched := false
	if view.SearchKnowledge != nil && strings.TrimSpace(view.Query) != "" {
		var err error
		results, err = view.SearchKnowledge(view.Query)
		searched = err == nil
		if err != nil {
			results = nil
		}
	}
	return ui.CreateElement(KnowledgeSearchPage, KnowledgeSearchPageProps{
		Query:       view.Query,
		Action:      statefulHref(view, PageKnowledgeSearch),
		Label:       view.Locale.Text("global_search.label"),
		Placeholder: view.Locale.Text("global_search.placeholder"),
		SubmitLabel: view.Locale.Text("history.apply"),
		Navigate:    view.Navigate,
		Unavailable: EmptyStateProps{
			Title:       view.Locale.Text("knowledge_search.unavailable_title"),
			Description: view.Locale.Text("knowledge_search.unavailable_detail"),
			Role:        "status",
			Action: &ActionLinkProps{
				Label: view.Locale.Text("knowledge_search.return_home"), Href: statefulHref(view, PageHome),
				Class: "button secondary", Navigate: view.Navigate,
			},
		},
		Results:      results,
		ResultsLabel: view.Locale.Text("knowledge_search.results"),
		NoResults: EmptyStateProps{
			Title:       view.Locale.Text("knowledge_search.no_results_title"),
			Description: view.Locale.Text("knowledge_search.no_results_detail"),
			Role:        "status",
		},
		SearchCompleted: searched,
	})
}
