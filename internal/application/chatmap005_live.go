package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The live and governance surface of location sharing. Each method is reached
// only through the HTTP overlay's optional interfaces, so a deployment whose
// repository lacks live storage answers "location unavailable" instead of
// pretending.

type chatmapPolicyWire struct {
	SharingEnabled, LiveEnabled, ExactAllowed bool
	MaxLiveSeconds, MaxRetentionSeconds       int
	// CanAdminister is set on answers so the page knows to offer the form.
	CanAdminister bool
}

func chatmapPolicyToWire(p chat.LocationPolicy) chatmapPolicyWire {
	return chatmapPolicyWire{SharingEnabled: p.SharingEnabled, LiveEnabled: p.LiveEnabled, ExactAllowed: p.ExactAllowed, MaxLiveSeconds: int(p.MaxLive / time.Second), MaxRetentionSeconds: int(p.MaxRetention / time.Second)}
}
func (w chatmapPolicyWire) policy() chat.LocationPolicy {
	return chat.LocationPolicy{SharingEnabled: w.SharingEnabled, LiveEnabled: w.LiveEnabled, ExactAllowed: w.ExactAllowed, MaxLive: time.Duration(w.MaxLiveSeconds) * time.Second, MaxRetention: time.Duration(w.MaxRetentionSeconds) * time.Second}
}

// live serves the routes added for live sharing and settings. The surface is
// asked for each capability, so a deployment without it reports unavailable.
func (h ChatmapHTTP) live(ctx context.Context, path string, p chat.Principal, k chat.LocationKey, body chatmapBody) (any, error) {
	unavailable := chat.ErrLocationUnavailable
	// The workspace settings page asks about the person's own workspace and
	// does not name a tenant; the service refuses any other.
	if k.TenantID == "" && (path == ChatmapPath+"/settings" || path == ChatmapPath+"/setjurisdiction" || (path == ChatmapPath+"/setpolicy" && k.ConversationID == "")) {
		k.TenantID = p.TenantID
	}
	switch path {
	case ChatmapPath + "/update":
		s, ok := h.Surface.(interface {
			UpdateLive(context.Context, chat.Principal, chat.LocationKey, chat.LocationPlace) (chat.LocationShare, error)
		})
		if !ok {
			return nil, unavailable
		}
		share, err := s.UpdateLive(ctx, p, k, body.Place)
		return share, err
	case ChatmapPath + "/mine":
		s, ok := h.Surface.(interface {
			MyShares(context.Context, chat.Principal) ([]chat.LocationShare, error)
		})
		if !ok {
			return nil, unavailable
		}
		return s.MyShares(ctx, p)
	case ChatmapPath + "/endmine":
		s, ok := h.Surface.(interface {
			EndMine(context.Context, chat.Principal, string, string) (int64, error)
		})
		if !ok {
			return nil, unavailable
		}
		n, err := s.EndMine(ctx, p, k.ConversationID, body.Reason)
		return struct{ Ended int64 }{n}, err
	case ChatmapPath + "/map":
		s, ok := h.Surface.(interface {
			LiveMap(context.Context, chat.Principal, string, string) (chat.LiveMapView, error)
		})
		if !ok {
			return nil, unavailable
		}
		return s.LiveMap(ctx, p, k.TenantID, k.ConversationID)
	case ChatmapPath + "/policy":
		s, ok := h.Surface.(interface {
			LocationPolicyFor(context.Context, chat.Principal, string, string) (chatmapPolicyWire, error)
		})
		if !ok {
			return nil, unavailable
		}
		return s.LocationPolicyFor(ctx, p, k.TenantID, k.ConversationID)
	case ChatmapPath + "/setpolicy":
		s, ok := h.Surface.(interface {
			SetLocationPolicy(context.Context, chat.Principal, string, string, chatmapPolicyWire) error
		})
		if !ok {
			return nil, unavailable
		}
		return struct{ Saved bool }{true}, s.SetLocationPolicy(ctx, p, k.TenantID, k.ConversationID, body.Policy)
	case ChatmapPath + "/setjurisdiction":
		s, ok := h.Surface.(interface {
			SetLocationJurisdiction(context.Context, chat.Principal, string, chat.LocationJurisdiction) error
		})
		if !ok {
			return nil, unavailable
		}
		return struct{ Saved bool }{true}, s.SetLocationJurisdiction(ctx, p, k.TenantID, chat.LocationJurisdiction{Country: chatmapNormalizeCountry(body.Country), Enabled: body.Enabled, Basis: body.Basis})
	case ChatmapPath + "/settings":
		s, ok := h.Surface.(interface {
			WorkspaceLocationSettings(context.Context, chat.Principal, string) (chatmapSettingsWire, error)
		})
		if !ok {
			return nil, unavailable
		}
		return s.WorkspaceLocationSettings(ctx, p, k.TenantID)
	}
	return nil, chat.ErrNotFound
}

func (s *integrate2Location) liveService() chat.LiveLocationService {
	return chat.LiveLocationService{Locations: s.LocationService}
}

func (s *integrate2Location) lease(ctx context.Context, tenant, conversation string) (context.Context, error) {
	if s.Routes == nil || s.LocationService == nil {
		return nil, chat.ErrLocationUnavailable
	}
	return s.Routes.ChatWriteContext(ctx, tenant, conversation)
}

func (s *integrate2Location) UpdateLive(ctx context.Context, p chat.Principal, k chat.LocationKey, place chat.LocationPlace) (chat.LocationShare, error) {
	leased, err := s.lease(ctx, k.TenantID, k.ConversationID)
	if err != nil {
		return chat.LocationShare{}, err
	}
	return s.liveService().Update(leased, p, k, place)
}

func (s *integrate2Location) MyShares(ctx context.Context, p chat.Principal) ([]chat.LocationShare, error) {
	if s.LocationService == nil {
		return nil, chat.ErrLocationUnavailable
	}
	return s.liveService().MyShares(ctx, p)
}

func (s *integrate2Location) EndMine(ctx context.Context, p chat.Principal, conversation, reason string) (int64, error) {
	if s.LocationService == nil {
		return 0, chat.ErrLocationUnavailable
	}
	return s.liveService().EndMine(ctx, p, conversation, reason)
}

func (s *integrate2Location) LiveMap(ctx context.Context, p chat.Principal, tenant, conversation string) (chat.LiveMapView, error) {
	if s.LocationService == nil {
		return chat.LiveMapView{}, chat.ErrLocationUnavailable
	}
	return s.liveService().LiveMap(ctx, p, tenant, conversation)
}

func (s *integrate2Location) LocationPolicyFor(ctx context.Context, p chat.Principal, tenant, conversation string) (chatmapPolicyWire, error) {
	if s.LocationService == nil {
		return chatmapPolicyWire{}, chat.ErrLocationUnavailable
	}
	live := s.liveService()
	pol, err := live.Policy(ctx, p, tenant, conversation)
	wire := chatmapPolicyToWire(pol)
	wire.CanAdminister = err == nil && live.CanAdminister(ctx, p, tenant, conversation)
	return wire, err
}

func (s *integrate2Location) SetLocationPolicy(ctx context.Context, p chat.Principal, tenant, conversation string, w chatmapPolicyWire) error {
	if s.LocationService == nil {
		return chat.ErrLocationUnavailable
	}
	return s.liveService().SetPolicy(ctx, p, tenant, conversation, w.policy())
}

// chatmapSettingsWire is the workspace settings page's one read.
type chatmapSettingsWire struct {
	Policy        chatmapPolicyWire
	Jurisdictions []chat.LocationJurisdiction
}

func (s *integrate2Location) WorkspaceLocationSettings(ctx context.Context, p chat.Principal, tenant string) (chatmapSettingsWire, error) {
	if s.LocationService == nil {
		return chatmapSettingsWire{}, chat.ErrLocationUnavailable
	}
	v, err := s.liveService().WorkspaceSettings(ctx, p, tenant)
	if err != nil {
		return chatmapSettingsWire{}, err
	}
	wire := chatmapSettingsWire{Policy: chatmapPolicyToWire(v.Policy), Jurisdictions: v.Jurisdictions}
	wire.Policy.CanAdminister = true
	return wire, nil
}

func (s *integrate2Location) SetLocationJurisdiction(ctx context.Context, p chat.Principal, tenant string, j chat.LocationJurisdiction) error {
	if s.LocationService == nil {
		return chat.ErrLocationUnavailable
	}
	return s.liveService().SetJurisdiction(ctx, p, tenant, j)
}
