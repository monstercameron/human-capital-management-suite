package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	homeAttentionLimit = 5
	homeDraftLimit     = 3
	homeTrackedLimit   = 3
	homeRecentLimit    = 5
)

func homePage(view View) ui.Node {
	// Counts, recent activity, and the collection draw from the admitted
	// population, so denied journeys appear nowhere on home.
	population := admittedWork(view)
	workVisible := view.Allows(PageWork, "view")
	historyVisible := view.Allows(PageHistory, "view")
	workPopulation := population
	if !workVisible {
		workPopulation = nil
	}
	historyPopulation := population
	if !historyVisible {
		historyPopulation = nil
	}
	peopleVisible := view.Allows(PagePeople, "view")
	visiblePeople := admittedPeople(view)
	// UXLIVE-027/030: every journey number on Home is the server's one
	// authorized summary, the same one Insights and Journeys reconcile to.
	totals, _ := journeyTotals(view)
	// REV-069-02: the recent rail derives from the governed
	// completed-work history mapped by ID, never hand-picked per
	// row, so redefining "completed" only touches
	// CompletedHistory. The human task label still names the
	// person, so no detail line repeats it (UXLIVE-030).
	activities := completedActivityProps(view, CompletedHistory(historyPopulation), historyPopulation)
	if len(activities) > homeRecentLimit {
		activities = activities[:homeRecentLimit]
	}
	// REV-069-01: the announcements slot renders the
	// tenant-admin-authored stream through the governed
	// polite/assertive split, never hand-split.
	politeAnnouncements, assertiveAnnouncements := GovernAnnouncements(view.Announcements)
	announcementProps, showAnnouncements := homeAnnouncementProps(view, politeAnnouncements, assertiveAnnouncements)
	scoped := view
	viewerWork := MyWorkItems(workPopulation, view.Viewer)
	queue := ActionableWorkItems(viewerWork)
	orderedQueue := PrioritizeDueWork(queue)
	if len(orderedQueue) > homeAttentionLimit {
		orderedQueue = orderedQueue[:homeAttentionLimit]
	}
	scoped.Work = orderedQueue
	work := workCollectionProps(scoped, workCollectionOptions{Title: view.Locale.Text("home.attention_title")})
	work.CountLabel = view.Locale.Plural("work.item_count", int64(len(queue)))
	work.Description = view.Locale.Text("home.work_description")
	work.EmptyTitle = view.Locale.Text("work.action_queue_empty_title")
	work.EmptyDetail = view.Locale.Text("work.action_queue_empty_detail")
	if !view.Allows(PageWork, "view") {
		work.Footer.Action = ActionLinkProps{}
	}
	buckets := pageWorkBuckets(viewerWork, view.Viewer)
	draftsView := view
	// Drafts are a continuity surface, not actionable queue items. A
	// non-empty filter keeps workCollectionProps from narrowing them through
	// ActionQueue while preserving the shared row projection and due ordering.
	draftsView.WorkFilter = "drafts"
	draftsView.Work = PrioritizeDueWork(buckets.Drafts)
	if len(draftsView.Work) > homeDraftLimit {
		draftsView.Work = draftsView.Work[:homeDraftLimit]
	}
	drafts := workCollectionProps(draftsView, workCollectionOptions{Title: view.Locale.Text("home.drafts_title"), Kind: "drafts"})
	drafts.CountLabel = view.Locale.Plural("work.item_count", int64(len(buckets.Drafts)))
	drafts.Description = view.Locale.Text("home.drafts_description")
	drafts.Tabs = nil
	trackedItems := append(append([]WorkItem(nil), buckets.Tracked...), buckets.PassiveWaits...)
	tracked := trackedRequestsFor(view, trackedItems)
	// Tracked rows are status-only, but their status still comes from the
	// server's localized projection. Keep this adapter aligned with attention
	// and history so a status key never falls back to an English/raw label.
	localizedTrackedStatuses := make(map[string]string, len(trackedItems))
	for _, item := range trackedItems {
		if _, exists := localizedTrackedStatuses[item.ID]; !exists {
			localizedTrackedStatuses[item.ID] = localizedWorkStatus(view.Locale, item)
		}
	}
	for index := range tracked.Items {
		if status, ok := localizedTrackedStatuses[tracked.Items[index].ID]; ok {
			tracked.Items[index].Status = status
		}
	}
	if len(tracked.Items) > homeTrackedLimit {
		tracked.Items = tracked.Items[:homeTrackedLimit]
		tracked.More = ActionLinkProps{Label: view.Locale.Text("work.view"), Href: statefulHref(view, PageWork), Navigate: view.Navigate}
	}
	// People continuity follows the admitted work stream, including active
	// requests, while the activity rail remains terminal-only. The join still
	// drops anyone absent from the authorized people projection.
	recentPopulation := make([]WorkItem, 0, len(population))
	for _, item := range population {
		if item.Terminal && historyVisible || !item.Terminal && workVisible {
			recentPopulation = append(recentPopulation, item)
		}
	}
	recentPeople := RecentPeople(visiblePeople, recentPopulation, 5)
	for index := range recentPeople {
		if view.Allows(PagePerson, "view") {
			recentPeople[index].Href = statefulHref(view, PagePerson, "person", recentPeople[index].ID)
			recentPeople[index].Navigate = view.Navigate
		}
	}
	actions := ResolveHomeQuickActions(view)
	quickTitle := view.Locale.Text("home.start_title")
	if !(view.Allows(PageJourneys, "create") && view.Allows(PagePeople, "view")) {
		quickTitle = view.Locale.Text("home.quick_links_title")
	}
	groups := homeOperationalGroups(view, totals, workVisible, historyVisible, peopleVisible, visiblePeople)
	exceptions, showExceptions := homeExceptions(view, totals)
	recentRequests, showRecentRequests := homeRecentRequests(view)
	scopeKey := "home.activity_scope"
	if workVisible && !peopleVisible {
		scopeKey = "home.activity_scope_work"
	} else if !workVisible && peopleVisible {
		scopeKey = "home.activity_scope_people"
	}
	overview := SummaryCardProps{Title: view.Locale.Text("home.activity_title"), Description: view.Locale.Text(scopeKey), Groups: groups}
	if len(groups) == 0 {
		overview = SummaryCardProps{}
	}
	recent := RecentActivityProps{Title: view.Locale.Text("home.recent_completed_title"), Items: activities, EmptyTitle: view.Locale.Text("home.recent_completed_empty_title"), EmptyDescription: view.Locale.Text("home.recent_completed_empty_detail")}
	if !historyVisible {
		recent = RecentActivityProps{}
	}
	sections := make(map[string]bool, len(HomeFloorplanSections()))
	for _, section := range ResolveHomeFloorplan(view) {
		sections[section.ID] = true
	}
	if !sections[HomeSectionQuickActions] {
		actions = nil
	}
	// A visible employee count is useful context, not activity. A genuinely
	// quiet work stream should not expand four zero-state cards around it.
	// A failed read is not evidence of a quiet workspace: the shell reports
	// the failure, and Home makes no "no work" claim under it (UXLIVE-027).
	compactEmpty := totals.Total == 0 && len(population) == 0 && len(queue) == 0 && len(buckets.Drafts) == 0 && len(tracked.Items) == 0 && len(recentPeople) == 0 && len(activities) == 0
	return ui.CreateElement(HomePage, HomePageProps{
		Work: work, ShowWork: sections[HomeSectionAttention],
		// UXLIVE-030: a populated Home drops continuity cards whose own
		// population is empty; the summary card already states the zeros.
		Drafts: drafts, ShowDrafts: sections[HomeSectionRecentWork] && workVisible && len(buckets.Drafts) > 0,
		DraftsEmptyTitle: view.Locale.Text("home.drafts_empty_title"), DraftsEmptyDetail: view.Locale.Text("home.drafts_empty_detail"),
		Tracked: tracked, ShowTracked: sections[HomeSectionRecentWork] && workVisible && len(tracked.Items) > 0,
		RecentRequests: recentRequests, ShowRecentRequest: sections[HomeSectionRecentWork] && workVisible && showRecentRequests,
		Exceptions: exceptions, ShowExceptions: sections[HomeSectionAttention] && workVisible && showExceptions,
		RecentPeople: RecentPeopleProps{
			Title: view.Locale.Text("home.recent_people_title"), Description: view.Locale.Text("home.recent_people_description"), Items: recentPeople,
			EmptyTitle: view.Locale.Text("home.recent_people_empty_title"), EmptyDetail: view.Locale.Text("home.recent_people_empty_detail"),
		}, ShowPeople: sections[HomeSectionRecentWork] && peopleVisible && len(recentPeople) > 0,
		Overview: overview, ShowOverview: sections[HomeSectionSummaries] && len(groups) > 0,
		QuickStart: QuickActionsProps{Title: quickTitle, Class: "home-quick-actions", Actions: actions},
		Recent:     recent, ShowRecent: sections[HomeSectionRecentWork] && historyVisible && len(activities) > 0,
		Announcements: announcementProps, ShowAnnouncements: showAnnouncements,
		CompactEmpty: compactEmpty, SuppressEmptyClaim: view.LoadError != "",
		EmptyTitle: view.Locale.Text("home.empty_title"), EmptyDetail: view.Locale.Text("home.empty_detail"),
	})
}

func journeyCounts(items []WorkItem) (active, terminal int) {
	for _, item := range items {
		if item.Terminal {
			terminal++
		} else {
			active++
		}
	}
	return active, terminal
}
