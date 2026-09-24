// Package documentmedia is the authenticated HTTP boundary for document
// attachments and exports. It adapts HTTP only: who may upload, read or
// export a document, and what the bytes are, are the Service's decisions.
//
// Routes, under PathPrefix:
//
//	POST {doc}/upload            multipart "file"; needs edit
//	GET  {doc}                   the document's attachments as JSON; needs read
//	GET  {doc}/{attachment}      the bytes (?download=1 for a download); needs read
//	GET  {doc}/export?format=f   txt, md or pdf; needs read
package documentmedia

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PathPrefix is where the edge mounts this boundary. The product shell's
// connect-src names it, so the page may fetch it with its bearer.
const PathPrefix = "/v1/documents/media/"

// Transport bounds.
const (
	// MaxUploadBytes is the largest file accepted (the PDF cap); the
	// service applies the per-type caps.
	MaxUploadBytes = 25 << 20
	// multipartSlack covers the multipart framing around the file.
	multipartSlack = 64 << 10
	// MaxConcurrentUploads bounds uploads in flight per handler: each holds
	// its whole file in memory until it is stored.
	MaxConcurrentUploads = 4
)

var (
	// ErrNotFound is a document or attachment that does not exist or is
	// not visible to the caller; the two are deliberately the same answer.
	ErrNotFound = errors.New("document media: not found")
	// ErrForbidden is a visible document the caller may not change.
	ErrForbidden = errors.New("document media: forbidden")
	// ErrTooLarge is a file over its type's cap.
	ErrTooLarge = errors.New("document media: too large")
	// ErrUnsupported is a file that is not an admitted image or PDF.
	ErrUnsupported = errors.New("document media: unsupported media type")
	// ErrInvalid is a malformed request (unknown export format).
	ErrInvalid = errors.New("document media: invalid request")
)

// Attachment is one attachment as the client sees it.
type Attachment struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	Filename   string    `json:"filename"`
	MediaType  string    `json:"media_type"`
	Size       int64     `json:"size"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Pages      int       `json:"pages,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// File is an export ready to send.
type File struct {
	Name, ContentType string
	Content           []byte
}

// Service is the port this boundary calls.
type Service interface {
	Upload(ctx context.Context, tenant, actor, documentID, filename string, content []byte) (Attachment, error)
	List(ctx context.Context, tenant, actor, documentID string) ([]Attachment, error)
	Open(ctx context.Context, tenant, actor, documentID, attachmentID string) (Attachment, []byte, error)
	Export(ctx context.Context, tenant, actor, documentID, format, origin string) (File, error)
}

// Handler serves PathPrefix.
type Handler struct {
	Service Service
	// Principal reads the caller; nil uses the trusted human principal the
	// edge admission put on the context.
	Principal func(*http.Request) (tenant, actor string, ok bool)
	uploads   chan struct{}
}

// NewHandler returns the boundary with its upload bound configured.
func NewHandler(service Service) *Handler {
	return &Handler{Service: service, uploads: make(chan struct{}, MaxConcurrentUploads)}
}

// TrustedPrincipal is the caller from the admitted request context: a
// human principal's tenant and subject, as the document RPCs use.
func TrustedPrincipal(r *http.Request) (string, string, bool) {
	p, ok := trust.FromContext(r.Context())
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return "", "", false
	}
	return p.Tenant().String(), p.Subject(), true
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every answer is per caller and grant-dependent: never cached.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if h.Service == nil {
		http.Error(w, "document media unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := h.Principal
	if principal == nil {
		principal = TrustedPrincipal
	}
	tenant, actor, ok := principal(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, PathPrefix), "/"), "/")
	if len(parts) == 0 || len(parts) > 2 || !validID(parts[0]) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	documentID := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		h.list(w, r, tenant, actor, documentID)
	case len(parts) == 2 && parts[1] == "upload" && r.Method == http.MethodPost:
		h.upload(w, r, tenant, actor, documentID)
	case len(parts) == 2 && parts[1] == "export" && r.Method == http.MethodGet:
		h.export(w, r, tenant, actor, documentID)
	case len(parts) == 2 && parts[1] != "upload" && validID(parts[1]) && r.Method == http.MethodGet:
		h.open(w, r, tenant, actor, documentID, parts[1])
	case len(parts) == 2 && (parts[1] == "upload" || parts[1] == "export" || validID(parts[1])):
		w.Header().Set("Allow", map[bool]string{true: http.MethodPost, false: http.MethodGet}[parts[1] == "upload"])
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// validID admits the identifiers the document service issues.
func validID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return false
		}
	}
	return id != "." && id != ".."
}

// status maps a service outcome onto a status and a safe message; the
// service's own error text never reaches the client.
func status(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "file too large"
	case errors.Is(err, ErrUnsupported):
		return http.StatusUnsupportedMediaType, "unsupported file type"
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest, "invalid request"
	default:
		return http.StatusInternalServerError, "the request could not be completed"
	}
}

func fail(w http.ResponseWriter, err error) {
	code, message := status(err)
	http.Error(w, message, code)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, tenant, actor, documentID string) {
	items, err := h.Service.List(r.Context(), tenant, actor, documentID)
	if err != nil {
		fail(w, err)
		return
	}
	if items == nil {
		items = []Attachment{}
	}
	writeJSON(w, http.StatusOK, struct {
		Attachments []Attachment `json:"attachments"`
	}{items})
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, tenant, actor, documentID string) {
	if h.uploads != nil {
		select {
		case h.uploads <- struct{}{}:
			defer func() { <-h.uploads }()
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many uploads in flight", http.StatusTooManyRequests)
			return
		}
	}
	if r.ContentLength > MaxUploadBytes+multipartSlack {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+multipartSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	var (
		content  []byte
		filename string
		sawFile  bool
	)
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			var tooBig *http.MaxBytesError
			if errors.As(partErr, &tooBig) {
				http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid upload", http.StatusBadRequest)
			return
		}
		if part.FormName() == "file" && !sawFile {
			// The declared Content-Type of the part is ignored on purpose:
			// the service sniffs the bytes.
			filename = part.FileName()
			content, partErr = io.ReadAll(io.LimitReader(part, MaxUploadBytes+1))
			sawFile = true
		} else {
			_, partErr = io.Copy(io.Discard, io.LimitReader(part, multipartSlack))
		}
		_ = part.Close()
		if partErr != nil {
			var tooBig *http.MaxBytesError
			if errors.As(partErr, &tooBig) {
				http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid upload", http.StatusBadRequest)
			return
		}
		if len(content) > MaxUploadBytes {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
	}
	if !sawFile || len(content) == 0 {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	attachment, err := h.Service.Upload(r.Context(), tenant, actor, documentID, filename, content)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

func (h *Handler) open(w http.ResponseWriter, r *http.Request, tenant, actor, documentID, attachmentID string) {
	attachment, content, err := h.Service.Open(r.Context(), tenant, actor, documentID, attachmentID)
	if err != nil {
		fail(w, err)
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" || !strings.HasPrefix(attachment.MediaType, "image/") && attachment.MediaType != "application/pdf" {
		disposition = "attachment"
	}
	writeFile(w, r, File{Name: attachment.Filename, ContentType: attachment.MediaType, Content: content}, disposition)
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request, tenant, actor, documentID string) {
	format := r.URL.Query().Get("format")
	if format != "txt" && format != "md" && format != "pdf" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	file, err := h.Service.Export(r.Context(), tenant, actor, documentID, format, requestOrigin(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeFile(w, r, file, "attachment")
}

// writeFile sends bytes with headers that keep them inert: an exact type,
// nosniff, and a sandboxing CSP in case the file is opened directly.
func writeFile(w http.ResponseWriter, r *http.Request, file File, disposition string) {
	name := file.Name
	if name == "" {
		name = "download"
	}
	header := w.Header()
	header.Set("Content-Type", file.ContentType)
	if value := mime.FormatMediaType(disposition, map[string]string{"filename": name}); value != "" {
		header.Set("Content-Disposition", value)
	} else {
		header.Set("Content-Disposition", disposition)
	}
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Content-Length", strconv.Itoa(len(file.Content)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(file.Content)
	}
}

// requestOrigin is the scheme and host the caller reached, for the
// absolute attachment addresses in a Markdown export.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" || strings.ContainsAny(host, "/\\ \"'<>") {
		host = "localhost"
	}
	return scheme + "://" + host
}
