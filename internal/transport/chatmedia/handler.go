// Package chatmedia exposes the protected HTTP media boundary. It is a thin
// adapter: authorization, quarantine and range policy remain in collaboration.
package chatmedia

import (
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
)

// Transport bounds for one upload.
const (
	// MaxUploadBytes is the largest artifact this boundary accepts. It matches
	// the collaboration service's own default ceiling, so an upload that would
	// be refused there is refused before it is buffered here.
	MaxUploadBytes = 64 << 20
	// MaxUploadFieldBytes bounds one non-file multipart field (transcript, alt
	// text). Accessibility text is prose, not payload.
	MaxUploadFieldBytes = 64 << 10
	// DefaultMaxConcurrentUploadsPerTenant is how many uploads one company may
	// have in flight at once. Every upload holds its whole artifact in memory
	// until the collaboration port grows a streaming seam, so unbounded
	// concurrency is unbounded memory: one tenant could exhaust the process.
	DefaultMaxConcurrentUploadsPerTenant = 4
)

// grantTTL is how long a minted download grant stays valid. It is short
// because the grant is meant to cover one player or one <img>, not a session;
// the collaboration service clamps anything above fifteen minutes anyway.
const grantTTL = 5 * time.Minute

// UploadGate bounds concurrent uploads per tenant.
//
// Its state is behind reference types on purpose: [Handler] is used as a value
// (its ServeHTTP has a value receiver, and a composition root may build it as
// a struct literal), so a copied gate has to share the counters of the gate it
// was copied from.
type UploadGate struct {
	limit    int
	mu       *sync.Mutex
	inflight map[string]int
}

// NewUploadGate returns a gate admitting at most limit uploads per tenant. A
// limit below one is raised to one rather than meaning "unbounded".
func NewUploadGate(limit int) UploadGate {
	if limit < 1 {
		limit = 1
	}
	return UploadGate{limit: limit, mu: &sync.Mutex{}, inflight: map[string]int{}}
}

// acquire reserves a slot for tenant and reports whether it got one. An
// unconfigured gate admits everything, which is what a [Handler] built as a
// struct literal without one gets.
func (g UploadGate) acquire(tenant string) bool {
	if g.mu == nil || g.inflight == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight[tenant] >= g.limit {
		return false
	}
	g.inflight[tenant]++
	return true
}

// release returns a slot.
func (g UploadGate) release(tenant string) {
	if g.mu == nil || g.inflight == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight[tenant] <= 1 {
		delete(g.inflight, tenant)
		return
	}
	g.inflight[tenant]--
}

type Handler struct {
	Service   *core.Service
	Principal func(*http.Request) (tenant, conversation, principal string, ok bool)
	// Uploads bounds concurrent uploads per tenant. A zero value admits every
	// upload; [NewHandler] supplies a configured one.
	Uploads UploadGate
}

// NewHandler returns the protected media boundary with its upload gate
// configured. A composition that builds [Handler] as a struct literal gets an
// unbounded gate, so this is the constructor a composition root should use.
func NewHandler(service *core.Service, principal func(*http.Request) (tenant, conversation, principal string, ok bool)) *Handler {
	return &Handler{Service: service, Principal: principal, Uploads: NewUploadGate(DefaultMaxConcurrentUploadsPerTenant)}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Nothing this boundary serves may be cached: the bytes are protected by a
	// grant that expires, and the refusals are per principal.
	w.Header().Set("Cache-Control", "no-store")
	if h.Service == nil {
		http.Error(w, "media unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/upload") {
		h.upload(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/grant") {
		h.grant(w, r)
		return
	}
	if r.Method == http.MethodGet {
		h.get(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// uploadStatus maps an upload outcome onto a status and a safe message.
//
// The provider's own error text never reaches the client: it names sentinels,
// sizes and store paths, and a caller probing the boundary should learn only
// which of the published conditions it hit.
func uploadStatus(err error) (int, string) {
	switch {
	case errors.Is(err, core.ErrScannerUnavailable):
		return http.StatusServiceUnavailable, "media inspection is unavailable"
	case errors.Is(err, core.ErrBusy):
		return http.StatusTooManyRequests, "image processing is busy"
	case errors.Is(err, core.ErrQuarantined):
		return http.StatusUnprocessableEntity, "the artifact was not admitted"
	case errors.Is(err, core.ErrUnsupported):
		return http.StatusUnsupportedMediaType, "unsupported or mismatched media type"
	case errors.Is(err, core.ErrUnauthorized):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, core.ErrInvalid):
		return http.StatusBadRequest, "invalid upload"
	default:
		return http.StatusInternalServerError, "the upload could not be completed"
	}
}

// readStatus maps a protected-read outcome onto a status and a safe message.
func readStatus(err error) (int, string) {
	switch {
	case errors.Is(err, core.ErrRange):
		return http.StatusRequestedRangeNotSatisfiable, "invalid range"
	case errors.Is(err, core.ErrQuarantined):
		return http.StatusForbidden, "the artifact is not admitted"
	case errors.Is(err, core.ErrUnauthorized), errors.Is(err, core.ErrRevoked):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, core.ErrInvalid):
		return http.StatusBadRequest, "invalid request"
	case errors.Is(err, core.ErrUnsupported):
		return http.StatusUnsupportedMediaType, "image rendition unavailable"
	default:
		// A grant is only ever minted for an artifact the store already
		// produced, so an unclassified failure on a granted read is this
		// process failing, not the caller asking for something absent.
		return http.StatusInternalServerError, "the artifact could not be read"
	}
}

func (h Handler) upload(w http.ResponseWriter, r *http.Request) {
	tenant, conv, principal, ok := h.identity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.Uploads.acquire(tenant) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many uploads in flight", http.StatusTooManyRequests)
		return
	}
	defer h.Uploads.release(tenant)

	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+1)
	parts, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	// One pass over the parts, one copy of the artifact. The previous shape
	// ran ParseMultipartForm and then io.ReadAll over the result, so a 64 MiB
	// upload was buffered twice before the service saw it once.
	var (
		content             []byte
		filename, declared  string
		transcript, altText string
		sawFile             bool
	)
	for {
		part, partErr := parts.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			http.Error(w, "invalid upload", http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case "file":
			filename, declared = part.FileName(), part.Header.Get("Content-Type")
			content, partErr = io.ReadAll(io.LimitReader(part, MaxUploadBytes+1))
			sawFile = true
		case "transcript":
			transcript, partErr = readField(part)
		case "alt_text":
			altText, partErr = readField(part)
		default:
			_, partErr = io.Copy(io.Discard, io.LimitReader(part, MaxUploadFieldBytes+1))
		}
		closeErr := part.Close()
		if partErr != nil || closeErr != nil {
			http.Error(w, "invalid upload", http.StatusBadRequest)
			return
		}
		if len(content) > MaxUploadBytes {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
			return
		}
	}
	if !sawFile {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	ref, err := h.Service.Upload(r.Context(), core.UploadRequest{TenantID: tenant, ConversationID: conv, PrincipalID: principal, Filename: filename, DeclaredType: declared, EvidenceID: r.Header.Get("Idempotency-Key"), Content: content, Transcript: transcript, AltText: altText})
	if err != nil {
		if errors.Is(err, core.ErrBusy) {
			w.Header().Set("Retry-After", "1")
		}
		status, message := uploadStatus(err)
		http.Error(w, message, status)
		return
	}
	// The upload response carries the grant the reader needs, so a client
	// never has to guess how to fetch back what it just sent.
	h.writeRef(w, r, ref, tenant, conv, principal)
}

// readField reads one bounded non-file multipart field.
func readField(part *multipart.Part) (string, error) {
	b, err := io.ReadAll(io.LimitReader(part, MaxUploadFieldBytes))
	return string(b), err
}

// writeRef encodes an artifact reference plus a freshly minted grant.
func (h Handler) writeRef(w http.ResponseWriter, r *http.Request, ref core.Reference, tenant, conv, principal string) {
	out := struct {
		core.Reference
		Grant     string `json:"grant,omitempty"`
		ExpiresAt string `json:"grant_expires_at,omitempty"`
	}{Reference: ref}
	if g, err := h.Service.Authorize(r.Context(), core.AccessRequest{TenantID: tenant, ConversationID: conv, PrincipalID: principal, ArtifactID: ref.ArtifactID}, grantTTL); err == nil {
		out.Grant, out.ExpiresAt = g.Token, g.ExpiresAt.UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// grant mints a short-lived download authorization for an authorized member.
//
// Without it the protected read was unreachable: the read requires a grant
// token and nothing on the wire ever produced one, so every GET of admitted
// bytes answered 403. The minting itself is the collaboration service's
// decision - it re-checks conversation authorization and refuses an artifact
// that is not admitted - and this handler only projects the answer.
func (h Handler) grant(w http.ResponseWriter, r *http.Request) {
	tenant, conv, principal, ok := h.identity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := artifactID(strings.TrimSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/grant"), r)
	if id == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	g, err := h.Service.Authorize(r.Context(), core.AccessRequest{TenantID: tenant, ConversationID: conv, PrincipalID: principal, ArtifactID: id}, grantTTL)
	if err != nil {
		status, message := readStatus(err)
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Grant      string `json:"grant"`
		ArtifactID string `json:"artifact_id"`
		ExpiresAt  string `json:"grant_expires_at"`
	}{g.Token, g.ArtifactID, g.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (h Handler) get(w http.ResponseWriter, r *http.Request) {
	tenant, conv, principal, ok := h.identity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := artifactID(r.URL.Path, r)
	grant := r.Header.Get("X-Chat-Media-Grant")
	if grant == "" {
		grant = r.Header.Get("Authorization")
	}
	if grant == "" {
		// A query grant exists for one reason: an <img src> cannot carry a
		// header. It is the same token, checked the same way, with the same
		// expiry and revocation, and the response is still no-store — so a URL
		// that leaks is a token that has already expired rather than a standing
		// door. Header first, so a caller that can send one is unaffected.
		grant = r.URL.Query().Get("grant")
	}
	grant = strings.TrimPrefix(grant, "Bearer ")
	start, end, rangeErr := byteRange(r.Header.Get("Range"))
	if rangeErr != nil {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	body, ref, err := h.Service.OpenVariant(r.Context(), core.AccessRequest{TenantID: tenant, ConversationID: conv, PrincipalID: principal, ArtifactID: id, Grant: grant}, r.URL.Query().Get("variant"), start, end)
	if err != nil {
		status, message := readStatus(err)
		http.Error(w, message, status)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", string(ref.MediaType))
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Accept-Ranges", "bytes")
	actualEnd := ref.Size
	if end != nil {
		actualEnd = *end
	}
	w.Header().Set("Content-Length", strconv.FormatInt(actualEnd-start, 10))
	if r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(actualEnd-1, 10)+"/"+strconv.FormatInt(ref.Size, 10))
		w.WriteHeader(http.StatusPartialContent)
	}
	_, _ = io.Copy(w, body)
}

// artifactID resolves the artifact identifier from the route or, for a mount
// that does not declare a pattern variable, from the last path segment.
func artifactID(path string, r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return parts[len(parts)-1]
}

// byteRange parses a single-range Range header. An absent header is the whole
// artifact; anything this boundary does not serve is a range error, never a
// silently widened read.
func byteRange(header string) (int64, *int64, error) {
	if header == "" {
		return 0, nil, nil
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, nil, core.ErrRange
	}
	p := strings.Split(strings.TrimPrefix(header, "bytes="), "-")
	// RFC byte-range-spec has one explicit first byte and exactly one hyphen.
	// Suffix ranges, multi-ranges, and a bare position are refused rather than
	// being reinterpreted as a broader read by a downstream store.
	if len(p) != 2 || p[0] == "" {
		return 0, nil, core.ErrRange
	}
	start, err := parseRangeIndex(p[0])
	if err != nil {
		return 0, nil, core.ErrRange
	}
	if p[1] != "" {
		last, parseErr := parseRangeIndex(p[1])
		if parseErr != nil || last == int64(^uint64(0)>>1) || last < start {
			return 0, nil, core.ErrRange
		}
		last++
		return start, &last, nil
	}
	return start, nil, nil
}

func parseRangeIndex(s string) (int64, error) {
	if s == "" {
		return 0, core.ErrRange
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, core.ErrRange
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

func (h Handler) identity(r *http.Request) (string, string, string, bool) {
	if h.Principal == nil {
		return "", "", "", false
	}
	return h.Principal(r)
}
