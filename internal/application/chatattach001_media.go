package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	mediahttp "github.com/monstercameron/human-capital-management-suite/internal/transport/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// This upload adapter calls the attachment port; reads continue through the
// existing grant boundary. Both use one durable store and one media authority.
type chatattach001HTTP struct {
	uploads       *chatmedia.Chatattach001Uploads
	principal     func(*http.Request) (string, string, string, bool)
	reads         http.Handler
	readAuthorize chatmedia.Authorizer
	slots         chan struct{}
}

func (h *chatattach001HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	t, c, p, ok := h.principal(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/upload") {
		if h.readAuthorize == nil || h.readAuthorize(r.Context(), chatmedia.AccessRequest{TenantID: t, ConversationID: c, PrincipalID: p}) != nil {
			http.Error(w, "forbidden", 403)
			return
		}
		if err := h.uploads.Retain(r.Context(), t); err != nil {
			http.Error(w, "media unavailable", 503)
			return
		}
		h.reads.ServeHTTP(w, r)
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "uploads busy", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, chatmedia.Chatattach001MaxBytes+(64<<10))
	parts, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid upload", 400)
		return
	}
	req := chatmedia.UploadRequest{TenantID: t, ConversationID: c, PrincipalID: p}
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "invalid upload", 400)
			return
		}
		if part.FormName() != "file" || req.Content != nil {
			_ = part.Close()
			http.Error(w, "one file required", 400)
			return
		}
		req.Filename, req.DeclaredType = part.FileName(), part.Header.Get("Content-Type")
		req.Content, err = io.ReadAll(io.LimitReader(part, chatmedia.Chatattach001MaxBytes+1))
		_ = part.Close()
		if int64(len(req.Content)) > chatmedia.Chatattach001MaxBytes {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			http.Error(w, "invalid upload", 400)
			return
		}
	}
	ref, err := h.uploads.Upload(r.Context(), req)
	if err != nil {
		status, message := 500, "upload failed; try again"
		switch {
		case errors.Is(err, chatmedia.ErrChatattach001Size):
			status, message = 413, "file too large"
		case errors.Is(err, chatmedia.ErrChatattach001Quota):
			status, message = 429, "attachment quota reached"
		case errors.Is(err, chatmedia.ErrUnsupported), errors.Is(err, chatmedia.ErrQuarantined):
			status, message = 415, "file type not allowed"
		case errors.Is(err, chatmedia.ErrUnauthorized):
			status, message = 403, "posting not allowed"
		case errors.Is(err, chatmedia.ErrScannerUnavailable):
			status, message = 503, "inspection unavailable; try again"
		case errors.Is(err, chatmedia.ErrInvalid):
			status, message = 400, "choose a file"
		}
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ref)
}

func chatattach001Linked(store *chatstore.Store) func(context.Context, chatmedia.Reference) (bool, error) {
	return func(ctx context.Context, ref chatmedia.Reference) (bool, error) {
		var found bool
		// Match only identity, rather than the zero-value snapshot fields.
		needle, _ := json.Marshal([]map[string]string{{"Kind": "MEDIA", "ID": ref.ArtifactID}})
		err := store.RunTenantTx(ctx, ref.TenantID, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_post p WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND (p.references_json @> $3::jsonb OR EXISTS(SELECT 1 FROM chat_post_revision r WHERE r.tenant_id=p.tenant_id AND r.post_id=p.id AND r.references_json @> $3::jsonb)))`, ref.TenantID, ref.ConversationID, string(needle)).Scan(&found)
		})
		return found, err
	}
}

func chatattach001PostAuthority(runtime composedChat) chatmedia.Authorizer {
	return func(ctx context.Context, r chatmedia.AccessRequest) error {
		if runtime.extensions == nil || runtime.core == nil || runtime.store == nil {
			return chat.ErrUnavailable
		}
		if err := runtime.extensions.AuthorizeMedia(ctx, r); err != nil {
			return err
		}
		p, ok := trust.FromContext(ctx)
		if !ok || p == nil {
			return chat.ErrPermissionDenied
		}
		principal := chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
		req := chat.GetConversationRequest{Principal: principal, TenantID: r.TenantID, ConversationID: r.ConversationID}
		c, err := runtime.service.GetConversation(ctx, req)
		if err != nil || c.Archived {
			return chat.ErrPermissionDenied
		}
		member, err := chatstore.NewAdapter(runtime.store).GetMembership(ctx, r.TenantID, r.ConversationID, principal.TenantID, principal.SubjectID)
		if err != nil || member.LeftAt != nil {
			return chat.ErrPermissionDenied
		}
		if c.Kind == chat.PublicChannel || c.Kind == chat.PrivateChannel {
			snapshot, err := runtime.core.GetChannelStatusSnapshot(ctx, req)
			if err != nil || !snapshot.CanPost {
				return chat.ErrPermissionDenied
			}
		}
		return nil
	}
}

// ChatAttachmentsAvailabilityPath answers whether this server takes uploads:
// {"uploads":true} where the upload service is composed, {"uploads":false}
// where it is not. Without a scanner every upload fails closed, and the page
// offered "Attach a file" all the same; the page now asks first and leaves the
// item out, and pasted or dropped files alone, where the answer is no. The
// answer is a fact about the deployment, the same for everyone signed in.
const ChatAttachmentsAvailabilityPath = workspace.PathChatMediaPrefix + "attachments/availability"

// chatattach001Availability serves the availability answer behind the same
// admission as the media routes and hands every other request on.
func chatattach001Availability(next http.Handler, admission transport.Config, uploads bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ChatAttachmentsAvailabilityPath {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge}); denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"uploads": uploads})
	})
}

func chatattach001Overlay(next http.Handler, cfg ChatMediaConfig, admission transport.Config, profile string, runtime composedChat) (http.Handler, error) {
	if profile != ServeProfileLocalDev && cfg.Scanner == nil {
		// The upload route stays mounted and stays closed; the page is told.
		return chatattach001Availability(OverlayChatMedia(next, cfg, admission), admission, false), nil
	}
	store, err := chatmedia.NewFilesystemStore(cfg.ArtifactRoot)
	if err != nil {
		return nil, err
	}
	scanner := chatmedia.Chatattach001Scanner{External: cfg.Scanner}
	media := chatmedia.New(chatmedia.Config{Store: store, Scanner: scanner, Authorize: cfg.Authorize, MaxBytes: chatmedia.Chatattach001MaxBytes})
	uploads := &chatmedia.Chatattach001Uploads{Media: media, Root: cfg.ArtifactRoot, Authorize: chatattach001PostAuthority(runtime), Linked: chatattach001Linked(runtime.store)}
	if runtime.core != nil {
		runtime.core.SetMediaDirectory(chatattach001Directory{media: media})
	}
	h := &chatattach001HTTP{uploads: uploads, principal: cfg.Principal, reads: mediahttp.NewHandler(media, cfg.Principal), readAuthorize: cfg.Authorize, slots: make(chan struct{}, 4)}
	mux := http.NewServeMux()
	mux.Handle(workspace.PathChatMediaPrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _, err := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if err != nil {
			http.Error(w, "request denied", err.HTTPStatus())
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	mux.Handle("/", next)
	return chatattach001Availability(mux, admission, true), nil
}

type chatattach001Directory struct{ media *chatmedia.Service }

func (d chatattach001Directory) MediaArtifact(ctx context.Context, tenant, conversation, id string) (chat.MediaFacts, error) {
	ref, err := d.media.Chatattach001Describe(ctx, tenant, conversation, id)
	if err != nil {
		return chat.MediaFacts{}, err
	}
	return chat.MediaFacts{ContentType: string(ref.MediaType), ByteSize: uint64(ref.Size), Admitted: ref.State == chatmedia.StateAdmitted}, nil
}

func chatattach001AttachmentTypes(next func(context.Context, chat.ContentInput) ([]string, error)) func(context.Context, chat.ContentInput) ([]string, error) {
	return func(ctx context.Context, in chat.ContentInput) ([]string, error) {
		count := 0
		for _, ref := range in.References {
			if ref.Kind == chat.MediaAttachment {
				count++
			}
		}
		if count > 10 {
			return nil, chat.ErrInvalidArgument
		}
		if next == nil {
			return nil, chat.ErrUnavailable
		}
		return next(ctx, in)
	}
}
