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
	journeyState, journeyTone := "Connected", "positive"
	journeyAvailability := ActionState{}
	if view.LoadError != "" {
		journeyState, journeyTone = "Unavailable", "warning"
		journeyAvailability = ActionState{Availability: ActionUnavailable, Reason: view.Locale.Text("admin.journeys_unavailable_reason")}
	}
	// This organization has no page-configuration capability published yet:
	// the action is unavailable with its reason and no live link, never a
	// live link to something unavailable.
	studioAvailability := ActionState{Availability: ActionUnavailable, Reason: view.Locale.Text("admin.studio_unavailable_reason")}
	capabilities := []adminCapability{
		{
			page: PageRoles, title: "Roles & access", description: "Create tenant roles and assign one or more roles to every employee from the live workforce directory.", state: "Available", tone: "positive",
			actionLabel: "Manage roles →",
		},
		{
			page: PageOrganizationVisibility, title: "Organization visibility", description: "For each role, choose everyone, the employee's own unit, an approved set of units, or all units except a restricted set.", state: "Available", tone: "positive",
			actionLabel: "Configure visibility →",
		},
		{
			page: PageWorkerIDs, title: "Worker ID rules", description: "Issue organization-specific worker numbers from an atomic, non-reusing sequence with governed formatting rules.", state: "Available", tone: "positive",
			actionLabel: "Configure worker IDs →",
		},
		{
			page: PageAppearance, title: "Brand & appearance", description: "Governed palettes, shapes, density, glyphs, and motion are available across the product shell.", state: "Available", tone: "positive",
			actionLabel: "Configure appearance →",
		},
		{
			page: PageJourneys, title: "Journey service", description: view.Locale.Text("admin.journey_card_description"), state: journeyState, tone: journeyTone, availability: journeyAvailability,
			actionLabel: "Open details →",
		},
		{
			page: PageStudio, title: "Experience configuration", description: view.Locale.Text("admin.studio_card_description"), state: "Unavailable", tone: "warning", availability: studioAvailability,
			actionLabel: "Open details →",
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
	cards := make([]CapabilityCardProps, 0, len(capabilities))
	for _, capability := range capabilities {
		cards = append(cards, CapabilityCardProps{
			Title: capability.title, Description: capability.description, State: capability.state, Tone: capability.tone, Availability: capability.availability,
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
