package agentinvoke

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	MaxThreadPosts          = 50
	TaintUntrustedPeer      = "UNTRUSTED_PEER"
	TaintInvokerInstruction = "INVOKER_INSTRUCTION"
)

// ThreadReader is a typed adapter over Chat's current visibility checks. It
// must return only posts the invoker can read; this package then rejects rows
// that escape the requested conversation or thread.
type ThreadReader interface {
	ReadThread(context.Context, ThreadReadRequest) ([]ThreadPost, error)
}

type ThreadReadRequest struct {
	TenantID, ConversationID, ThreadID, InvokerID, InvokingPostID string
	Limit                                                         int
}

type ThreadPost struct {
	TenantID, ConversationID, ThreadID, ID, AuthorID, Body string
	Bot                                                    bool
	Files                                                  []ThreadAttachment
	Embeds                                                 []ThreadAttachment
}

type ThreadAttachment struct {
	ID, Digest string
}

// PeerExtractor is intentionally typed. An implementation may use the
// SchemaFlux-backed quarantine gateway, but it cannot return free-form peer
// instructions to the planner.
type PeerExtractor interface {
	Extract(context.Context, PeerExtractionRequest) (PeerExtraction, error)
}

type PeerExtractionRequest struct {
	PostID, AuthorID, Digest, Content string
}

type PeerExtraction struct {
	SchemaID, SchemaVersion string
	Values                  map[string]string
	SourceDigest            string
}

type ContextEntry struct {
	PostID       string
	AuthorID     string
	Digest       string
	Taint        string
	Extraction   PeerExtraction
	Attachment   bool
	AttachmentID string
}

type BoundContext struct {
	Goal    string
	Entries []ContextEntry
}

// BuildContext binds a persona to exactly one invoker's readable thread.
// Peer bodies never enter BoundContext; only their digest and optional
// schema-bound extraction do. The invoker's post is the sole goal source.
func (s *Service) BuildContext(ctx context.Context, invocation Invocation, goal string) (BoundContext, error) {
	if s == nil || s.threads == nil {
		return BoundContext{}, fmt.Errorf("%w: thread reader is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(goal) == "" {
		return BoundContext{}, fmt.Errorf("%w: invoking goal is required", ErrInvalidRequest)
	}
	posts, err := s.threads.ReadThread(ctx, ThreadReadRequest{TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, InvokerID: invocation.InvokerID, InvokingPostID: invocation.PostID, Limit: MaxThreadPosts})
	if err != nil {
		return BoundContext{}, err
	}
	if len(posts) > MaxThreadPosts {
		return BoundContext{}, fmt.Errorf("%w: thread reader returned more than %d posts", ErrInvalidRequest, MaxThreadPosts)
	}
	entries := make([]ContextEntry, 0, len(posts))
	foundInvoker := false
	for _, post := range posts {
		if post.TenantID != invocation.TenantID || post.ConversationID != invocation.ConversationID || post.ThreadID != invocation.ThreadID || strings.TrimSpace(post.ID) == "" {
			return BoundContext{}, fmt.Errorf("%w: thread post escaped the invocation conversation", ErrInvalidRequest)
		}
		digest := digestText(post.Body)
		if post.ID == invocation.PostID && post.AuthorID == invocation.InvokerID && !post.Bot {
			foundInvoker = true
			entries = append(entries, ContextEntry{PostID: post.ID, AuthorID: post.AuthorID, Digest: digest, Taint: TaintInvokerInstruction})
		} else {
			entry := ContextEntry{PostID: post.ID, AuthorID: post.AuthorID, Digest: digest, Taint: TaintUntrustedPeer}
			if s.peerExtractor != nil {
				extraction, extractErr := s.peerExtractor.Extract(ctx, PeerExtractionRequest{PostID: post.ID, AuthorID: post.AuthorID, Digest: digest, Content: post.Body})
				if extractErr != nil {
					return BoundContext{}, extractErr
				}
				if extraction.SourceDigest != digest {
					return BoundContext{}, fmt.Errorf("%w: peer extraction is not bound to its post digest", ErrInvalidRequest)
				}
				if strings.TrimSpace(extraction.SchemaID) == "" || strings.TrimSpace(extraction.SchemaVersion) == "" {
					return BoundContext{}, fmt.Errorf("%w: peer extraction must declare a schema", ErrInvalidRequest)
				}
				entry.Extraction = cloneExtraction(extraction)
			}
			entries = append(entries, entry)
			for _, attachment := range append(slicesClone(post.Files), post.Embeds...) {
				attachmentDigest := attachment.Digest
				if strings.TrimSpace(attachmentDigest) == "" {
					attachmentDigest = digestText(attachment.ID)
				}
				entries = append(entries, ContextEntry{PostID: post.ID, AuthorID: post.AuthorID, Digest: attachmentDigest, Taint: TaintUntrustedPeer, Attachment: true, AttachmentID: attachment.ID})
			}
		}
	}
	if !foundInvoker {
		entries = append(entries, ContextEntry{PostID: invocation.PostID, AuthorID: invocation.InvokerID, Digest: digestText(goal), Taint: TaintInvokerInstruction})
	}
	return BoundContext{Goal: goal, Entries: entries}, nil
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func slicesClone(in []ThreadAttachment) []ThreadAttachment {
	out := make([]ThreadAttachment, len(in))
	copy(out, in)
	return out
}

func cloneExtraction(in PeerExtraction) PeerExtraction {
	in.Values = copyStringMap(in.Values)
	return in
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
