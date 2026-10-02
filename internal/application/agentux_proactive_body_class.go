package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// Sponsored output is inspected under the already verified installation. The
// fictional demo inspector needs no fabricated human token for a schedule.
func (r *AgentAnnouncementRuntime) bodyClass(ctx context.Context, tenant, text string) (dlp.DataClass, error) {
	if local, ok := r.BodyClasses.(*LocalDevPersonaPostClassifier); ok {
		pack, known := demoworkforce.PackFor(tenant)
		if !known || pack.Key != tenant || !local.enabled || local.inspector == nil {
			return "", ErrPersonaAudienceFloorUnavailable
		}
		class, err := local.inspectText(text)
		if err != nil || class != dlp.ClassPublic && class != dlp.ClassInternal {
			return "", ErrPersonaAudienceFloorUnavailable
		}
		return class, nil
	}
	return personaRuntimePublicBodyClass(ctx, r.BodyClasses, tenant, text)
}
