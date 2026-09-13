package productui

import (
	"math/big"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type workCollectionOptions struct {
	Title      string
	ListDetail bool
}

// workPage is a route adapter: it resolves application state into immutable,
// purpose-built props and delegates all markup to the component layer.
func workPage(view View) ui.Node {
	// The queue and its preview draw from the admitted population, so
	// denied proposal artifacts never render.
	scoped := view
	scoped.Work = admittedWork(view)
	collection := workCollectionProps(scoped, workCollectionOptions{Title: scoped.Locale.Text("work.promotion_journeys"), ListDetail: true})
	return ui.CreateElement(WorkPage, WorkPageProps{
		I18nProps:   I18nProps{Locale: scoped.Locale},
		Collection:  collection,
		Preview:     workPreviewProps(scoped, selectedOpenWork(scoped)),
		HidePreview: len(collection.Rows) == 0,
	})
}

func workCollectionProps(view View, options workCollectionOptions) WorkCollectionProps {
	tabs := []WorkTabProps{
		workTabProps(view, "", view.Locale.Text("work.all")),
		workTabProps(view, "review", view.Locale.Text("work.awaiting")),
		workTabProps(view, "blocked", view.Locale.Text("work.blocked")),
	}
	if options.ListDetail {
		tabs = append(tabs, WorkTabProps{Label: view.Locale.Text("work.past"), Href: statefulHref(view, PageHistory), Navigate: view.Navigate})
	}
	items := view.Work
	if view.WorkFilter == "" {
		items = OpenWorkItems(items)
	}
	// UXAUDIT-017: an action queue orders by urgency, not admission order.
	items = SortWorkByUrgency(items)
	selectedID := ""
	if options.ListDetail {
		selectedID = selectedOpenWork(view).ID
	}
	rows := make([]WorkRowProps, 0, len(items))
	for _, item := range items {
		rows = append(rows, WorkRowProps{
			ID: item.ID, Initials: item.Initials, PhotoURL: item.PhotoURL, Title: item.Title, Person: item.Person,
			Summary: item.Summary, Due: item.Due, JourneyStage: item.Status,
			StatusProjection: item.StatusProjection,
			Disposition:      approvalDispositionCardProps(view.Locale, item.Disposition),
			Href:             statefulHref(view, PageWork, "filter", view.WorkFilter, "selected", item.ID),
			Selected:         options.ListDetail && item.ID == selectedID, Navigate: view.Navigate,
		})
	}
	footer := WorkCollectionFooterProps{Label: view.Locale.Text("work.authorized")}
	if !options.ListDetail {
		footer.Action = ActionLinkProps{
			Label: view.Locale.Text("work.view"), Href: statefulHref(view, PageWork), Navigate: view.Navigate,
		}
	}
	return WorkCollectionProps{
		Title: options.Title, CountLabel: view.Locale.Plural("work.item_count", int64(len(items))), Tabs: tabs, Rows: rows,
		Footer: footer,
	}
}

func selectedOpenWork(view View) WorkItem {
	if view.WorkFilter != "" {
		return selectedWork(view)
	}
	items := OpenWorkItems(view.Work)
	if view.SelectedWork != "" {
		for _, item := range items {
			if item.ID == view.SelectedWork {
				return item
			}
		}
		return WorkItem{}
	}
	// No explicit selection: preview the same item the urgency-ordered
	// queue shows first, so the highlighted row and the preview panel never
	// disagree about which item "first" means.
	ordered := SortWorkByUrgency(items)
	if len(ordered) > 0 {
		return ordered[0]
	}
	return WorkItem{}
}

func workTabProps(view View, filter, label string) WorkTabProps {
	return WorkTabProps{Label: label, Href: statefulHref(view, PageWork, "filter", filter), Active: view.WorkFilter == filter, Navigate: view.Navigate}
}

func workPreviewProps(view View, item WorkItem) WorkPreviewProps {
	if item.ID == "" {
		return WorkPreviewProps{
			Empty: true, EmptyTitle: view.Locale.Text("work.nothing_selected"), EmptyDetail: view.Locale.Text("work.nothing_detail"),
			Action: ActionLinkProps{Label: view.Locale.Text("work.show_all"), Href: statefulHref(view, PageWork), Class: "button secondary", Navigate: view.Navigate},
		}
	}
	return WorkPreviewProps{
		ID: item.ID, Initials: item.Initials, PhotoURL: item.PhotoURL, Title: item.Title, Person: item.Person,
		Summary: item.Summary, JourneyStage: item.Status, StatusProjection: item.StatusProjection, Provenance: item.Provenance,
		Disposition: approvalDispositionCardProps(view.Locale, item.Disposition), FactsTitle: view.Locale.Text("work.server_proposal"),
		Facts: []FactProps{
			{Label: view.Locale.Text("work.effective_date"), Value: valueOrUnavailableFor(view.Locale, item.EffectiveDate)},
			{Label: view.Locale.Text("work.current_base"), Value: money(view.Locale, item.CurrentBase)},
			{Label: view.Locale.Text("work.proposed_base"), Value: money(view.Locale, item.ProposedBase)},
			{Label: view.Locale.Text("work.journey_id"), Value: item.ID},
		},
		Action: ActionLinkProps{Label: view.Locale.Text("work.open_journey"), Href: item.Href, Class: "button primary full", Navigate: view.Navigate},
	}
}

func valueOrUnavailable(value string) string {
	if value == "" {
		return "Not reported"
	}
	return value
}

func valueOrUnavailableFor(locale LocaleContext, value string) string {
	if value == "" {
		return locale.Text("common.not_reported")
	}
	return value
}

func money(locale LocaleContext, value values.Money) string {
	if value.Validate() != nil {
		return locale.Text("common.not_disclosed")
	}
	return locale.FormatMoney(value.Amount().String(), value.Currency(), int(value.Amount().Scale()))
}

// percentage formats an exact decimal ratio without converting compensation
// data through binary floating point.
func percentage(locale LocaleContext, value string) string {
	ratio, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return locale.Text("common.not_disclosed")
	}
	ratio.Mul(ratio, big.NewRat(100, 1))
	decimal := strings.TrimRight(strings.TrimRight(ratio.FloatString(2), "0"), ".")
	fraction := 0
	if point := strings.IndexByte(decimal, '.'); point >= 0 {
		fraction = len(decimal) - point - 1
	}
	return locale.FormatNumber(decimal, fraction) + "%"
}
