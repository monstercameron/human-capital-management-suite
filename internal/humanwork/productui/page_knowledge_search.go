package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func knowledgeSearchPage(view View) ui.Node {
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
	})
}
