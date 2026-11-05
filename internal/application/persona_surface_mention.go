package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errSurfaceMentionBridge = errors.New("application: surface persona mention bridge unavailable")

// SurfaceCommentCommit is one newly created comment on a workflow case or a
// documentation-hub document. Handles are the raw @handles stored on the
// comment; the bridge resolves which of them name personas.
type SurfaceCommentCommit struct {
	TenantID   string
	Kind       chatrecipient.SurfaceKind
	ResourceID string
	// VersionID pins the viewed document version; hub document comments are
	// version-anchored. It is empty for workflow cases.
	VersionID string
	CommentID string
	// ThreadID is the comment thread root: the parent comment or the
	// comment itself.
	ThreadID string
	AuthorID string
	Body     string
	Handles  []string
	// Resolved and Orphaned describe the comment's state. Only a live
	// comment can invoke a persona.
	Resolved bool
	Orphaned bool
}

// surfaceChannelPrefix marks an agentinvoke channel as a surface rather
// than a chat conversation. The one shared invocation resolver keys its
// claims by channel and post, so each surface resource is its own channel.
const surfaceChannelPrefix = "surface:"

// encodeSurfaceChannel names the invocation channel of one surface
// resource. Hub document channels pin the viewed version.
func encodeSurfaceChannel(kind chatrecipient.SurfaceKind, resourceID, versionID string) string {
	if kind == chatrecipient.SurfaceHubDocument {
		return surfaceChannelPrefix + string(kind) + ":" + resourceID + ":" + versionID
	}
	return surfaceChannelPrefix + string(kind) + ":" + resourceID
}

// parseSurfaceChannel splits a surface channel back into its resource. It
// rejects chat channels and malformed surface channels.
func parseSurfaceChannel(channel string) (chatrecipient.SurfaceKind, string, string, bool) {
	rest, ok := strings.CutPrefix(channel, surfaceChannelPrefix)
	if !ok {
		return "", "", "", false
	}
	kindPart, ids, ok := strings.Cut(rest, ":")
	if !ok {
		return "", "", "", false
	}
	kind := chatrecipient.SurfaceKind(kindPart)
	if !kind.Valid() {
		return "", "", "", false
	}
	if kind == chatrecipient.SurfaceWorkflowCase {
		if strings.Contains(ids, ":") || !required(ids) {
			return "", "", "", false
		}
		return kind, ids, "", true
	}
	resource, version, ok := strings.Cut(ids, ":")
	if !ok || strings.Contains(version, ":") || !required(resource) || !required(version) {
		return "", "", "", false
	}
	return kind, resource, version, true
}

// surfaceCommitFromDocumentComment maps one stored hub comment to the
// commit the surface bridge admits.
func surfaceCommitFromDocumentComment(tenantID string, row documenthubstore.Comment) SurfaceCommentCommit {
	thread := row.ParentID
	if thread == "" {
		thread = row.ID
	}
	return SurfaceCommentCommit{
		TenantID: tenantID, Kind: chatrecipient.SurfaceHubDocument,
		ResourceID: row.DocumentID, VersionID: row.VersionID,
		CommentID: row.ID, ThreadID: thread, AuthorID: row.AuthorID,
		Body: row.Body, Handles: append([]string(nil), row.Mentions...),
		Resolved: row.Resolved, Orphaned: row.Orphaned,
	}
}

// surfacePersonaDirectory resolves a raw @handle to a persona identity on
// one surface. Unknown handles are user mentions, not personas.
type surfacePersonaDirectory interface {
	ResolveSurfacePersona(context.Context, string, chatrecipient.SurfaceKind, string) (string, bool, error)
}

// surfaceMentionBridge is the comment-commit boundary between workflow
// pages and documentation-hub documents and the shared persona invocation
// service. The surfaces remain responsible for durability; agentinvoke
// remains responsible for admission, grants, and idempotent run start.
type surfaceMentionBridge struct {
	directory surfacePersonaDirectory
	resolver  *agentinvoke.Service
}

type surfaceMentionBridgeConfig struct {
	Directory surfacePersonaDirectory
	Resolver  *agentinvoke.Service
}

func newSurfaceMentionBridge(cfg surfaceMentionBridgeConfig) (*surfaceMentionBridge, error) {
	if cfg.Directory == nil || cfg.Resolver == nil {
		return nil, fmt.Errorf("%w: persona directory and invocation service are required", errSurfaceMentionBridge)
	}
	return &surfaceMentionBridge{directory: cfg.Directory, resolver: cfg.Resolver}, nil
}

// admitDocumentComment maps one stored hub comment and admits its persona
// mentions through the shared resolver.
func (b *surfaceMentionBridge) admitDocumentComment(ctx context.Context, tenantID string, row documenthubstore.Comment) ([]agentinvoke.Invocation, error) {
	if b == nil || b.directory == nil || b.resolver == nil {
		return nil, errSurfaceMentionBridge
	}
	return b.HandleCommentCommit(ctx, surfaceCommitFromDocumentComment(tenantID, row))
}

// HandleCommentCommit resolves persona handles on one newly created human
// comment. The principal is read from trusted context and must match both
// the comment author and tenant; caller-supplied fields are not used as
// authority. Replaying the same comment is safe because agentinvoke keys
// its claim by channel, comment and persona.
func (b *surfaceMentionBridge) HandleCommentCommit(ctx context.Context, commit SurfaceCommentCommit) ([]agentinvoke.Invocation, error) {
	if b == nil || b.directory == nil || b.resolver == nil {
		return nil, errSurfaceMentionBridge
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return nil, fmt.Errorf("%w: trusted human principal is required", errSurfaceMentionBridge)
	}
	if principal.Subject() != commit.AuthorID || principal.Tenant().String() != commit.TenantID {
		return nil, fmt.Errorf("%w: principal does not match comment author", errSurfaceMentionBridge)
	}
	if err := validSurfaceMentionCommit(commit); err != nil {
		return nil, err
	}
	mentions, err := b.resolveSurfaceMentions(ctx, commit)
	if err != nil {
		return nil, err
	}
	if len(mentions) == 0 {
		return nil, nil
	}
	channel := encodeSurfaceChannel(commit.Kind, commit.ResourceID, commit.VersionID)
	ctx = withPersonaChatAuthorityTuple(ctx, commit.TenantID, principal.Subject(), channel, commit.ThreadID, commit.CommentID)
	invocations, err := b.resolver.OnPostCommit(ctx, agentinvoke.PostCommit{
		TenantID: commit.TenantID, ConversationID: channel, ThreadID: commit.ThreadID,
		PostID: commit.CommentID, AuthorID: principal.Subject(), AuthorKind: agentinvoke.HumanAuthor,
		Body: commit.Body, New: true, Mentions: mentions,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: invoke surface mentions: %v", errSurfaceMentionBridge, err)
	}
	return invocations, nil
}

func validSurfaceMentionCommit(commit SurfaceCommentCommit) error {
	for name, value := range map[string]string{
		"tenant id": commit.TenantID, "resource id": commit.ResourceID, "comment id": commit.CommentID,
		"thread id": commit.ThreadID, "author id": commit.AuthorID,
	} {
		if !required(value) {
			return fmt.Errorf("%w: %s is required", errSurfaceMentionBridge, name)
		}
	}
	if !commit.Kind.Valid() {
		return fmt.Errorf("%w: unknown surface kind", errSurfaceMentionBridge)
	}
	if commit.Kind == chatrecipient.SurfaceHubDocument && !required(commit.VersionID) {
		return fmt.Errorf("%w: document version is required", errSurfaceMentionBridge)
	}
	if commit.Kind == chatrecipient.SurfaceWorkflowCase && commit.VersionID != "" {
		return fmt.Errorf("%w: workflow cases carry no document version", errSurfaceMentionBridge)
	}
	if strings.TrimSpace(commit.Body) == "" {
		return fmt.Errorf("%w: comment body is required", errSurfaceMentionBridge)
	}
	if commit.Resolved || commit.Orphaned {
		return fmt.Errorf("%w: only a live comment can invoke a persona", errSurfaceMentionBridge)
	}
	return nil
}

func (b *surfaceMentionBridge) resolveSurfaceMentions(ctx context.Context, commit SurfaceCommentCommit) ([]agentinvoke.Mention, error) {
	seen := make(map[string]struct{}, len(commit.Handles))
	mentions := make([]agentinvoke.Mention, 0, len(commit.Handles))
	for _, raw := range commit.Handles {
		handle := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
		if handle == "" {
			continue
		}
		id, ok, err := b.directory.ResolveSurfacePersona(ctx, commit.TenantID, commit.Kind, handle)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve surface handle: %v", errSurfaceMentionBridge, err)
		}
		if !ok || !required(id) {
			// A user mention, not a persona.
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		mentions = append(mentions, agentinvoke.Mention{Kind: agentinvoke.PersonaMention, PersonaID: id, Canonical: true})
	}
	return mentions, nil
}

// SurfaceSegment is one piece of the viewed surface resource the invoker
// may read: a case summary or note, or a document section.
type SurfaceSegment struct {
	ID       string
	AuthorID string
	Body     string
}

// SurfaceSegmentRequest asks for the invoker-visible segments of the exact
// resource version under discussion.
type SurfaceSegmentRequest struct {
	TenantID   string
	Kind       chatrecipient.SurfaceKind
	ResourceID string
	VersionID  string
	ThreadID   string
	InvokerID  string
	// InvokingCommentID is the comment carrying the mention.
	InvokingCommentID string
	Limit             int
}

// SurfaceSegmentReader returns only segments the invoker may read from the
// requested resource version. The thread reader below rejects rows that
// escape that version.
type SurfaceSegmentReader interface {
	ReadSurfaceSegments(context.Context, SurfaceSegmentRequest) ([]SurfaceSegment, error)
}

// surfaceThreadReader is the agentinvoke.ThreadReader adapter over surface
// segments. Context binding, digesting, tainting and the escape check stay
// in the shared BuildContext; this adapter only translates the surface
// channel and refuses rows from outside the viewed resource.
type surfaceThreadReader struct {
	segments SurfaceSegmentReader
}

func (r surfaceThreadReader) ReadThread(ctx context.Context, req agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	if r.segments == nil {
		return nil, fmt.Errorf("%w: surface segment reader is required", errSurfaceMentionBridge)
	}
	kind, resource, version, ok := parseSurfaceChannel(req.ConversationID)
	if !ok {
		return nil, fmt.Errorf("%w: not a surface channel", errSurfaceMentionBridge)
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ThreadID) == "" ||
		strings.TrimSpace(req.InvokerID) == "" || strings.TrimSpace(req.InvokingPostID) == "" {
		return nil, fmt.Errorf("%w: thread read request is incomplete", errSurfaceMentionBridge)
	}
	segments, err := r.segments.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: req.TenantID, Kind: kind, ResourceID: resource, VersionID: version,
		ThreadID: req.ThreadID, InvokerID: req.InvokerID, InvokingCommentID: req.InvokingPostID, Limit: req.Limit,
	})
	if err != nil {
		return nil, err
	}
	posts := make([]agentinvoke.ThreadPost, 0, len(segments))
	for _, segment := range segments {
		if !required(segment.ID) || !required(segment.AuthorID) || strings.TrimSpace(segment.Body) == "" {
			return nil, fmt.Errorf("%w: surface segment is incomplete", errSurfaceMentionBridge)
		}
		if !surfaceSegmentBelongs(segment.ID, kind, resource, version) {
			return nil, fmt.Errorf("%w: surface segment escaped the viewed resource", errSurfaceMentionBridge)
		}
		posts = append(posts, agentinvoke.ThreadPost{
			TenantID: req.TenantID, ConversationID: req.ConversationID, ThreadID: req.ThreadID,
			ID: segment.ID, AuthorID: segment.AuthorID, Body: segment.Body,
		})
	}
	return posts, nil
}

// surfaceSegmentBelongs reports whether a segment id names a part of the
// viewed resource version: "case-1/..." for a workflow case,
// "doc-9/v12#..." or "doc-9/v12/..." for a hub document.
func surfaceSegmentBelongs(id string, kind chatrecipient.SurfaceKind, resource, version string) bool {
	prefix := resource
	if kind == chatrecipient.SurfaceHubDocument {
		prefix += "/" + version
	}
	if !strings.HasPrefix(id, prefix) || len(id) == len(prefix) {
		return false
	}
	next := id[len(prefix)]
	return next == '/' || next == '#'
}

// withDocumentPersonaMentions wires persona @mention admission into a
// document service. A composition without a bridge, or a foreign service,
// is returned unchanged.
func withDocumentPersonaMentions(service transportdocument.Service, bridge *surfaceMentionBridge) transportdocument.Service {
	ds, ok := service.(documentService)
	if !ok || bridge == nil {
		return service
	}
	ds.personaMentions = bridge
	return ds
}

// admitDocumentPersonaMentions admits the persona mentions of one committed
// document comment. The comment is already durable; an invocation failure
// is recorded and never fails the comment.
func (s documentService) admitDocumentPersonaMentions(ctx context.Context, tenantID string, row documenthubstore.Comment) {
	if s.personaMentions == nil {
		return
	}
	if _, err := s.personaMentions.admitDocumentComment(ctx, tenantID, row); err != nil {
		slog.WarnContext(ctx, "hcmnext.surface_mention_admission_failed", "comment_id", row.ID, "document_id", row.DocumentID, "error", err)
	}
}
