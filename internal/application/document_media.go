package application

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/docsexport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportmedia "github.com/monstercameron/human-capital-management-suite/internal/transport/documentmedia"
)

// documentMediaRootLeaf is the document media directory, a sibling of the
// chat media root: the same durable location a deployment configured for
// chat media, with the two workloads' files kept apart.
const documentMediaRootLeaf = "document-media"

// DefaultDocumentMediaRoot resolves where document attachment bytes live
// from the chat media configuration (chat-media-root, else the artifact
// root, else .artifacts).
func DefaultDocumentMediaRoot(mediaRoot, artifactRoot string) string {
	return filepath.Join(filepath.Dir(DefaultChatMediaArtifactRoot(mediaRoot, artifactRoot)), documentMediaRootLeaf)
}

// documentMediaService adapts the document store and the export renderers
// to the attachment boundary's port.
type documentMediaService struct {
	store *documenthubstore.Store
	blobs documenthubstore.MediaBlobs
}

var _ transportmedia.Service = documentMediaService{}

func transportAttachment(m documenthubstore.Media) transportmedia.Attachment {
	return transportmedia.Attachment{ID: m.ID, DocumentID: m.DocumentID, Filename: m.Filename, MediaType: m.MediaType, Size: m.SizeBytes, Width: m.Width, Height: m.Height, Pages: m.Pages, CreatedAt: m.CreatedAt}
}

// mediaError maps store sentinels onto the boundary's.
func mediaError(err error) error {
	switch {
	case errors.Is(err, documenthubstore.ErrDenied):
		return transportmedia.ErrNotFound
	case errors.Is(err, documenthubstore.ErrMediaTooLarge):
		return transportmedia.ErrTooLarge
	case errors.Is(err, documenthubstore.ErrMediaUnsupported):
		return transportmedia.ErrUnsupported
	}
	return err
}

func (s documentMediaService) Upload(ctx context.Context, tenant, actor, documentID, filename string, content []byte) (transportmedia.Attachment, error) {
	m, err := s.store.AddMedia(ctx, s.blobs, tenant, actor, documentID, filename, content)
	if errors.Is(err, documenthubstore.ErrDenied) {
		// A reader who may not edit learns that much; anyone else learns
		// nothing about the document.
		if _, readErr := s.store.ListMedia(ctx, tenant, actor, documentID); readErr == nil {
			return transportmedia.Attachment{}, transportmedia.ErrForbidden
		}
	}
	if err != nil {
		return transportmedia.Attachment{}, mediaError(err)
	}
	return transportAttachment(m), nil
}

func (s documentMediaService) List(ctx context.Context, tenant, actor, documentID string) ([]transportmedia.Attachment, error) {
	rows, err := s.store.ListMedia(ctx, tenant, actor, documentID)
	if err != nil {
		return nil, mediaError(err)
	}
	out := make([]transportmedia.Attachment, 0, len(rows))
	for _, m := range rows {
		out = append(out, transportAttachment(m))
	}
	return out, nil
}

func (s documentMediaService) Open(ctx context.Context, tenant, actor, documentID, attachmentID string) (transportmedia.Attachment, []byte, error) {
	m, content, err := s.store.OpenMedia(ctx, s.blobs, tenant, actor, documentID, attachmentID)
	if err != nil {
		return transportmedia.Attachment{}, nil, mediaError(err)
	}
	return transportAttachment(m), content, nil
}

// Export renders the version the caller may read: plain text, Markdown
// with attachment references made absolute, or PDF with attached images
// embedded.
func (s documentMediaService) Export(ctx context.Context, tenant, actor, documentID, format, origin string) (transportmedia.File, error) {
	summary, version, err := s.store.ReadPersonalDocument(ctx, tenant, actor, documentID)
	if err != nil {
		return transportmedia.File{}, mediaError(err)
	}
	base := exportFilename(summary.Title)
	switch format {
	case "txt":
		return transportmedia.File{Name: base + ".txt", ContentType: "text/plain; charset=utf-8", Content: []byte(docsexport.PlainText(version.Markdown))}, nil
	case "md":
		prefix := strings.TrimRight(origin, "/") + transportmedia.PathPrefix + documentID + "/"
		markdown := docsexport.Markdown(version.Markdown, func(id string) string { return prefix + id + "?download=1" })
		return transportmedia.File{Name: base + ".md", ContentType: "text/markdown; charset=utf-8", Content: []byte(markdown)}, nil
	case "pdf":
		images := func(destination string) ([]byte, bool) {
			id, ok := strings.CutPrefix(destination, docsexport.AttachmentScheme)
			if !ok {
				return nil, false
			}
			m, content, err := s.store.OpenMedia(ctx, s.blobs, tenant, actor, documentID, id)
			if err != nil || !m.IsImage() {
				return nil, false
			}
			return content, true
		}
		return transportmedia.File{Name: base + ".pdf", ContentType: "application/pdf", Content: docsexport.PDF(summary.Title, version.Markdown, images)}, nil
	}
	return transportmedia.File{}, transportmedia.ErrInvalid
}

// exportFilename is a document title made safe as a file name.
func exportFilename(title string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(title))
	if runes := []rune(name); len(runes) > 80 {
		name = string(runes[:80])
	}
	name = strings.Trim(name, " .-")
	if name == "" {
		return "document"
	}
	return name
}

// DocumentMediaHandler returns the attachment and export boundary, or a
// 503 endpoint when the document store or the media root is unavailable.
func DocumentMediaHandler(store *documenthubstore.Store, root string) http.Handler {
	if store == nil {
		return unavailableDocumentMedia()
	}
	blobs, err := documenthubstore.NewMediaFiles(root)
	if err != nil {
		return unavailableDocumentMedia()
	}
	return transportmedia.NewHandler(documentMediaService{store: store, blobs: blobs})
}

func unavailableDocumentMedia() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "document media unavailable", http.StatusServiceUnavailable)
	})
}

// OverlayDocumentMedia mounts the document attachment boundary ahead of
// the edge, behind the same admission as every other edge request.
func OverlayDocumentMedia(next http.Handler, store *documenthubstore.Store, root string, admission transport.Config) http.Handler {
	mux := http.NewServeMux()
	h := DocumentMediaHandler(store, root)
	mux.Handle(transportmedia.PathPrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _, err := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if err != nil {
			http.Error(w, "request denied", err.HTTPStatus())
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	mux.Handle("/", next)
	return mux
}
