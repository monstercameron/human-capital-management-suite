package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type integrate2Location struct {
	*chat.LocationService
	Routes interface {
		ChatWriteContext(context.Context, string, string) (context.Context, error)
	}
}

func (s *integrate2Location) Attach(ctx context.Context, r chat.AttachLocationRequest) (chat.LocationShare, error) {
	if s.Routes == nil || s.LocationService == nil || isNilPersonaOutputPort(s.Grant) {
		return chat.LocationShare{}, chat.ErrLocationUnavailable
	}
	if !s.Grant.AllowsLocation(ctx, r.Principal, r.TenantID, r.ConversationID) {
		return chat.LocationShare{}, chat.ErrPermissionDenied
	}
	leased, err := s.Routes.ChatWriteContext(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return chat.LocationShare{}, err
	}
	return s.LocationService.Attach(leased, r)
}
func (s *integrate2Location) End(ctx context.Context, p chat.Principal, key chat.LocationKey) error {
	if s.Routes == nil {
		return chat.ErrLocationUnavailable
	}
	leased, err := s.Routes.ChatWriteContext(ctx, key.TenantID, key.ConversationID)
	if err != nil {
		return err
	}
	return s.LocationService.End(leased, p, key)
}

func integrate2ComposeLocations(routed chat.ConversationService, repo chat.LocationRepository, input ChatComposition, now chat.Clock) (ChatmapSurface, ChatmapPictures) {
	reader, readable := routed.(chat.AuthorizedReferenceReader)
	routes, writable := routed.(interface {
		ChatWriteContext(context.Context, string, string) (context.Context, error)
	})
	if !readable || !writable || isNilPersonaOutputPort(repo) || isNilPersonaOutputPort(input.LocationGrant) || isNilPersonaOutputPort(input.LocationSites) || isNilPersonaOutputPort(input.LocationLookup) || isNilPersonaOutputPort(input.LocationUsage) {
		return nil, nil
	}
	service := &chat.LocationService{Chat: reader, Repo: repo, Grant: input.LocationGrant, Sites: input.LocationSites, Lookup: input.LocationLookup, Usage: input.LocationUsage, Now: now}
	return &integrate2Location{LocationService: service, Routes: routes}, &ChatmapPictureCache{Locations: service, Renderer: chat.SchematicMap{}, Usage: input.LocationUsage}
}
