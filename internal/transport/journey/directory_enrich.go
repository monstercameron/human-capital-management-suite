package journey

import (
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
)

// enrichDemoDirectory names each worker's department and photograph and, when
// contacts is set, adds the work email and phone of the demo company served
// under tenantKey. A tenant that is not a demo company is left untouched: its
// workers keep the unit code alone and the client presents that.
//
// This is the one place a demo person's photograph is settled for Chat, the
// workspace shell and the organization chart: they all read the worker the two
// directory calls return. A photograph the worker row already carries wins.
//
// contacts is true only for the chat directory, the business-only projection;
// the full listing never gains contact fields.
func enrichDemoDirectory(tenantKey string, workers []*journeyv1.Worker, contacts bool) {
	pack, ok := demoworkforce.PackFor(tenantKey)
	if !ok {
		return
	}
	entries, err := pack.Directory()
	if err != nil {
		return
	}
	for _, worker := range workers {
		if worker == nil {
			continue
		}
		if name := pack.OrgUnitName(worker.GetOrgUnit()); name != "" {
			worker.OrgUnitName = name
		}
		entry, found := entries[strings.TrimSpace(worker.GetWorkerRef())]
		if !found {
			entry, found = entries[strings.TrimSpace(worker.GetSubjectId())]
		}
		if !found {
			continue
		}
		if strings.TrimSpace(worker.GetProfilePhotoUrl()) == "" {
			worker.ProfilePhotoUrl = entry.PhotoURL
		}
		if contacts {
			worker.WorkEmail, worker.WorkPhone = entry.Email, entry.Phone
		}
	}
}
