package workspace

import (
	"html"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/presentation"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/recovery"
)

// pageRecovery resolves only from the server-owned Page and its already
// resolved contract. It is a presentation projection: it does not authorize
// or execute anything, and it never derives a business outcome from markup.
func pageRecovery(page Page) presentation.Presentation {
	hasSimulation := page.Contract.Simulation.Status != ""
	return recovery.Resolve(recovery.Input{
		Authorization:        presentation.AuthorizationAllowed,
		Freshness:            presentation.FreshnessFresh,
		Operational:          presentation.OperationalReady,
		HasResult:            true,
		HasSimulation:        hasSimulation,
		ExternalOutcomeKnown: true,
		EvidenceRefs:         page.Reading.EvidenceIDs,
		Locale:               page.Locale.Resolved,
	})
}

// recoveryAttributes carries the server-resolved status and safe action set
// on the document already served by RenderPage. The browser may use these as
// display metadata, but cannot turn them into authority or a provider call.
func recoveryAttributes(page Page) string {
	projection := pageRecovery(page)
	actions := make([]string, 0, len(projection.Actions))
	for _, action := range projection.Actions {
		if action.Enabled {
			actions = append(actions, string(action.Action))
		}
	}
	return ` data-recovery-state="` + html.EscapeString(string(projection.State)) +
		`" data-recovery-actions="` + html.EscapeString(strings.Join(actions, ",")) + `"`
}
