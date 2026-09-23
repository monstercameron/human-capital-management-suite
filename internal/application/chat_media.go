package application

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportmedia "github.com/monstercameron/human-capital-management-suite/internal/transport/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatMediaConfig is the explicit composition seam for protected chat media.
// A deployment must provide both a durable artifact root and a real scanner;
// an absent scanner intentionally produces an unavailable handler.
type ChatMediaConfig struct {
	ArtifactRoot string
	Scanner      chatmedia.Scanner
	Authorize    chatmedia.Authorizer
	Principal    func(*http.Request) (tenant, conversation, principal string, ok bool)
}

// EnvChatMediaRoot and EnvArtifactRoot name the durable location for chat media
// bytes. The repository convention is the ignored .artifacts/ tree, with
// .artifacts/chat-media as this workload's child of it, so a deployment that
// exports neither variable still gets a real store instead of a permanent 503.
const (
	EnvChatMediaRoot        = "HCMNEXT_CHAT_MEDIA_ROOT"
	EnvArtifactRoot         = "HCMNEXT_ARTIFACT_ROOT"
	defaultArtifactRootPath = ".artifacts"
	chatMediaRootLeaf       = "chat-media"
)

// DefaultChatMediaArtifactRoot resolves explicit configuration values: a media
// root, else a child of the artifact root, else .artifacts/chat-media.
func DefaultChatMediaArtifactRoot(mediaRoot, artifactRoot string) string {
	if mediaRoot != "" {
		return mediaRoot
	}
	if artifactRoot != "" {
		return filepath.Join(artifactRoot, chatMediaRootLeaf)
	}
	return filepath.Join(defaultArtifactRootPath, chatMediaRootLeaf)
}

// WithDefaults fills the seams a deployment did not set: the artifact root from
// the artifact-root convention and the principal reader from the trusted edge.
// The scanner is never defaulted; an absent scanner must keep failing closed.
func (c ChatMediaConfig) WithDefaults(mediaRoot, artifactRoot string) ChatMediaConfig {
	if c.ArtifactRoot == "" {
		c.ArtifactRoot = DefaultChatMediaArtifactRoot(mediaRoot, artifactRoot)
	}
	if c.Principal == nil {
		c.Principal = trustedMediaPrincipal
	}
	return c
}

// ComposeChatMedia creates the protected media handler. It never supplies an
// accepting scanner: until malware inspection is configured, uploads fail
// closed with HTTP 503 after recording their rejection. The artifact root stays
// mandatory here so the seam is explicit; callers that want the convention's
// default apply WithDefaults first.
func ComposeChatMedia(cfg ChatMediaConfig) (http.Handler, error) {
	if cfg.ArtifactRoot == "" {
		return nil, errors.New("application: chat media artifact root is unavailable")
	}
	store, err := chatmedia.NewFilesystemStore(cfg.ArtifactRoot)
	if err != nil {
		return nil, err
	}
	service := chatmedia.New(chatmedia.Config{Store: store, Scanner: cfg.Scanner, Authorize: cfg.Authorize})
	principal := cfg.Principal
	if principal == nil {
		principal = trustedMediaPrincipal
	}
	return transportmedia.Handler{Service: service, Principal: principal}, nil
}

// ChatMediaDirectory adapts the media service to the chat service's
// commit-time attachment check. It lives here because it is the one place that
// already knows both: chatmedia must not learn the chat contract, and chat must
// not learn how media is stored.
type ChatMediaDirectory struct{ Media *chatmedia.Service }

// MediaArtifact implements chatcore.MediaDirectory.
func (d ChatMediaDirectory) MediaArtifact(ctx context.Context, tenantID, conversationID, artifactID string) (chatcore.MediaFacts, error) {
	if d.Media == nil {
		return chatcore.MediaFacts{}, chatcore.ErrUnavailable
	}
	ref, err := d.Media.Describe(ctx, tenantID, conversationID, artifactID)
	if err != nil {
		return chatcore.MediaFacts{}, err
	}
	return chatcore.MediaFacts{ContentType: string(ref.MediaType), ByteSize: uint64(ref.Size), Admitted: true}, nil
}

func trustedMediaPrincipal(r *http.Request) (tenant, conversation, principal string, ok bool) {
	if _, trusted := transport.InvocationFromContext(r.Context()); !trusted {
		return "", "", "", false
	}
	p, found := trust.FromContext(r.Context())
	if !found || p == nil {
		return "", "", "", false
	}
	tenant = r.URL.Query().Get("host_tenant_id")
	if tenant == "" {
		tenant = p.Tenant().String()
	}
	conversation = r.URL.Query().Get("conversation_id")
	if conversation == "" {
		return "", "", "", false
	}
	return tenant, conversation, p.Subject(), true
}

// ChatMediaHandler always returns a mountable endpoint. Configuration or
// scanner absence is surfaced as 503, after the request has passed the outer
// authenticated edge; it never fabricates a clean scan.
func ChatMediaHandler(cfg ChatMediaConfig) http.Handler {
	h, err := ComposeChatMedia(cfg)
	if err == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "chat media unavailable", http.StatusServiceUnavailable)
	})
}

// OverlayChatMedia mounts the media endpoint ahead of the existing edge. The
// admission is mandatory so every media request receives the same trusted
// context as the rest of the edge.
func OverlayChatMedia(next http.Handler, cfg ChatMediaConfig, admission transport.Config) http.Handler {
	mux := http.NewServeMux()
	h := ChatMediaHandler(cfg)
	mux.Handle(workspace.PathChatMediaPrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
