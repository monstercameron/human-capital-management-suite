package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

var errSurfaceSegmentReader = errors.New("application: surface segment reader unavailable")

// surfaceVersionContent is the invoker-visible content of one hub document
// version: the exact Markdown the store authorized for this reader.
type surfaceVersionContent struct {
	AuthorID string
	Title    string
	Markdown string
	Hash     string
}

// surfaceVersionComment is one comment on the viewed version.
type surfaceVersionComment struct {
	ID        string
	AuthorID  string
	Body      string
	CreatedAt time.Time
}

// surfaceDocumentContent is the narrow hub read the segment reader needs.
// Both methods authorize the invoker themselves: revocation closes this path
// at once, and an outsider reads nothing.
type surfaceDocumentContent interface {
	ReadSurfaceVersion(ctx context.Context, tenant, docID, versionID, invokerID string) (surfaceVersionContent, error)
	ListSurfaceVersionComments(ctx context.Context, tenant, docID, versionID, invokerID string) ([]surfaceVersionComment, error)
}

// surfaceDocumentStoreContent is the production surfaceDocumentContent over
// the durable documentation-hub store.
type surfaceDocumentStoreContent struct{ store *documenthubstore.Store }

func newSurfaceDocumentStoreContent(store *documenthubstore.Store) (*surfaceDocumentStoreContent, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: document store is required", errSurfaceSegmentReader)
	}
	return &surfaceDocumentStoreContent{store: store}, nil
}

func (c *surfaceDocumentStoreContent) ReadSurfaceVersion(ctx context.Context, tenant, docID, versionID, invokerID string) (surfaceVersionContent, error) {
	if c == nil || c.store == nil || ctx == nil {
		return surfaceVersionContent{}, errSurfaceSegmentReader
	}
	version, err := c.store.ReadVersion(ctx, tenant, docID, versionID, "person", invokerID)
	if err != nil {
		return surfaceVersionContent{}, fmt.Errorf("%w: read surface version: %w", errSurfaceSegmentReader, err)
	}
	return surfaceVersionContent{AuthorID: version.CreatorID, Title: version.Title, Markdown: version.Markdown, Hash: version.Hash}, nil
}

func (c *surfaceDocumentStoreContent) ListSurfaceVersionComments(ctx context.Context, tenant, docID, versionID, invokerID string) ([]surfaceVersionComment, error) {
	if c == nil || c.store == nil || ctx == nil {
		return nil, errSurfaceSegmentReader
	}
	thread, err := c.store.ListComments(ctx, tenant, docID, versionID, "person", invokerID)
	if err != nil {
		return nil, fmt.Errorf("%w: list surface comments: %w", errSurfaceSegmentReader, err)
	}
	out := make([]surfaceVersionComment, 0, len(thread))
	for _, comment := range thread {
		out = append(out, surfaceVersionComment{ID: comment.ID, AuthorID: comment.AuthorID, Body: comment.Body, CreatedAt: comment.CreatedAt})
	}
	return out, nil
}

// storeSurfaceSegmentReader returns the invoker-visible segments of the exact
// resource version under discussion. A hub document binds its pinned version
// split into sections plus that version's comment thread, in store order;
// workflow cases bind nothing until their comment surface exists in-tree, so
// the invoker's question stands alone rather than binding foreign rows. The
// read never exceeds the shared thread bound, whatever limit it is given.
type storeSurfaceSegmentReader struct{ documents surfaceDocumentContent }

func newStoreSurfaceSegmentReader(documents surfaceDocumentContent) (*storeSurfaceSegmentReader, error) {
	if documents == nil {
		return nil, fmt.Errorf("%w: document content is required", errSurfaceSegmentReader)
	}
	return &storeSurfaceSegmentReader{documents: documents}, nil
}

func (r *storeSurfaceSegmentReader) ReadSurfaceSegments(ctx context.Context, request SurfaceSegmentRequest) ([]SurfaceSegment, error) {
	if r == nil || r.documents == nil || ctx == nil {
		return nil, errSurfaceSegmentReader
	}
	for name, value := range map[string]string{
		"tenant id": request.TenantID, "resource id": request.ResourceID, "thread id": request.ThreadID,
		"invoker id": request.InvokerID, "invoking comment id": request.InvokingCommentID,
	} {
		if !required(value) {
			return nil, fmt.Errorf("%w: %s is required", errSurfaceSegmentReader, name)
		}
	}
	if !request.Kind.Valid() {
		return nil, fmt.Errorf("%w: unknown surface kind", errSurfaceSegmentReader)
	}
	if request.Kind == chatrecipient.SurfaceWorkflowCase {
		if request.VersionID != "" {
			return nil, fmt.Errorf("%w: workflow cases carry no document version", errSurfaceSegmentReader)
		}
		return nil, nil
	}
	if !required(request.VersionID) {
		return nil, fmt.Errorf("%w: document version is required", errSurfaceSegmentReader)
	}
	version, err := r.documents.ReadSurfaceVersion(ctx, request.TenantID, request.ResourceID, request.VersionID, request.InvokerID)
	if err != nil {
		return nil, err
	}
	comments, err := r.documents.ListSurfaceVersionComments(ctx, request.TenantID, request.ResourceID, request.VersionID, request.InvokerID)
	if err != nil {
		return nil, err
	}
	segments := make([]SurfaceSegment, 0, len(comments)+4)
	for index, section := range splitSurfaceMarkdownSections(version.Markdown) {
		segments = append(segments, SurfaceSegment{
			ID:       request.ResourceID + "/" + request.VersionID + "#section-" + strconv.Itoa(index+1),
			AuthorID: version.AuthorID, Body: section,
		})
	}
	for _, comment := range comments {
		segments = append(segments, SurfaceSegment{
			ID:       request.ResourceID + "/" + request.VersionID + "/comment-" + comment.ID,
			AuthorID: comment.AuthorID, Body: comment.Body,
		})
	}
	limit := request.Limit
	if limit <= 0 || limit > agentinvoke.MaxThreadPosts {
		limit = agentinvoke.MaxThreadPosts
	}
	if len(segments) > limit {
		segments = segments[:limit]
	}
	return segments, nil
}

var _ SurfaceSegmentReader = (*storeSurfaceSegmentReader)(nil)

// splitSurfaceMarkdownSections splits version Markdown into one segment per
// ATX heading plus the preamble. Blank chunks are dropped; surviving text is
// otherwise byte-preserved, because trailing spaces carry Markdown meaning.
// Fenced code blocks never split: a "#" line inside a sample is content.
func splitSurfaceMarkdownSections(markdown string) []string {
	var sections []string
	var current []string
	flush := func() {
		text := strings.Trim(strings.Join(current, "\n"), "\n")
		current = nil
		if strings.TrimSpace(text) == "" {
			return
		}
		sections = append(sections, text)
	}
	fenced := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
		}
		if !fenced && isSurfaceMarkdownHeading(trimmed) {
			flush()
		}
		current = append(current, line)
	}
	flush()
	return sections
}

func isSurfaceMarkdownHeading(line string) bool {
	depth := 0
	for depth < len(line) && line[depth] == '#' {
		depth++
	}
	if depth == 0 || depth > 6 || depth == len(line) {
		return depth > 0 && depth <= 6 && depth == len(line)
	}
	next := line[depth]
	return next == ' ' || next == '\t'
}
