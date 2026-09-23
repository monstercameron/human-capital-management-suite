// Package chatmedia owns the security boundary for recorded chat media.
// Bytes are quarantined, sniffed, scanned, and only then exposed through a
// short lived authorization that is checked on every read.
package chatmedia

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

type MediaType string

const (
	MediaMP3  MediaType = "audio/mpeg"
	MediaWAV  MediaType = "audio/wav"
	MediaBMP  MediaType = "image/bmp"
	MediaJPEG MediaType = "image/jpeg"
	MediaPNG  MediaType = "image/png"
	MediaGIF  MediaType = "image/gif"
	MediaMP4  MediaType = "video/mp4"
)

var (
	ErrInvalid            = errors.New("chatmedia: invalid request")
	ErrUnauthorized       = errors.New("chatmedia: unauthorized")
	ErrQuarantined        = errors.New("chatmedia: artifact is not admitted")
	ErrScannerUnavailable = errors.New("chatmedia: scanner unavailable")
	ErrUnsupported        = errors.New("chatmedia: unsupported or mismatched media")
	ErrRevoked            = errors.New("chatmedia: grant revoked")
	ErrRange              = errors.New("chatmedia: invalid range")
	ErrEmbedDenied        = errors.New("chatmedia: embed denied")
	ErrBusy               = errors.New("chatmedia: image processing busy")
)

type ArtifactState string

const (
	StateQuarantined ArtifactState = "QUARANTINED"
	StateAdmitted    ArtifactState = "ADMITTED"
	StateRejected    ArtifactState = "REJECTED"
)

type Reference struct {
	ArtifactID, TenantID, ConversationID string
	MediaType                            MediaType
	Size                                 int64
	State                                ArtifactState
	Transcript                           string
	AltText                              string
}
type UploadRequest struct {
	TenantID, ConversationID, PrincipalID, Filename, DeclaredType, EvidenceID string
	Content                                                                   []byte
	Transcript, AltText                                                       string
}
type Artifact struct {
	Reference
	Content                           []byte
	Renditions                        map[string]Rendition
	ScannerID, ScannerVersion, Reason string
}

// Rendition is a scanned image's smaller encoding. It inherits the source
// artifact's identity and grant; it is never authorized separately.
type Rendition struct {
	MediaType MediaType
	Content   []byte
	Width     int
	Height    int
}

const (
	VariantThumbnail = "thumbnail"
	VariantDisplay   = "display"
)

// Store is the durable seam. Implementations must keep quarantine bytes
// inaccessible to readers until SetVerdict(ADMITTED).
type Store interface {
	Quarantine(context.Context, Artifact) error
	SetRenditions(context.Context, string, string, map[string]Rendition) error
	SetVerdict(context.Context, string, string, ArtifactState, string, string) error
	Get(context.Context, string, string) (Artifact, error)
	GetRendition(context.Context, string, string, string) (Artifact, error)
}

// Scanner reuses the owned malware-inspection contract.
type Scanner interface {
	Scan(context.Context, string, io.Reader) (quarantine.Verdict, error)
}
type Authorizer func(context.Context, AccessRequest) error
type AccessRequest struct{ TenantID, ConversationID, PrincipalID, ArtifactID, Grant string }
type Grant struct {
	Token, ArtifactID, TenantID, ConversationID, PrincipalID string
	ExpiresAt                                                time.Time
	Revision                                                 uint64
}
type EmbedGrant struct {
	Token, TenantID, ConversationID, PrincipalID, Origin string
	ExpiresAt                                            time.Time
	Revision                                             uint64
}

type Service struct {
	store      Store
	scanner    Scanner
	authorize  Authorizer
	now        func() time.Time
	maxBytes   int64
	retention  time.Duration
	imageSlots chan struct{}
	mu         sync.RWMutex
	grants     map[string]Grant
	embeds     map[string]EmbedGrant
	revoked    map[string]revocation
}

// revocation is one artifact's revocation counter with the time it last moved.
// The counter is only consulted by a live grant, and a grant lives at most
// MaxGrantTTL, so an entry older than the retention window can be dropped.
type revocation struct {
	count uint64
	at    time.Time
}

type Config struct {
	Store     Store
	Scanner   Scanner
	Authorize Authorizer
	Now       func() time.Time
	MaxBytes  int64
	// Retention bounds how long an expired grant, embed grant or revocation
	// counter is kept in this process. It must exceed MaxGrantTTL; zero means
	// DefaultRetention.
	Retention time.Duration
}

// MaxGrantTTL is the longest life any issued grant can have, and
// DefaultRetention is the process-local bookkeeping window that outlives it.
// Together they make the grant, embed and revocation maps bounded by live
// traffic rather than by process uptime.
const (
	MaxGrantTTL                   = 15 * time.Minute
	DefaultRetention              = time.Hour
	MaxConcurrentImageDerivations = 4
)

func New(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 64 << 20
	}
	if cfg.Retention <= MaxGrantTTL {
		cfg.Retention = DefaultRetention
	}
	return &Service{store: cfg.Store, scanner: cfg.Scanner, authorize: cfg.Authorize, now: cfg.Now, maxBytes: cfg.MaxBytes, retention: cfg.Retention, imageSlots: make(chan struct{}, MaxConcurrentImageDerivations), grants: map[string]Grant{}, embeds: map[string]EmbedGrant{}, revoked: map[string]revocation{}}
}

// expireLocked drops grant, embed and revocation bookkeeping that can no longer
// affect a decision. It runs on every write to these maps, so the process-local
// state stays proportional to live media traffic.
func (s *Service) expireLocked(now time.Time) {
	cutoff := now.Add(-s.retention)
	for token, g := range s.grants {
		if !now.Before(g.ExpiresAt) {
			delete(s.grants, token)
		}
	}
	for token, g := range s.embeds {
		if !now.Before(g.ExpiresAt) {
			delete(s.embeds, token)
		}
	}
	for artifact, r := range s.revoked {
		if r.at.Before(cutoff) {
			delete(s.revoked, artifact)
		}
	}
}

// Retained reports how many grants, embed grants and revocation counters this
// process currently holds. It exists so the bound can be asserted.
func (s *Service) Retained() (grants, embeds, revocations int) {
	if s == nil {
		return 0, 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.grants), len(s.embeds), len(s.revoked)
}

func (s *Service) Upload(ctx context.Context, req UploadRequest) (Reference, error) {
	if s == nil || s.store == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ConversationID) == "" || strings.TrimSpace(req.PrincipalID) == "" || len(req.Content) == 0 {
		return Reference{}, ErrInvalid
	}
	if int64(len(req.Content)) > s.maxBytes {
		return Reference{}, fmt.Errorf("%w: size exceeds %d", ErrInvalid, s.maxBytes)
	}
	mt, ok := normalizeType(req.DeclaredType)
	if !ok {
		return Reference{}, ErrUnsupported
	}
	if sniff(req.Content) != mt {
		return Reference{}, ErrUnsupported
	}
	id := scopedArtifactID(req.TenantID, req.ConversationID, req.Content)
	if s.authorize == nil {
		return Reference{}, ErrUnauthorized
	}
	if err := s.authorize(ctx, AccessRequest{TenantID: req.TenantID, ConversationID: req.ConversationID, PrincipalID: req.PrincipalID, ArtifactID: id}); err != nil {
		return Reference{}, ErrUnauthorized
	}
	if mt == MediaPNG || mt == MediaJPEG || mt == MediaBMP {
		// Bound decoded image memory across all tenants. Refuse excess work
		// before storing or copying another full upload into quarantine.
		select {
		case s.imageSlots <- struct{}{}:
			defer func() { <-s.imageSlots }()
		default:
			return Reference{}, ErrBusy
		}
	}
	ref := Reference{ArtifactID: id, TenantID: req.TenantID, ConversationID: req.ConversationID, MediaType: mt, Size: int64(len(req.Content)), State: StateQuarantined, Transcript: req.Transcript, AltText: req.AltText}
	if err := s.store.Quarantine(ctx, Artifact{Reference: ref, Content: append([]byte(nil), req.Content...), Reason: "awaiting scanner verdict"}); err != nil {
		return Reference{}, err
	}
	if s.scanner == nil {
		_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, "scanner unavailable", "")
		return Reference{}, ErrScannerUnavailable
	}
	v, err := s.scanner.Scan(ctx, id, bytes.NewReader(req.Content))
	if err != nil {
		_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, err.Error(), "")
		return Reference{}, ErrScannerUnavailable
	}
	if !v.Safe {
		reason := v.Reason
		if reason == "" {
			reason = "scanner reported unsafe content"
		}
		_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, reason, "")
		return Reference{}, ErrQuarantined
	}
	if mt == MediaPNG || mt == MediaJPEG || mt == MediaBMP {
		renditions, deriveErr := imageRenditions(req.Content, mt)
		if deriveErr != nil {
			_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, "image decode or rendition failed", "")
			return Reference{}, deriveErr
		}
		if err := s.store.SetRenditions(ctx, id, req.TenantID, renditions); err != nil {
			_ = s.store.SetVerdict(ctx, id, req.TenantID, StateRejected, "image rendition failed", "")
			return Reference{}, err
		}
	}
	if err := s.store.SetVerdict(ctx, id, req.TenantID, StateAdmitted, "", ""); err != nil {
		return Reference{}, err
	}
	ref.State = StateAdmitted
	return ref, nil
}

func scopedArtifactID(tenant, conversation string, content []byte) string {
	h := sha256.New()
	for _, part := range []string{tenant, conversation} {
		_, _ = h.Write([]byte{byte(len(part) >> 8), byte(len(part))})
		_, _ = h.Write([]byte(part))
	}
	_, _ = h.Write(content)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Describe reports what an admitted artifact is, without opening its bytes and
// without a grant. It exists for the one caller that has to decide whether a
// post may reference an artifact: the chat service, at commit time. It discloses
// nothing a member of the conversation cannot already see, and it refuses an
// artifact from another tenant or conversation and one that is not admitted, so
// a quarantined or rejected upload can never be attached to a post.
func (s *Service) Describe(ctx context.Context, tenantID, conversationID, artifactID string) (Reference, error) {
	if s == nil || s.store == nil || tenantID == "" || conversationID == "" || artifactID == "" {
		return Reference{}, ErrInvalid
	}
	a, err := s.store.Get(ctx, artifactID, tenantID)
	if err != nil {
		return Reference{}, err
	}
	if a.TenantID != tenantID || a.ConversationID != conversationID {
		return Reference{}, ErrUnauthorized
	}
	if a.State != StateAdmitted {
		return Reference{}, ErrQuarantined
	}
	return a.Reference, nil
}

func (s *Service) Authorize(ctx context.Context, req AccessRequest, ttl time.Duration) (Grant, error) {
	if s == nil || s.store == nil || req.TenantID == "" || req.ConversationID == "" || req.PrincipalID == "" || req.ArtifactID == "" {
		return Grant{}, ErrInvalid
	}
	if s.authorize == nil || s.authorize(ctx, req) != nil {
		return Grant{}, ErrUnauthorized
	}
	a, err := s.store.Get(ctx, req.TenantID, req.ArtifactID)
	if err != nil {
		return Grant{}, err
	}
	if a.State != StateAdmitted || a.ConversationID != req.ConversationID {
		return Grant{}, ErrQuarantined
	}
	if ttl <= 0 || ttl > 15*time.Minute {
		ttl = 5 * time.Minute
	}
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return Grant{}, err
	}
	now := s.now()
	g := Grant{Token: hex.EncodeToString(b), ArtifactID: req.ArtifactID, TenantID: req.TenantID, ConversationID: req.ConversationID, PrincipalID: req.PrincipalID, ExpiresAt: now.Add(ttl), Revision: 1}
	s.mu.Lock()
	s.expireLocked(now)
	s.grants[g.Token] = g
	s.mu.Unlock()
	return g, nil
}
func (s *Service) Revoke(artifactID string) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(now)
	r := s.revoked[artifactID]
	r.count++
	r.at = now
	s.revoked[artifactID] = r
}

// AuthorizeEmbed issues a short-lived isolated embed grant for one approved
// public origin. It carries no HCM credentials or ambient browser capability.
func (s *Service) AuthorizeEmbed(ctx context.Context, req AccessRequest, rawURL string, allowlist []string, ttl time.Duration) (EmbedGrant, error) {
	if s == nil || req.TenantID == "" || req.ConversationID == "" || req.PrincipalID == "" || s.authorize == nil {
		return EmbedGrant{}, ErrEmbedDenied
	}
	if err := s.authorize(ctx, req); err != nil {
		return EmbedGrant{}, ErrEmbedDenied
	}
	u, err := ValidateEmbedURL(rawURL, allowlist)
	if err != nil {
		return EmbedGrant{}, err
	}
	if ttl <= 0 || ttl > 15*time.Minute {
		ttl = 5 * time.Minute
	}
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return EmbedGrant{}, err
	}
	now := s.now()
	g := EmbedGrant{Token: hex.EncodeToString(b), TenantID: req.TenantID, ConversationID: req.ConversationID, PrincipalID: req.PrincipalID, Origin: u.Scheme + "://" + u.Host, ExpiresAt: now.Add(ttl), Revision: 1}
	s.mu.Lock()
	s.expireLocked(now)
	s.embeds[g.Token] = g
	s.mu.Unlock()
	return g, nil
}

// CheckEmbed reauthorizes both the conversation and the exact approved origin
// on every navigation or bridge request.
func (s *Service) CheckEmbed(ctx context.Context, g EmbedGrant, req AccessRequest, rawURL string, allowlist []string) error {
	if s == nil || s.authorize == nil || g.Token == "" || req.TenantID != g.TenantID || req.ConversationID != g.ConversationID || req.PrincipalID != g.PrincipalID || !s.now().Before(g.ExpiresAt) {
		return ErrEmbedDenied
	}
	if err := s.authorize(ctx, req); err != nil {
		return ErrEmbedDenied
	}
	u, err := ValidateEmbedURL(rawURL, allowlist)
	if err != nil || u.Scheme+"://"+u.Host != g.Origin {
		return ErrEmbedDenied
	}
	return nil
}

func (s *Service) Open(ctx context.Context, req AccessRequest, start int64, end *int64) (io.ReadCloser, Reference, error) {
	return s.OpenVariant(ctx, req, "", start, end)
}

// OpenVariant applies the original grant, current conversation authority and
// revocation check to every rendition read. An empty variant serves the exact
// uploaded bytes; GIF remains animated and is always served unchanged.
func (s *Service) OpenVariant(ctx context.Context, req AccessRequest, variant string, start int64, end *int64) (io.ReadCloser, Reference, error) {
	if variant != "" && variant != VariantThumbnail && variant != VariantDisplay {
		return nil, Reference{}, ErrInvalid
	}
	g, err := s.check(ctx, req)
	if err != nil {
		return nil, Reference{}, err
	}
	var a Artifact
	if variant == "" {
		a, err = s.store.Get(ctx, req.TenantID, req.ArtifactID)
	} else {
		a, err = s.store.GetRendition(ctx, req.TenantID, req.ArtifactID, variant)
	}
	if err != nil {
		return nil, Reference{}, err
	}
	if a.State != StateAdmitted {
		return nil, Reference{}, ErrQuarantined
	}
	if start < 0 || start >= a.Size || end != nil && (*end <= start || *end > a.Size) {
		return nil, Reference{}, ErrRange
	}
	if a.ConversationID != req.ConversationID {
		return nil, Reference{}, ErrUnauthorized
	}
	_ = g
	data := a.Content
	if end == nil {
		end = &a.Size
	}
	return io.NopCloser(bytes.NewReader(data[start:*end])), a.Reference, nil
}
func (s *Service) check(ctx context.Context, req AccessRequest) (Grant, error) {
	if s.authorize == nil {
		return Grant{}, ErrUnauthorized
	}
	if err := s.authorize(ctx, req); err != nil {
		return Grant{}, ErrUnauthorized
	}
	s.mu.RLock()
	g, ok := s.grants[req.Grant]
	rev := s.revoked[req.ArtifactID].count
	s.mu.RUnlock()
	if !ok || g.ArtifactID != req.ArtifactID || g.TenantID != req.TenantID || g.ConversationID != req.ConversationID || g.PrincipalID != req.PrincipalID || !s.now().Before(g.ExpiresAt) || rev >= g.Revision {
		return Grant{}, ErrRevoked
	}
	return g, nil
}

func normalizeType(t string) (MediaType, bool) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case string(MediaMP3), "mp3":
		return MediaMP3, true
	case string(MediaWAV), "audio/x-wav", "wav":
		return MediaWAV, true
	case string(MediaBMP), "bmp":
		return MediaBMP, true
	case string(MediaJPEG), "jpg", "jpeg":
		return MediaJPEG, true
	case string(MediaPNG), "png":
		return MediaPNG, true
	case string(MediaGIF), "gif":
		return MediaGIF, true
	case string(MediaMP4), "mp4":
		return MediaMP4, true
	}
	return "", false
}
func sniff(b []byte) MediaType {
	switch {
	case len(b) >= 3 && string(b[:3]) == "ID3":
		return MediaMP3
	case len(b) >= 2 && b[0] == 0xff && b[1]&0xe0 == 0xe0:
		return MediaMP3
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WAVE":
		return MediaWAV
	case len(b) >= 2 && b[0] == 'B' && b[1] == 'M':
		return MediaBMP
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return MediaJPEG
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return MediaPNG
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return MediaGIF
	case len(b) >= 12 && string(b[4:8]) == "ftyp" && isMP4Brand(string(b[8:12])):
		return MediaMP4
	}
	return ""
}
func isMP4Brand(b string) bool {
	switch b {
	case "isom", "iso2", "mp41", "mp42", "avc1", "M4V ":
		return true
	}
	return false
}

// ValidateEmbedURL admits only HTTPS public origins. It rejects credentials,
// fragments, private/link-local destinations and script/data/blob URLs.
func ValidateEmbedURL(raw string, allowlist []string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, ErrEmbedDenied
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") || strings.HasSuffix(strings.ToLower(host), ".local") || strings.HasSuffix(strings.ToLower(host), ".internal") {
		return nil, ErrEmbedDenied
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return nil, ErrEmbedDenied
	}
	for _, a := range allowlist {
		if strings.EqualFold(strings.TrimSuffix(a, "."), u.Hostname()) {
			return u, nil
		}
	}
	return nil, ErrEmbedDenied
}
