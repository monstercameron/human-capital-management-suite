package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const LocationReference ReferenceKind = "LOCATION"
const LocationVersion = 1
const LocationClassification = "SENSITIVE_PERSONAL_DATA"

var ErrLocationUnavailable = errors.New("location capability unavailable")

type LocationSource string

const (
	LocationDevice      LocationSource = "device"
	LocationAddress     LocationSource = "typed_address"
	LocationPin         LocationSource = "picked_on_map"
	LocationJobSite     LocationSource = "job_site"
	LocationAgent       LocationSource = "agent"
	LocationIntegration LocationSource = "integration"
)

type LocationPosition struct{ Latitude, Longitude, Accuracy float64 }
type LocationPlace struct {
	Position               *LocationPosition
	Source                 LocationSource
	Label, Address, SiteID string
	CapturedAt             time.Time
	Precision              string
	ApproximateRadius      float64
}
type LocationShare struct {
	Version                                                        int
	ID, TenantID, ConversationID, PostID, SharerID, SharerTenantID string
	PostRevision                                                   uint64
	SharedAt                                                       time.Time
	Classification                                                 string
	SharerKind                                                     string
	Place                                                          LocationPlace
	ExpiresAt                                                      *time.Time
	Ended                                                          bool
}
type LocationKey struct{ TenantID, ConversationID, PostID, ID string }
type AttachLocationRequest struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
	PostRevision                     uint64
	Place                            LocationPlace
	ExpiresAt                        *time.Time
	// Machine callers need a verified skill grant, never a JSON request flag.
}
type LocationGrant interface {
	AllowsLocation(context.Context, Principal, string, string) bool
}
type LocationRepository interface {
	AttachLocation(context.Context, LocationShare) (LocationShare, error)
	ReadLocation(context.Context, LocationKey, time.Time) (LocationShare, error)
	EndLocation(context.Context, LocationKey) error
	SweepLocations(context.Context, string, time.Time) (int64, error)
}
type LocationSearchPort interface {
	SearchLocations(context.Context, Principal, *LocationService, string, string, string, time.Time) ([]LocationShare, error)
}

func (s *LocationService) SharingNow(ctx context.Context, p Principal, tenant, conversation string) ([]LocationShare, error) {
	if s == nil || s.Repo == nil {
		return nil, ErrLocationUnavailable
	}
	search, ok := s.Repo.(LocationSearchPort)
	if !ok {
		return nil, ErrLocationUnavailable
	}
	all, err := search.SearchLocations(ctx, p, s, tenant, conversation, "", s.now())
	if err != nil {
		return nil, err
	}
	out := []LocationShare{}
	for _, v := range all {
		if !v.Ended && v.SharerID == p.SubjectID && v.SharerTenantID == p.TenantID {
			out = append(out, v)
		}
	}
	return out, nil
}

type LocationSite struct {
	ID, Label, Address string
	Position           LocationPosition
}
type LocationSitePort interface {
	ReadLocationSite(context.Context, Principal, string) (LocationSite, error)
	SearchLocationSites(context.Context, Principal, string) ([]LocationSite, error)
}

// SharedLocationSiteResolver is used only after the parent message read has
// established its audience. Project assignment is required to select a site,
// while reading an intentionally shared site follows the message audience.
type SharedLocationSiteResolver interface {
	ResolveSharedLocationSite(context.Context, string, string) (LocationSite, error)
}
type AddressLookup interface {
	LookupAddress(context.Context, Principal, string) ([]LocationPlace, error)
}
type LocationUsagePort interface {
	RecordLocationUsage(context.Context, string, string, string, string) error
}
type NoopLocationUsage struct{}

func (NoopLocationUsage) RecordLocationUsage(context.Context, string, string, string, string) error {
	return nil
}

type UnavailableAddressLookup struct{}

func (UnavailableAddressLookup) LookupAddress(context.Context, Principal, string) ([]LocationPlace, error) {
	return nil, ErrLocationUnavailable
}

type FixtureAddressLookup struct{ Places []LocationPlace }

func (f FixtureAddressLookup) LookupAddress(_ context.Context, _ Principal, query string) ([]LocationPlace, error) {
	out := []LocationPlace{}
	for _, p := range f.Places {
		if strings.Contains(strings.ToLower(p.Address+" "+p.Label), strings.ToLower(query)) {
			out = append(out, p)
		}
	}
	return out, nil
}

type FixtureLocationSites struct {
	Sites          []LocationSite
	AllowedSubject string
	AllowedTenant  string
}

func (f FixtureLocationSites) SearchLocationSites(_ context.Context, p Principal, query string) ([]LocationSite, error) {
	if p.SubjectID != f.AllowedSubject || p.TenantID != f.AllowedTenant {
		return nil, ErrPermissionDenied
	}
	out := []LocationSite{}
	for _, s := range f.Sites {
		if strings.Contains(strings.ToLower(s.Label+" "+s.Address), strings.ToLower(query)) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f FixtureLocationSites) ResolveSharedLocationSite(_ context.Context, tenant, id string) (LocationSite, error) {
	if tenant != f.AllowedTenant {
		return LocationSite{}, ErrPermissionDenied
	}
	for _, site := range f.Sites {
		if site.ID == id {
			return site, nil
		}
	}
	return LocationSite{}, ErrNotFound
}
func (f FixtureLocationSites) ReadLocationSite(ctx context.Context, p Principal, id string) (LocationSite, error) {
	sites, err := f.SearchLocationSites(ctx, p, "")
	if err != nil {
		return LocationSite{}, err
	}
	for _, s := range sites {
		if s.ID == id {
			return s, nil
		}
	}
	return LocationSite{}, ErrNotFound
}

type LocationService struct {
	Chat   AuthorizedReferenceReader
	Repo   LocationRepository
	Sites  LocationSitePort
	Grant  LocationGrant
	Lookup AddressLookup
	Usage  LocationUsagePort
	Now    Clock
}

func (s *LocationService) SearchLocationSites(ctx context.Context, p Principal, tenant, conversation, query string) ([]LocationSite, error) {
	if s == nil || s.Chat == nil || s.Sites == nil {
		return nil, ErrLocationUnavailable
	}
	if len(query) > 200 {
		return nil, ErrInvalidArgument
	}
	if err := s.checkActor(ctx, p, tenant, conversation); err != nil {
		return nil, err
	}
	c, _, err := s.Chat.ReadAuthorizedReference(ctx, p, tenant, conversation, "")
	if err != nil || c.TenantID != tenant || c.ID != conversation {
		return nil, ErrPermissionDenied
	}
	return s.Sites.SearchLocationSites(ctx, p, query)
}
func (s *LocationService) LookupAddress(ctx context.Context, p Principal, tenant, conversation, query string) ([]LocationPlace, error) {
	if s == nil || s.Chat == nil || s.Lookup == nil {
		return nil, ErrLocationUnavailable
	}
	if len(query) > 1000 {
		return nil, ErrInvalidArgument
	}
	if err := s.checkActor(ctx, p, tenant, conversation); err != nil {
		return nil, err
	}
	c, _, err := s.Chat.ReadAuthorizedReference(ctx, p, tenant, conversation, "")
	if err != nil || c.TenantID != tenant || c.ID != conversation {
		return nil, ErrPermissionDenied
	}
	usage := s.Usage
	if usage == nil {
		usage = NoopLocationUsage{}
	}
	sum := sha256.Sum256([]byte(tenant + "\x00" + query))
	if err := usage.RecordLocationUsage(ctx, p.TenantID, p.SubjectID, "address.lookup", hex.EncodeToString(sum[:])); err != nil {
		return nil, ErrLocationUnavailable
	}
	return s.Lookup.LookupAddress(ctx, p, query)
}

func (s *LocationService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *LocationService) authorize(ctx context.Context, p Principal, k LocationKey) (*Post, error) {
	if s == nil || s.Chat == nil || s.Repo == nil {
		return nil, ErrUnavailable
	}
	if p.SubjectID == "" || p.TenantID == "" || k.TenantID == "" || k.ConversationID == "" || k.PostID == "" {
		return nil, ErrInvalidArgument
	}
	if err := s.checkActor(ctx, p, k.TenantID, k.ConversationID); err != nil {
		return nil, err
	}
	_, post, err := s.Chat.ReadAuthorizedReference(ctx, p, k.TenantID, k.ConversationID, k.PostID)
	if err != nil || post == nil || post.TenantID != k.TenantID || post.ConversationID != k.ConversationID || post.ID != k.PostID || post.Deleted {
		return nil, ErrNotFound
	}
	return post, nil
}
func (s *LocationService) checkActor(ctx context.Context, p Principal, tenant, conversation string) error {
	if p.SubjectID == "" || p.TenantID == "" || tenant == "" || conversation == "" {
		return ErrInvalidArgument
	}
	if actor, ok := trust.FromContext(ctx); ok && actor != nil {
		if actor.Subject() != p.SubjectID || actor.Tenant().String() != p.TenantID || !s.now().Before(actor.ExpiresAt()) {
			return ErrPermissionDenied
		}
		if actor.SubjectKind() != trust.SubjectKindHuman && (s.Grant == nil || !s.Grant.AllowsLocation(ctx, p, tenant, conversation)) {
			return ErrPermissionDenied
		}
	}
	return nil
}
func (s *LocationService) Attach(ctx context.Context, r AttachLocationRequest) (LocationShare, error) {
	k := LocationKey{TenantID: r.TenantID, ConversationID: r.ConversationID, PostID: r.PostID}
	post, err := s.authorize(ctx, r.Principal, k)
	if err != nil {
		return LocationShare{}, err
	}
	if post.AuthorID != r.Principal.SubjectID || (post.AuthorHomeTenantID != "" && post.AuthorHomeTenantID != r.Principal.TenantID) {
		return LocationShare{}, ErrPermissionDenied
	}
	if r.PostRevision == 0 {
		return LocationShare{}, ErrConflict
	}
	now := s.now()
	if r.ExpiresAt != nil {
		// PostgreSQL timestamps have microsecond precision; normalize retries too.
		at := r.ExpiresAt.UTC().Truncate(time.Microsecond)
		r.ExpiresAt = &at
	}
	if r.ExpiresAt != nil && (!r.ExpiresAt.After(now) || r.ExpiresAt.Sub(now) > 24*time.Hour) {
		return LocationShare{}, ErrInvalidArgument
	}
	rawPlace := r.Place
	kind := "human"
	if actor, ok := trust.FromContext(ctx); ok && actor != nil {
		kind = actor.SubjectKind().String()
		switch actor.SubjectKind() {
		case trust.SubjectKindAgent:
			if rawPlace.Source != LocationJobSite {
				rawPlace.Source = LocationAgent
			}
		case trust.SubjectKindIntegration, trust.SubjectKindService:
			if rawPlace.Source != LocationJobSite {
				rawPlace.Source = LocationIntegration
			}
		default:
			if rawPlace.Source == LocationAgent || rawPlace.Source == LocationIntegration {
				return LocationShare{}, ErrPermissionDenied
			}
		}
	}
	place, err := PrepareLocation(rawPlace, now)
	if err != nil {
		return LocationShare{}, err
	}
	if place.Source == LocationJobSite {
		if s.Sites == nil {
			return LocationShare{}, ErrLocationUnavailable
		}
		if _, err = s.Sites.ReadLocationSite(ctx, r.Principal, place.SiteID); err != nil {
			return LocationShare{}, err
		}
		place.Position = nil
		place.Label = ""
		place.Address = ""
	}
	share := LocationShare{Version: LocationVersion, ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(r.TenantID+"\x00"+r.ConversationID+"\x00"+r.PostID)).String(), TenantID: r.TenantID, ConversationID: r.ConversationID, PostID: r.PostID, PostRevision: r.PostRevision, SharerID: r.Principal.SubjectID, SharerTenantID: r.Principal.TenantID, Place: place, ExpiresAt: r.ExpiresAt, SharedAt: now, Classification: LocationClassification}
	share.SharerKind = kind
	for _, ref := range post.References {
		if ref.Kind == LocationReference {
			k.ID = ref.ID
			prior, e := s.Read(ctx, r.Principal, k)
			if e != nil {
				return LocationShare{}, e
			}
			sameExpiry := prior.ExpiresAt == nil && r.ExpiresAt == nil
			if prior.ExpiresAt != nil && r.ExpiresAt != nil {
				sameExpiry = prior.ExpiresAt.Equal(*r.ExpiresAt)
			}
			canonical := prior.Place
			if canonical.Source == LocationJobSite {
				canonical.Position = nil
				canonical.Label = ""
				canonical.Address = ""
			}
			if prior.Ended || !reflect.DeepEqual(canonical, place) || !sameExpiry {
				return LocationShare{}, ErrConflict
			}
			return prior, nil
		}
	}
	if post.Revision != r.PostRevision {
		return LocationShare{}, ErrConflict
	}
	return s.Repo.AttachLocation(ctx, share)
}
func (s *LocationService) Read(ctx context.Context, p Principal, k LocationKey) (LocationShare, error) {
	post, err := s.authorize(ctx, p, k)
	if err != nil {
		return LocationShare{}, err
	}
	v, err := s.Repo.ReadLocation(ctx, k, s.now())
	if err != nil {
		return LocationShare{}, err
	}
	if v.TenantID != k.TenantID || v.ConversationID != k.ConversationID || v.PostID != k.PostID || v.ID != k.ID {
		return LocationShare{}, ErrNotFound
	}
	v.Classification = LocationClassification
	found := false
	for _, ref := range post.References {
		if ref.Kind == LocationReference && ref.ID == v.ID && ref.TenantID == v.TenantID {
			found = true
		}
	}
	if !found {
		return LocationShare{}, ErrNotFound
	}
	if v.Ended {
		v.Place = LocationPlace{}
		return v, nil
	}
	if v.ExpiresAt != nil && !s.now().Before(*v.ExpiresAt) {
		if err = s.Repo.EndLocation(ctx, k); err != nil {
			return LocationShare{}, ErrUnavailable
		}
		v.Ended = true
		v.Place = LocationPlace{}
		return v, nil
	}
	if !v.Ended && v.Place.Source == LocationJobSite {
		if s.Sites == nil {
			return LocationShare{}, ErrLocationUnavailable
		}
		resolver, ok := s.Sites.(SharedLocationSiteResolver)
		if !ok {
			return LocationShare{}, ErrLocationUnavailable
		}
		site, e := resolver.ResolveSharedLocationSite(ctx, v.SharerTenantID, v.Place.SiteID)
		if e != nil {
			return LocationShare{}, e
		}
		v.Place.Position = &site.Position
		v.Place.Label = site.Label
		v.Place.Address = site.Address
		v.Place, e = PrepareLocation(v.Place, s.now())
		if e != nil {
			return LocationShare{}, e
		}
	}
	return v, nil
}

// MessageLocations resolves canonical message references without requiring a
// generated transport enum to carry the location payload.
func (s *LocationService) MessageLocations(ctx context.Context, p Principal, tenant, conversation, postID string) ([]LocationShare, error) {
	k := LocationKey{TenantID: tenant, ConversationID: conversation, PostID: postID}
	post, err := s.authorize(ctx, p, k)
	if err != nil {
		return nil, err
	}
	out := []LocationShare{}
	for _, ref := range post.References {
		if ref.Kind != LocationReference {
			continue
		}
		k.ID = ref.ID
		v, err := s.Read(ctx, p, k)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *LocationService) End(ctx context.Context, p Principal, k LocationKey) error {
	v, err := s.Read(ctx, p, k)
	if err != nil {
		return err
	}
	if v.SharerID != p.SubjectID || v.SharerTenantID != p.TenantID {
		return ErrPermissionDenied
	}
	return s.Repo.EndLocation(ctx, k)
}

// PrepareLocation discards the precise input before it can reach persistence.
func PrepareLocation(p LocationPlace, now time.Time) (LocationPlace, error) {
	p.CapturedAt = p.CapturedAt.UTC()
	if len(p.Label) > 300 || len(p.Address) > 1000 || p.CapturedAt.IsZero() {
		return LocationPlace{}, ErrInvalidArgument
	}
	switch p.Source {
	case LocationDevice, LocationAddress, LocationPin, LocationJobSite, LocationAgent, LocationIntegration:
	default:
		return LocationPlace{}, ErrInvalidArgument
	}
	if p.Source == LocationDevice && (p.CapturedAt.Before(now.Add(-5*time.Minute)) || p.CapturedAt.After(now.Add(time.Minute))) {
		return LocationPlace{}, ErrInvalidArgument
	}
	if p.Precision != "exact" && p.Precision != "approximate" {
		return LocationPlace{}, ErrInvalidArgument
	}
	if p.Source == LocationJobSite && p.SiteID != "" && p.Position == nil {
		if p.Precision != "exact" {
			return LocationPlace{}, ErrInvalidArgument
		}
		return p, nil
	}
	if p.Position == nil {
		return LocationPlace{}, ErrInvalidArgument
	}
	q := *p.Position
	for _, x := range []float64{q.Latitude, q.Longitude, q.Accuracy, p.ApproximateRadius} {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return LocationPlace{}, ErrInvalidArgument
		}
	}
	if q.Latitude < -90 || q.Latitude > 90 || q.Longitude < -180 || q.Longitude > 180 || q.Accuracy < 1 || q.Accuracy > 100000 {
		return LocationPlace{}, ErrInvalidArgument
	}
	if p.Precision == "approximate" {
		if p.ApproximateRadius < 100 || p.ApproximateRadius > 100000 {
			return LocationPlace{}, ErrInvalidArgument
		}
		// Latitude and longitude use a fixed world grid; never persist a cell offset.
		step := p.ApproximateRadius / 111320
		q.Latitude = math.Max(-90, math.Min(90, math.Round(q.Latitude/step)*step))
		q.Longitude = math.Max(-180, math.Min(180, math.Round(q.Longitude/step)*step))
		q.Accuracy = math.Max(q.Accuracy, p.ApproximateRadius)
		// An exact address or site identifier could refine a coarse share.
		p.Label = ""
		p.Address = ""
		p.SiteID = ""
	}
	if p.Source == LocationJobSite && p.SiteID == "" {
		return LocationPlace{}, ErrInvalidArgument
	}
	p.Position = &q
	return p, nil
}

// LocationAccuracy includes the uncertainty added by sharing a coarsened pin.
func LocationAccuracy(p LocationPlace) float64 {
	if p.Position == nil {
		return 0
	}
	if p.Precision == "approximate" {
		return p.Position.Accuracy + p.ApproximateRadius
	}
	return p.Position.Accuracy
}
func (s *Service) validateLocationReference(ctx context.Context, p Principal, tenant, conversation string, ref Reference) error {
	if ref.TenantID != tenant || ref.ConversationID != conversation {
		return ErrPermissionDenied
	}
	repo, ok := s.store.(interface {
		LocationReferenceExists(context.Context, Principal, string, string, string) error
	})
	if !ok {
		return ErrUnavailable
	}
	return repo.LocationReferenceExists(ctx, p, tenant, conversation, ref.ID)
}
