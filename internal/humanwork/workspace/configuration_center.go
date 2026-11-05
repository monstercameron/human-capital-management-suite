package workspace

import (
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// configurationCenterAreas are the governed configuration areas the center
// can publish. Each area resolves its label, description and destination
// from the page registry and its state from live stores; areas the viewer
// may not open are omitted, never linked dead.
var configurationCenterAreas = []struct {
	id   string
	page productui.PageID
}{
	{"appearance", productui.PageAppearance},
	{"organization-visibility", productui.PageOrganizationVisibility},
}

// resolveConfigurationCenter builds the request-scoped configuration center
// projection from the already-loaded preference snapshot and the viewer's
// resolved access. It performs no reads of its own: versions come from the
// snapshot the page read once, names and destinations from the page
// registry, and visibility from the viewer's grants. A ready empty result
// is an honest no-areas outcome, not an unavailable service.
func resolveConfigurationCenter(locale productui.LocaleContext, access productAccess, snapshot preferences.Snapshot) *productui.ConfigurationCenterProjection {
	projection := &productui.ConfigurationCenterProjection{Ready: true}
	for _, area := range configurationCenterAreas {
		if !access.can(area.page, roleaccess.ActionView) {
			continue
		}
		definition, ok := productui.LookupPage(area.page)
		if !ok || strings.TrimSpace(definition.Route) == "" {
			continue
		}
		label := locale.Text(definition.TitleKey)
		description := locale.Text(definition.SubtitleKey)
		if strings.TrimSpace(label) == "" || strings.TrimSpace(description) == "" {
			continue
		}
		var version int64
		switch area.page {
		case productui.PageAppearance:
			version = snapshot.Theme.Version
		case productui.PageOrganizationVisibility:
			version = snapshot.OrganizationVisibility.Version
		}
		section := productui.ConfigurationSectionProjection{
			ID: area.id, Label: label, Description: description,
			State: "unpublished", Href: definition.Route,
		}
		if version > 0 {
			section.State = "published"
			section.Version = "v" + strconv.FormatInt(version, 10)
		}
		projection.Sections = append(projection.Sections, section)
	}
	return projection
}
