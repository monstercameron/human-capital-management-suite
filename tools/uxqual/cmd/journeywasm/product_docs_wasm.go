//go:build js && wasm

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// wireDocumentLibrary connects the Docs library's organization and sharing
// actions to the document service. Every callback runs its RPC off the event
// loop, answers on it, and then re-reads the route so counts, folders and
// stars come from the server rather than from local guesses.
func wireDocumentLibrary(view *productui.View, service documentv1.DocumentServiceClient, authorize func(context.Context) context.Context, parent context.Context) {
	run := func(call func(context.Context) error, done func(error), refresh bool) {
		go func() {
			ctx, cancel := context.WithTimeout(authorize(parent), 20*time.Second)
			err := call(ctx)
			cancel()
			ui.PostAsync(func() {
				done(err)
				if err == nil && refresh {
					docsQuietRefresh()
				}
			})
		}()
	}
	view.CreateDocumentFolder = func(name string, done func(productui.DocumentFolder, error)) {
		var folder productui.DocumentFolder
		run(func(ctx context.Context) error {
			response, err := service.CreateDocumentFolder(ctx, &documentv1.CreateDocumentFolderRequest{Name: name})
			if err != nil {
				return err
			}
			if response.GetFolder().GetFolderId() == "" {
				return errors.New("document folder create returned no folder")
			}
			folder = productui.DocumentFolder{ID: response.GetFolder().GetFolderId(), Name: response.GetFolder().GetName(), Count: int(response.GetFolder().GetDocumentCount())}
			return nil
		}, func(err error) { done(folder, err) }, true)
	}
	view.RenameDocumentFolder = func(folderID, name string, done func(error)) {
		run(func(ctx context.Context) error {
			_, err := service.RenameDocumentFolder(ctx, &documentv1.RenameDocumentFolderRequest{FolderId: folderID, Name: name})
			return err
		}, done, true)
	}
	view.DeleteDocumentFolder = func(folderID string, done func(error)) {
		run(func(ctx context.Context) error {
			_, err := service.DeleteDocumentFolder(ctx, &documentv1.DeleteDocumentFolderRequest{FolderId: folderID})
			return err
		}, done, true)
	}
	view.MoveDocuments = func(documentIDs []string, folderID string, done func(error)) {
		run(func(ctx context.Context) error {
			_, err := service.MoveDocuments(ctx, &documentv1.MoveDocumentsRequest{DocumentIds: documentIDs, FolderId: folderID})
			return err
		}, done, true)
	}
	view.SetDocumentStarred = func(documentID string, starred bool, done func(error)) {
		run(func(ctx context.Context) error {
			_, err := service.SetDocumentStarred(ctx, &documentv1.SetDocumentStarredRequest{DocumentId: documentID, Starred: starred})
			return err
		}, done, true)
	}
	view.ListDocumentAccess = func(documentID string, done func([]productui.DocumentAccessEntry, error)) {
		var entries []productui.DocumentAccessEntry
		run(func(ctx context.Context) error {
			response, err := service.ListDocumentAccess(ctx, &documentv1.ListDocumentAccessRequest{DocumentId: documentID})
			if err != nil {
				return err
			}
			for _, entry := range response.GetEntries() {
				if entry.GetSubjectKind() != "" && entry.GetSubjectKind() != "person" {
					continue
				}
				entries = append(entries, productui.DocumentAccessEntry{SubjectID: entry.GetSubjectId(), Role: entry.GetRole(), Removable: entry.GetRemovable()})
			}
			return nil
		}, func(err error) { done(entries, err) }, false)
	}
	view.RevokeDocumentAccess = func(documentID, subjectID string, done func(error)) {
		run(func(ctx context.Context) error {
			_, err := service.RevokeDocumentAccess(ctx, &documentv1.RevokeDocumentAccessRequest{DocumentId: documentID, SubjectKind: "person", SubjectId: subjectID})
			return err
		}, done, true)
	}
	view.ResolveDocumentComment = func(documentID, commentID string, resolved bool, done func(error)) {
		run(func(ctx context.Context) error {
			_, err := service.ResolveDocumentComment(ctx, &documentv1.ResolveDocumentCommentRequest{DocumentId: documentID, CommentId: commentID, Resolved: resolved})
			return err
		}, done, true)
	}
	if share := view.ShareDocument; share != nil {
		view.ShareDocument = func(request productui.DocumentShareRequest, done func(error)) {
			share(request, func(err error) {
				ui.PostAsync(func() {
					done(err)
					if err == nil {
						docsQuietRefresh()
					}
				})
			})
		}
	}
}

// docsReplaceRoute re-runs the product route in place (history replace);
// installed once the product router exists.
var docsReplaceRoute func(string)

// docsQuietRefresh re-reads the route in place after a Docs action: the page,
// its open dialogs and local state stay mounted while counts, stars, folders
// and access labels catch up, instead of the page blinking through a load.
func docsQuietRefresh() {
	if productRouteRetry != nil {
		productQuietRefreshPending = true
		productRouteRetry()
	}
}

// docsLastListHref is the most recent library address, so "Back to
// documents" returns to the same search, filters, sort and page.
var docsLastListHref string

func rememberDocsListHref(view *productui.View) {
	current := currentPath() + "?" + currentQuery()
	if strings.TrimSpace(view.DocumentID) == "" {
		docsLastListHref = strings.TrimSuffix(current, "?")
		return
	}
	view.DocumentReturnHref = docsLastListHref
}
