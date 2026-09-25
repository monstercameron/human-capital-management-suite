//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc"
)

// chatDocuments is the document service chat reads unfurls and the
// composer's document suggestions from, on the workspace's connection.
var chatDocuments documentv1.DocumentServiceClient

func configureChatDocuments(conn grpc.ClientConnInterface, cfg journeyclient.Config) {
	chatDocuments = documentv1.NewDocumentServiceClient(conn)
	// C-1 (r4): start the read the moment the client exists. A render that
	// ran before this point found no client (or no viewer yet) and either
	// armed a one-second retry or returned, so the landing channel's first
	// paint showed a loading card for a second or more; resolving here makes
	// the cold path as fast as the warm one.
	ui.PostAsync(func() { resolveVisibleChatDocs(chatBrowser.config(cfg)) })
}

// resolveVisibleChatDocs runs after a chat render: it shows what the cache
// already knows and asks, in one batched read as the current viewer, for
// every document the screen references that it does not.
func resolveVisibleChatDocs(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	if active.Tenant == "" || active.Subject == "" {
		return
	}
	identity := active.Tenant + "\x00" + active.Subject
	// C-1 (r3): check the client before claiming. claim() marks every ID it
	// returns pending, and a pending ID is never reissued until the timeout
	// -- which claim() only evaluates when another render calls it -- so
	// claiming with no client to send the read left a quiet channel reading
	// "Loading document…" for good.
	client := chatDocuments
	if client == nil {
		armChatDocRetry(cfg, time.Second)
		return
	}
	claims, epoch, shown := chatDocPreviews.claim(identity, chatDocPreviewIDs(chatBrowser.snapshot()), time.Now())
	changed := false
	chatBrowser.mutate(func(model *chatui.Model) { changed = mergeChatDocPreviews(model, shown) })
	if changed {
		refreshChatRoute()
	}
	if len(claims) == 0 {
		return
	}
	// A read that never answers (dropped connection, viewer switch mid-flight)
	// still resolves: past the pending timeout this pass runs again without
	// waiting for an unrelated render, and claim() turns the stale entry into
	// a definite "unavailable" card or reissues it.
	armChatDocRetry(cfg, chatDocPreviewPendingTimeout+time.Second)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		response, err := client.GetDocumentPreviews(chatRPCContext(ctx, active), &documentv1.GetDocumentPreviewsRequest{DocumentIds: claims})
		cancel()
		answers := make(map[string]chatui.DocPreview, len(claims))
		for _, id := range claims {
			answers[id] = chatui.DocPreview{ID: id, State: "unavailable"}
		}
		if err == nil {
			locale := productui.ResolveProductLocale(active.Locale)
			names := chatDirectorySnapshot()
			for _, preview := range response.GetPreviews() {
				id := preview.GetDocumentId()
				if _, asked := answers[id]; !asked {
					continue
				}
				owner := strings.TrimSpace(preview.GetOwnerName())
				if owner == "" || owner == preview.GetOwnerId() {
					if name := names[preview.GetOwnerId()]; name != "" {
						owner = name
					}
				}
				updated := ""
				if at := preview.GetUpdatedAt(); at != nil && at.IsValid() {
					// C-9 (r3): the docs library's own short date ("Sep 3",
					// or "Sep 3, 2025" for another year), so the card and the
					// document it opens share one date vocabulary.
					updated = productui.ChatDocDateLabel(locale, at.AsTime(), time.Now())
				}
				answers[id] = chatDocPreviewAnswer(id, preview.GetReadable(), preview.GetTitle(), owner, updated, preview.GetSnippet())
			}
		}
		current := chatBrowser.config(cfg)
		if current.Tenant+"\x00"+current.Subject != identity {
			return
		}
		ui.PostAsync(func() {
			if !chatDocPreviews.finish(identity, epoch, answers, time.Now()) {
				return
			}
			changed := false
			chatBrowser.mutate(func(model *chatui.Model) { changed = mergeChatDocPreviews(model, answers) })
			if changed {
				refreshChatRoute()
			}
		})
	}()
}

// chatDocRetryArmed keeps at most one deferred re-resolve outstanding.
var chatDocRetryArmed atomic.Bool

// armChatDocRetry runs resolveVisibleChatDocs again after delay on the UI
// loop, unless a retry is already waiting.
func armChatDocRetry(cfg journeyclient.Config, delay time.Duration) {
	if !chatDocRetryArmed.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(delay, func() {
		ui.PostAsync(func() {
			chatDocRetryArmed.Store(false)
			resolveVisibleChatDocs(cfg)
		})
	})
}

// withChatDocCallbacks gives the composer its document autocomplete: a
// title search over the documents the viewer can open. The document
// service applies the same read rule as opening each one.
func withChatDocCallbacks(callbacks chatui.Callbacks, cfg journeyclient.Config) chatui.Callbacks {
	callbacks.SuggestDocuments = func(query string, done func([]chatui.DocSuggestion)) {
		client := chatDocuments
		active := chatBrowser.config(cfg)
		if client == nil {
			done(nil)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			items, err := searchChatDocuments(chatRPCContext(ctx, active), client, query)
			cancel()
			if err != nil {
				items = nil
			}
			ui.PostAsync(func() { done(items) })
		}()
	}
	return callbacks
}

func searchChatDocuments(ctx context.Context, client documentv1.DocumentServiceClient, query string) ([]chatui.DocSuggestion, error) {
	request := &documentv1.ListDocumentsRequest{PageSize: 8, Query: strings.TrimSpace(query)}
	if request.Query != "" {
		request.SearchMode = "contains"
	}
	response, err := client.ListDocuments(ctx, request)
	if err != nil {
		return nil, err
	}
	names := chatDirectorySnapshot()
	out := make([]chatui.DocSuggestion, 0, len(response.GetDocuments()))
	for _, document := range response.GetDocuments() {
		if document.GetDocumentId() == "" || strings.TrimSpace(document.GetTitle()) == "" {
			continue
		}
		detail := names[document.GetOwnerId()]
		out = append(out, chatui.DocSuggestion{ID: document.GetDocumentId(), Title: document.GetTitle(), Detail: detail})
	}
	return out, nil
}
