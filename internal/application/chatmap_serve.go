package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// What the served product supplies to location sharing (CHATMAP-005, 006).
//
// Who administers the workspace's location settings is decided the way the other
// workspace Chat settings are: by role, through the same authority that decides
// "Manage filters" and translation settings. A channel's owner or manager keeps
// the channel's own settings. Where a person works comes from the people
// directory's work location.

// chatmapAdmins answers LocationAdminPort. An empty conversation is the
// workspace.
type chatmapAdmins struct {
	// Workspace decides by role. Its channel check also requires that the person
	// can read the channel, so administering settings never buys access to a
	// private room.
	Workspace ChatFilterAuthority
	// Channel is the chat store: a channel's owner or one of its managers.
	Channel chat.LocationAdminPort
}

func (a chatmapAdmins) IsLocationAdmin(ctx context.Context, p chat.Principal, tenant, conversation string) bool {
	if p.SubjectID == "" || p.TenantID == "" || p.TenantID != tenant {
		return false
	}
	actor := chatfilter.Actor{Tenant: tenant, Subject: p.SubjectID, Channel: conversation}
	if conversation == "" {
		return a.Workspace.AuthorizeFilters(ctx, actor, "") == nil && a.Workspace.Facts != nil
	}
	if a.Workspace.Facts != nil && a.Workspace.CanReadFilterConversation(ctx, actor, conversation) && a.Workspace.AuthorizeFilters(ctx, actor, "") == nil {
		return true
	}
	return a.Channel != nil && a.Channel.IsLocationAdmin(ctx, p, tenant, conversation)
}

// chatmapCountries names the country a person works in from the work location
// the directory holds for them.
type chatmapCountries struct{ Directory chatmapWorkLocations }

// chatmapWorkLocations is the people directory's work location for one person.
type chatmapWorkLocations interface {
	WorkLocation(ctx context.Context, tenant, subject string) (string, error)
}

// chatmapDirectory finds the people directory behind the chat authority facts, or
// nothing when this deployment has no worker records.
func chatmapDirectory(facts ChatAuthorityFacts) chatmapWorkLocations {
	if cached, ok := facts.(cachedChatFacts); ok {
		facts = cached.inner
	}
	if direct, ok := facts.(currentRoleChatFacts); ok && direct.workers != nil && direct.tenantUUID != nil {
		return direct
	}
	return nil
}

func (f currentRoleChatFacts) WorkLocation(ctx context.Context, tenant, subject string) (string, error) {
	tenantID := f.tenantUUID(values.TenantId(tenant))
	tx, err := f.workers.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return "", err
	}
	worker, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, subject)
	if err != nil || !found {
		return "", err
	}
	return worker.Location, nil
}

// LocationCountry is asked only about the signed-in person. A person whose work
// location cannot be read has no country, and the workspace-wide setting then
// applies on its own.
func (c chatmapCountries) LocationCountry(ctx context.Context, p chat.Principal) string {
	if c.Directory == nil {
		return ""
	}
	if actor, ok := trust.FromContext(ctx); !ok || actor == nil || actor.Subject() != p.SubjectID || actor.Tenant().String() != p.TenantID {
		return ""
	}
	location, err := c.Directory.WorkLocation(ctx, p.TenantID, p.SubjectID)
	if err != nil {
		return ""
	}
	return chatmapCountryOf(location)
}

// chatmapUSStates are the two-letter state codes. A work location such as
// "Boston, MA" names a state; a code that is also a country code (DE, IN, CA)
// is read as the state, so a site in another country is written with the
// country's name ("Berlin, Germany").
var chatmapUSStates = map[string]bool{
	"AL": true, "AK": true, "AZ": true, "AR": true, "CA": true, "CO": true, "CT": true, "DE": true, "DC": true, "FL": true,
	"GA": true, "HI": true, "ID": true, "IL": true, "IN": true, "IA": true, "KS": true, "KY": true, "LA": true, "ME": true,
	"MD": true, "MA": true, "MI": true, "MN": true, "MS": true, "MO": true, "MT": true, "NE": true, "NV": true, "NH": true,
	"NJ": true, "NM": true, "NY": true, "NC": true, "ND": true, "OH": true, "OK": true, "OR": true, "PA": true, "RI": true,
	"SC": true, "SD": true, "TN": true, "TX": true, "UT": true, "VT": true, "VA": true, "WA": true, "WV": true, "WI": true, "WY": true,
}

var chatmapCountryNames = map[string]string{
	"united states": "US", "united states of america": "US", "usa": "US", "us": "US",
	"canada": "CA", "mexico": "MX", "brazil": "BR", "united kingdom": "GB", "uk": "GB", "great britain": "GB", "england": "GB", "scotland": "GB", "wales": "GB",
	"germany": "DE", "deutschland": "DE", "france": "FR", "spain": "ES", "italy": "IT", "netherlands": "NL", "belgium": "BE", "austria": "AT",
	"switzerland": "CH", "ireland": "IE", "portugal": "PT", "poland": "PL", "sweden": "SE", "norway": "NO", "denmark": "DK", "finland": "FI",
	"czechia": "CZ", "czech republic": "CZ", "hungary": "HU", "romania": "RO", "greece": "GR", "india": "IN", "japan": "JP", "china": "CN",
	"south korea": "KR", "singapore": "SG", "australia": "AU", "new zealand": "NZ", "united arab emirates": "AE", "uae": "AE",
	"saudi arabia": "SA", "egypt": "EG", "south africa": "ZA", "philippines": "PH", "turkey": "TR", "ukraine": "UA", "israel": "IL",
}

// chatmapCodes are the country codes read from the end of a work location when
// they are not also a US state code.
var chatmapCodes = map[string]bool{
	"AT": true, "AU": true, "BE": true, "BG": true, "BR": true, "CH": true, "CN": true, "CZ": true, "DK": true, "EE": true, "EG": true,
	"ES": true, "FI": true, "FR": true, "GB": true, "GR": true, "HR": true, "HU": true, "IE": true, "IT": true, "JP": true, "KR": true,
	"LT": true, "LU": true, "LV": true, "MX": true, "NL": true, "NO": true, "NZ": true, "PH": true, "PL": true, "PT": true, "RO": true,
	"SA": true, "SE": true, "SG": true, "SI": true, "SK": true, "TR": true, "UA": true, "UK": true, "AE": true, "ZA": true,
}

// chatmapNormalizeCountry turns what an administrator typed (a code or a
// country's name) into the code the settings are stored under.
func chatmapNormalizeCountry(in string) string {
	in = strings.TrimSpace(in)
	if in == "*" {
		return in
	}
	if code, ok := chatmapCountryNames[strings.ToLower(in)]; ok {
		return code
	}
	in = strings.ToUpper(in)
	if in == "UK" {
		return "GB"
	}
	return in
}

// chatmapCountryOf reads a country code from a free-text work location: the
// text after its last comma, as a country's name, a country code or a US state.
// An unreadable location has no country, and the workspace-wide setting then
// applies on its own.
func chatmapCountryOf(location string) string {
	parts := strings.Split(location, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" || len(parts) < 2 && len(last) <= 2 {
		return ""
	}
	if code, ok := chatmapCountryNames[strings.ToLower(last)]; ok {
		return code
	}
	upper := strings.ToUpper(last)
	switch {
	case len(upper) == 2 && chatmapUSStates[upper]:
		return "US"
	case len(upper) == 2 && chatmapCodes[upper]:
		return chatmapNormalizeCountry(upper)
	}
	return ""
}

// chatmapHumanGrant lets people share a location and keeps agents out: an
// agent's grant is a skill grant, and none is composed here.
type chatmapHumanGrant struct{}

func (chatmapHumanGrant) AllowsLocation(ctx context.Context, _ chat.Principal, _, _ string) bool {
	actor, ok := trust.FromContext(ctx)
	return ok && actor != nil && actor.SubjectKind() == trust.SubjectKindHuman
}

// chatmapNoSites is the sites port for a deployment that has no project
// assignment feed yet: the job-site choice is empty, never wrong.
type chatmapNoSites struct{}

func (chatmapNoSites) ReadLocationSite(context.Context, chat.Principal, string) (chat.LocationSite, error) {
	return chat.LocationSite{}, chat.ErrNotFound
}
func (chatmapNoSites) SearchLocationSites(context.Context, chat.Principal, string) ([]chat.LocationSite, error) {
	return []chat.LocationSite{}, nil
}
func (chatmapNoSites) ResolveSharedLocationSite(context.Context, string, string) (chat.LocationSite, error) {
	return chat.LocationSite{}, chat.ErrNotFound
}

// chatmapServedInput fills the location ports a deployment did not supply. The
// address lookup stays unavailable until the owner approves a data source.
func chatmapServedInput(in ChatComposition) ChatComposition {
	if isNilPersonaOutputPort(in.LocationGrant) {
		in.LocationGrant = chatmapHumanGrant{}
	}
	if isNilPersonaOutputPort(in.LocationSites) {
		in.LocationSites = chatmapNoSites{}
	}
	if isNilPersonaOutputPort(in.LocationLookup) {
		in.LocationLookup = chat.UnavailableAddressLookup{}
	}
	if isNilPersonaOutputPort(in.LocationUsage) {
		in.LocationUsage = ChatmapNoopUsage{}
	}
	return in
}

// chatmapBindGovernance gives the composed surface its administrator and
// country ports.
func chatmapBindGovernance(surface ChatmapSurface, admins chat.LocationAdminPort, countries chat.LocationCountryResolver) {
	if s, ok := surface.(*integrate2Location); ok && s != nil && s.LocationService != nil {
		s.LocationService.Admin, s.LocationService.Country = admins, countries
	}
}
