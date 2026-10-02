package application

import (
	"context"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// ChattoneRoleAdministration is the production ChattoneAdministration: the
// person must be the authenticated caller and hold the workspace
// administrator role in the current governance facts (the same rule the chat
// filter administration uses). A request's own claims are never evidence.
type ChattoneRoleAdministration struct {
	Facts ChatAuthorityFacts
	Now   func() time.Time
}

func (a ChattoneRoleAdministration) ManageWritingStyles(ctx context.Context, id chatrewrite.Identity) error {
	if ctx == nil || a.Facts == nil || !id.Valid() {
		return personachat.ErrDenied
	}
	at := time.Now().UTC()
	if a.Now != nil {
		at = a.Now().UTC()
	}
	p, err := newChatAuthoritySource(a.Facts).Resolve(ctx, id.Tenant, id.Person, at)
	if err != nil || !slices.Contains(p.Roles, "hcm_admin") {
		return personachat.ErrDenied
	}
	return nil
}

var _ ChattoneAdministration = ChattoneRoleAdministration{}
