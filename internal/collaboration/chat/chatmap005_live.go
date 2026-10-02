package chat

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Live location sharing (CHATMAP-005) and its governance (CHATMAP-006).
//
// Only the latest position of a live share is ever stored: an update replaces
// the position in place, so no trail exists to read, export or hand to an
// administrator. Expiry is judged by the server's clock, never the device's.

const (
	// LocationHardMaxDuration is the longest any share may last, whatever an
	// administrator configures: it is the workspace's stated maximum.
	LocationHardMaxDuration = 24 * time.Hour
	// LiveDefaultInterval is how often a device sends while the product is open.
	LiveDefaultInterval = 30 * time.Second

	// LocationDLPClass is the data-loss class a location maps onto. The
	// vocabulary is closed, so the strictest class is used: exports, the
	// outbound verifier and agents treat a location as special-category data.
	LocationDLPClass = "SPECIAL_CATEGORY"

	LiveEndedStopped = "stopped"
	LiveEndedExpired = "expired"
	LiveEndedLeft    = "left"
	LiveEndedSignOut = "signed_out"
	LiveEndedPolicy  = "policy"
)

// LocationPolicy is what an administrator decides for a workspace or a channel.
type LocationPolicy struct {
	SharingEnabled bool
	LiveEnabled    bool
	ExactAllowed   bool
	// MaxLive bounds a live share; MaxRetention bounds a share kept with its
	// message. Neither can exceed LocationHardMaxDuration.
	MaxLive      time.Duration
	MaxRetention time.Duration
}

// DefaultLocationPolicy is the starting point: everything on, one day at most,
// live sharing for up to eight hours.
func DefaultLocationPolicy() LocationPolicy {
	return LocationPolicy{SharingEnabled: true, LiveEnabled: true, ExactAllowed: true, MaxLive: 8 * time.Hour, MaxRetention: LocationHardMaxDuration}
}

// Valid reports whether an administrator's choice is inside the stated limits.
func (p LocationPolicy) Valid() bool {
	ok := func(d time.Duration) bool { return d >= time.Minute && d <= LocationHardMaxDuration }
	return ok(p.MaxLive) && ok(p.MaxRetention)
}

// Narrow combines a workspace policy with a channel's: a channel can switch
// things off and shorten limits, never widen what the workspace allows.
func (p LocationPolicy) Narrow(o LocationPolicy) LocationPolicy {
	out := LocationPolicy{SharingEnabled: p.SharingEnabled && o.SharingEnabled, LiveEnabled: p.LiveEnabled && o.LiveEnabled, ExactAllowed: p.ExactAllowed && o.ExactAllowed, MaxLive: p.MaxLive, MaxRetention: p.MaxRetention}
	if o.MaxLive < out.MaxLive {
		out.MaxLive = o.MaxLive
	}
	if o.MaxRetention < out.MaxRetention {
		out.MaxRetention = o.MaxRetention
	}
	return out
}

// LocationJurisdiction records whether sharing is on for a country or legal
// entity and the basis for that, so it can stay off where an agreement with
// employee representatives is required and not yet made.
type LocationJurisdiction struct {
	Country string
	Enabled bool
	Basis   string
}

type LocationPolicyRepository interface {
	ReadLocationPolicy(ctx context.Context, tenant, conversation string) (workspace LocationPolicy, channel *LocationPolicy, err error)
	WriteLocationPolicy(ctx context.Context, tenant, conversation string, p LocationPolicy, by string) error
	ReadLocationJurisdiction(ctx context.Context, tenant, country string) (LocationJurisdiction, bool, error)
	WriteLocationJurisdiction(ctx context.Context, tenant string, j LocationJurisdiction, by string) error
}

// LocationAdminPort says who may change the settings. An empty conversation
// asks about the workspace. Without a port nobody may.
type LocationAdminPort interface {
	IsLocationAdmin(ctx context.Context, p Principal, tenant, conversation string) bool
}

// LocationCountryResolver names the country or legal entity a person works under.
type LocationCountryResolver interface {
	LocationCountry(ctx context.Context, p Principal) string
}

// LiveLocationRepository holds the live-only storage operations.
type LiveLocationRepository interface {
	// UpdateLivePosition replaces the stored position of a share that is still
	// live at now. A share that has ended or passed its time reports ErrNotFound.
	UpdateLivePosition(ctx context.Context, k LocationKey, place LocationPlace, now time.Time) error
	EndLocationReason(ctx context.Context, k LocationKey, reason string) error
	EndLiveBySharer(ctx context.Context, tenant, conversation, subject, subjectTenant, reason string) (int64, error)
	// SharerLocationKeys lists the shares one person has made. The subject is
	// always the caller's own principal.
	SharerLocationKeys(ctx context.Context, tenant, subject, subjectTenant string) ([]LocationKey, error)
}

// LiveLocationService is the live and governance surface. Nothing here accepts
// a person to look up: the subject is always the caller.
type LiveLocationService struct{ Locations *LocationService }

func (s *LocationService) liveRepo() (LiveLocationRepository, bool) {
	if s == nil || s.Repo == nil {
		return nil, false
	}
	r, ok := s.Repo.(LiveLocationRepository)
	return r, ok
}

func (s *LocationService) adminPort() LocationAdminPort {
	if s.Admin != nil {
		return s.Admin
	}
	if s.Repo != nil {
		if a, ok := s.Repo.(LocationAdminPort); ok {
			return a
		}
	}
	return nil
}

// effectivePolicy resolves the workspace and channel settings and the person's
// jurisdiction. A repository without policy storage leaves the defaults.
func (s *LocationService) effectivePolicy(ctx context.Context, p Principal, tenant, conversation string) (LocationPolicy, error) {
	repo, ok := s.Repo.(LocationPolicyRepository)
	if !ok {
		// No policy storage: nothing is configured, so nothing is shortened.
		pol := DefaultLocationPolicy()
		pol.MaxRetention = 0
		return pol, nil
	}
	ws, channel, err := repo.ReadLocationPolicy(ctx, tenant, conversation)
	if err != nil {
		return LocationPolicy{}, err
	}
	pol := ws
	if channel != nil {
		pol = ws.Narrow(*channel)
	}
	country := ""
	if s.Country != nil {
		country = strings.ToUpper(strings.TrimSpace(s.Country.LocationCountry(ctx, p)))
	}
	for _, key := range []string{country, "*"} {
		if key == "" {
			continue
		}
		j, found, err := repo.ReadLocationJurisdiction(ctx, tenant, key)
		if err != nil {
			return LocationPolicy{}, err
		}
		if found {
			if !j.Enabled {
				pol.SharingEnabled = false
			}
			break
		}
	}
	return pol, nil
}

// enforcePolicy applies the settings to a new share before anything is stored.
// It returns whether the expiry was shortened to the retention limit.
func (s *LocationService) enforcePolicy(ctx context.Context, r *AttachLocationRequest, now time.Time) (clamped bool, err error) {
	if r.Live && r.Place.Source != LocationDevice {
		// A live share follows a device; no other source can be live.
		return false, ErrInvalidArgument
	}
	switch r.Place.Source {
	case LocationDevice, LocationAddress, LocationPin:
	default:
		return false, nil
	}
	pol, err := s.effectivePolicy(ctx, r.Principal, r.TenantID, r.ConversationID)
	if err != nil {
		return false, ErrLocationUnavailable
	}
	if !pol.SharingEnabled {
		return false, ErrPermissionDenied
	}
	if !pol.ExactAllowed && r.Place.Precision == "exact" {
		return false, ErrPermissionDenied
	}
	if r.Live {
		if !pol.LiveEnabled {
			return false, ErrPermissionDenied
		}
		if r.Place.Source != LocationDevice || r.ExpiresAt == nil || r.ExpiresAt.Sub(now) > pol.MaxLive {
			return false, ErrInvalidArgument
		}
		if r.LiveInterval == 0 {
			r.LiveInterval = LiveDefaultInterval
		}
		if r.LiveInterval < 5*time.Second || r.LiveInterval > 5*time.Minute {
			return false, ErrInvalidArgument
		}
		return false, nil
	}
	if pol.MaxRetention > 0 {
		limit := now.Add(pol.MaxRetention).Truncate(time.Microsecond)
		if r.ExpiresAt == nil || r.ExpiresAt.After(limit) {
			r.ExpiresAt = &limit
			return true, nil
		}
	}
	return false, nil
}

// markPaused flags a live share whose device has gone quiet. The device sends
// at the share's interval only while the product is open, so three missed
// intervals mean it is closed.
func markPaused(v *LocationShare, now time.Time) {
	if !v.Live || v.Ended || v.PositionAt == nil {
		return
	}
	wait := 3 * v.LiveInterval
	if wait < time.Minute {
		wait = time.Minute
	}
	v.Paused = now.Sub(*v.PositionAt) > wait
}

// Update replaces the sharer's latest position. The place's precision is the
// share's own: a device cannot make a coarse share exact, and the server
// coarsens again before storing.
func (s LiveLocationService) Update(ctx context.Context, p Principal, k LocationKey, place LocationPlace) (LocationShare, error) {
	l := s.Locations
	repo, ok := l.liveRepo()
	if !ok {
		return LocationShare{}, ErrLocationUnavailable
	}
	v, err := l.Read(ctx, p, k)
	if err != nil {
		return LocationShare{}, err
	}
	if v.SharerID != p.SubjectID || v.SharerTenantID != p.TenantID {
		return LocationShare{}, ErrPermissionDenied
	}
	if v.Ended {
		return LocationShare{}, ErrNotFound
	}
	if !v.Live || place.Source != LocationDevice {
		return LocationShare{}, ErrInvalidArgument
	}
	now := l.now()
	pol, err := l.effectivePolicy(ctx, p, k.TenantID, k.ConversationID)
	if err != nil {
		return LocationShare{}, ErrLocationUnavailable
	}
	if !pol.SharingEnabled || !pol.LiveEnabled {
		_ = repo.EndLocationReason(ctx, k, LiveEndedPolicy)
		return LocationShare{}, ErrPermissionDenied
	}
	// Updates arriving faster than half the interval are not stored.
	if v.PositionAt != nil && v.LiveInterval > 0 && now.Sub(*v.PositionAt) < v.LiveInterval/2 {
		return v, nil
	}
	place.Precision = v.Place.Precision
	place.ApproximateRadius = v.Place.ApproximateRadius
	place.Label, place.Address, place.SiteID = v.Place.Label, v.Place.Address, ""
	if place.Precision == "approximate" {
		place.Label, place.Address = "", ""
	}
	prepared, err := PrepareLocation(place, now)
	if err != nil {
		return LocationShare{}, err
	}
	if err = repo.UpdateLivePosition(ctx, k, prepared, now); err != nil {
		return LocationShare{}, err
	}
	v.Place = prepared
	v.PositionAt = &now
	v.Paused = false
	return v, nil
}

// Stop ends one of the caller's own shares now.
func (s LiveLocationService) Stop(ctx context.Context, p Principal, k LocationKey) error {
	l := s.Locations
	repo, ok := l.liveRepo()
	if !ok {
		return l.End(ctx, p, k)
	}
	v, err := l.Read(ctx, p, k)
	if err != nil {
		return err
	}
	if v.SharerID != p.SubjectID || v.SharerTenantID != p.TenantID {
		return ErrPermissionDenied
	}
	return repo.EndLocationReason(ctx, k, LiveEndedStopped)
}

// EndMine ends the caller's live shares in one conversation, or in all of them
// when conversation is empty. Sign-out and leaving a conversation use it.
func (s LiveLocationService) EndMine(ctx context.Context, p Principal, conversation, reason string) (int64, error) {
	repo, ok := s.Locations.liveRepo()
	if !ok || p.SubjectID == "" || p.TenantID == "" {
		return 0, ErrLocationUnavailable
	}
	if reason != LiveEndedSignOut && reason != LiveEndedLeft && reason != LiveEndedStopped {
		return 0, ErrInvalidArgument
	}
	return repo.EndLiveBySharer(ctx, p.TenantID, conversation, p.SubjectID, p.TenantID, reason)
}

// MyShares lists every location the caller has shared that still exists, so
// each can be ended. It is by construction the caller's own: there is no
// parameter that names another person.
func (s LiveLocationService) MyShares(ctx context.Context, p Principal) ([]LocationShare, error) {
	repo, ok := s.Locations.liveRepo()
	if !ok || p.SubjectID == "" || p.TenantID == "" {
		return nil, ErrLocationUnavailable
	}
	keys, err := repo.SharerLocationKeys(ctx, p.TenantID, p.SubjectID, p.TenantID)
	if err != nil {
		return nil, err
	}
	out := []LocationShare{}
	for _, k := range keys {
		v, err := s.Locations.Read(ctx, p, k)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPermissionDenied) {
				continue
			}
			return nil, err
		}
		if !v.Ended && v.SharerID == p.SubjectID && v.SharerTenantID == p.TenantID {
			out = append(out, v)
		}
	}
	return out, nil
}

// LiveMapView is the channel's map: who is sharing live to it now, and the
// channel's job sites.
type LiveMapView struct {
	Shares []LocationShare
	Sites  []LocationSite
}

// LiveMap lists the live shares started to one conversation. A static point
// never appears here, and a share that has ended has no position to show.
func (s LiveLocationService) LiveMap(ctx context.Context, p Principal, tenant, conversation string) (LiveMapView, error) {
	l := s.Locations
	if l == nil || l.Repo == nil {
		return LiveMapView{}, ErrLocationUnavailable
	}
	search, ok := l.Repo.(LocationSearchPort)
	if !ok {
		return LiveMapView{}, ErrLocationUnavailable
	}
	all, err := search.SearchLocations(ctx, p, l, tenant, conversation, "", l.now())
	if err != nil {
		return LiveMapView{}, err
	}
	view := LiveMapView{Shares: []LocationShare{}, Sites: []LocationSite{}}
	for _, v := range all {
		if v.Live && !v.Ended && v.Place.Position != nil {
			view.Shares = append(view.Shares, v)
		}
	}
	if l.Sites != nil {
		if sites, err := l.Sites.SearchLocationSites(ctx, p, ""); err == nil {
			view.Sites = sites
		}
	}
	return view, nil
}

// Policy returns the settings that apply to the caller in a conversation.
func (s LiveLocationService) Policy(ctx context.Context, p Principal, tenant, conversation string) (LocationPolicy, error) {
	l := s.Locations
	if l == nil || l.Chat == nil {
		return LocationPolicy{}, ErrLocationUnavailable
	}
	if err := l.checkActor(ctx, p, tenant, conversation); err != nil {
		return LocationPolicy{}, err
	}
	if _, _, err := l.Chat.ReadAuthorizedReference(ctx, p, tenant, conversation, ""); err != nil {
		return LocationPolicy{}, ErrPermissionDenied
	}
	return l.effectivePolicy(ctx, p, tenant, conversation)
}

// CanAdminister tells the page whether to offer the settings form. It is only a
// hint for the page: SetPolicy checks again.
func (s LiveLocationService) CanAdminister(ctx context.Context, p Principal, tenant, conversation string) bool {
	if s.Locations == nil {
		return false
	}
	admin := s.Locations.adminPort()
	return admin != nil && p.TenantID == tenant && admin.IsLocationAdmin(ctx, p, tenant, conversation)
}

// SetPolicy stores an administrator's settings for the workspace (conversation
// empty) or one channel.
func (s LiveLocationService) SetPolicy(ctx context.Context, p Principal, tenant, conversation string, pol LocationPolicy) error {
	l := s.Locations
	if l == nil || l.Repo == nil {
		return ErrLocationUnavailable
	}
	repo, ok := l.Repo.(LocationPolicyRepository)
	if !ok {
		return ErrLocationUnavailable
	}
	scope := conversation
	if scope == "" {
		scope = "workspace"
	}
	if err := l.checkActor(ctx, p, tenant, scope); err != nil {
		return err
	}
	if !pol.Valid() {
		return ErrInvalidArgument
	}
	admin := l.adminPort()
	if admin == nil || p.TenantID != tenant || !admin.IsLocationAdmin(ctx, p, tenant, conversation) {
		return ErrPermissionDenied
	}
	return repo.WriteLocationPolicy(ctx, tenant, conversation, pol, p.SubjectID)
}

// SetJurisdiction records whether sharing is enabled for a country or legal
// entity and why.
func (s LiveLocationService) SetJurisdiction(ctx context.Context, p Principal, tenant string, j LocationJurisdiction) error {
	l := s.Locations
	if l == nil || l.Repo == nil {
		return ErrLocationUnavailable
	}
	repo, ok := l.Repo.(LocationPolicyRepository)
	if !ok {
		return ErrLocationUnavailable
	}
	j.Country = strings.ToUpper(strings.TrimSpace(j.Country))
	j.Basis = strings.TrimSpace(j.Basis)
	if j.Country == "" || len(j.Country) > 8 || j.Basis == "" || len(j.Basis) > 500 {
		return ErrInvalidArgument
	}
	admin := l.adminPort()
	if admin == nil || p.TenantID != tenant || !admin.IsLocationAdmin(ctx, p, tenant, "") {
		return ErrPermissionDenied
	}
	return repo.WriteLocationJurisdiction(ctx, tenant, j, p.SubjectID)
}
