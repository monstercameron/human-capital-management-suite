package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// adminCapability binds one Admin home card to the page
// its action opens, so the home can resolve its cards
// through registry authorization.
type adminCapability struct {
	page         PageID
	title        string
	description  string
	state        string
	tone         string
	availability ActionState
	actionLabel  string
}

func adminPage(view View) ui.Node {
	journeyState, journeyTone := view.Locale.Text("admin.available"), "positive"
	journeyAvailability := ActionState{}
	if view.LoadError != "" {
		journeyState, journeyTone = view.Locale.Text("admin.unavailable"), "warning"
		journeyAvailability = ActionState{Availability: ActionUnavailable, Reason: view.Locale.Text("admin.journeys_unavailable_reason")}
	}
	// This organization has no page-configuration capability published yet:
	// the action is unavailable with its reason and no live link, never a
	// live link to something unavailable.
	studioAvailability := ActionState{Availability: ActionUnavailable, Reason: view.Locale.Text("admin.studio_unavailable_reason")}
	capabilities := []adminCapability{
		{
			page: PageRoles, title: view.Locale.Text("page.roles.title"), description: view.Locale.Text("admin.roles_description"), state: view.Locale.Text("admin.available"), tone: "positive",
			actionLabel: view.Locale.Text("admin.roles_action"),
		},
		{
			page: PageOrganizationVisibility, title: view.Locale.Text("page.organization_visibility.title"), description: view.Locale.Text("admin.visibility_description"), state: view.Locale.Text("admin.available"), tone: "positive",
			actionLabel: view.Locale.Text("admin.visibility_action"),
		},
		{
			page: PageWorkerIDs, title: view.Locale.Text("page.worker_ids.title"), description: view.Locale.Text("admin.worker_ids_description"), state: view.Locale.Text("admin.available"), tone: "positive",
			actionLabel: view.Locale.Text("admin.worker_ids_action"),
		},
		{
			page: PageAppearance, title: view.Locale.Text("page.appearance.title"), description: view.Locale.Text("admin.appearance_description"), state: view.Locale.Text("admin.available"), tone: "positive",
			actionLabel: view.Locale.Text("admin.appearance_action"),
		},
		{
			page: PageChatSettings, title: retentionCopy(view.Locale).settingsTitle, description: retentionCopy(view.Locale).settingsDescription, state: view.Locale.Text("admin.available"), tone: "positive",
			actionLabel: retentionCopy(view.Locale).settingsAction,
		},
		{
			page: PageJourneys, title: view.Locale.Text("admin.promotion_title"), description: view.Locale.Text("admin.promotion_description"), state: journeyState, tone: journeyTone, availability: journeyAvailability,
			actionLabel: view.Locale.Text("admin.promotion_action"),
		},
		{
			page: PageStudio, title: view.Locale.Text("admin.custom_title"), description: view.Locale.Text("admin.custom_description"), state: view.Locale.Text("admin.planned"), tone: "warning", availability: studioAvailability,
			actionLabel: view.Locale.Text("admin.promotion_action"),
		},
	}
	// An admitted identity only sees cards for pages it may
	// open; transport authorization remains the enforcement
	// boundary. A nil role set is the unrestricted component
	// preview and keeps the full card set.
	if view.Roles != nil {
		resolved := capabilities[:0]
		for _, capability := range capabilities {
			if PageVisible(capability.page, view.Roles) {
				resolved = append(resolved, capability)
			}
		}
		capabilities = resolved
	}
	available := view.Locale.Text("admin.available")
	cards := make([]CapabilityCardProps, 0, len(capabilities))
	for _, capability := range capabilities {
		// The ordinary state is not news. Only a card whose state differs
		// from available carries a badge (UXLIVE-014).
		state := capability.state
		if state == available {
			state = ""
		}
		cards = append(cards, CapabilityCardProps{
			Title: capability.title, Description: capability.description, State: state, Tone: capability.tone, Availability: capability.availability,
			Action: ActionLinkProps{Label: capability.actionLabel, Href: statefulHref(view, capability.page), Navigate: view.Navigate},
		})
	}
	return ui.CreateElement(AdminPage, AdminPageProps{
		Hero: AdminHeroProps{
			Eyebrow: view.Locale.Text("admin.hero_eyebrow"), Title: valueOrUnavailable(view.Tenant), Description: view.Locale.Text("admin.hero_description"),
		},
		Capabilities: cards,
	})
}
