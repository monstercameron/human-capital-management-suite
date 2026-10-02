package main

import (
	"context"
	"errors"
	"strings"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// searchPersonaAdminDocuments adapts the documentation hub's existing
// authorized list and version calls into the reusable picker model. The
// caller always runs it away from the browser callback.
func searchPersonaAdminDocuments(ctx context.Context, client documentv1.DocumentServiceClient, query string, locale productui.LocaleContext, now time.Time) ([]productui.AgentDocumentSuggestion, error) {
	if client == nil {
		return nil, context.Canceled
	}
	request := &documentv1.ListDocumentsRequest{PageSize: 8, Query: strings.TrimSpace(query)}
	if request.Query != "" {
		request.SearchMode = "contains"
	}
	response, err := client.ListDocuments(ctx, request)
	if err != nil {
		return nil, err
	}
	folders := map[string]string{}
	if library, libraryErr := client.GetDocumentLibrary(ctx, &documentv1.GetDocumentLibraryRequest{}); libraryErr == nil {
		for _, folder := range library.GetFolders() {
			folders[folder.GetFolderId()] = folder.GetName()
		}
	}
	items := make([]productui.AgentDocumentSuggestion, 0, len(response.GetDocuments()))
	for _, document := range response.GetDocuments() {
		if document.GetDocumentId() == "" || strings.TrimSpace(document.GetTitle()) == "" {
			continue
		}
		// Each document's versions are read under their own short deadline, so
		// one document that never answers costs a few seconds, not the search.
		versionCtx, cancel := context.WithTimeout(ctx, personaDocumentVersionTimeout)
		versions, versionErr := client.ListDocumentVersions(versionCtx, &documentv1.ListDocumentVersionsRequest{DocumentId: document.GetDocumentId()})
		cancel()
		if versionErr != nil {
			// One listed document whose versions cannot be read (it was
			// removed, is no longer readable, or did not answer in time) is
			// left out. It must not turn every other match into "search is
			// unavailable". Only a search that itself was cancelled or ran out
			// of time fails, and then only if it has found nothing to offer.
			if ctx.Err() != nil {
				if len(items) > 0 {
					return items, nil
				}
				return nil, ctx.Err()
			}
			continue
		}
		published := uint64(0)
		for index, version := range versions.GetVersions() {
			if version.GetIsCurrent() {
				published = uint64(index + 1)
				break
			}
		}
		if published == 0 {
			continue
		}
		location := folders[document.GetFolderId()]
		if location == "" {
			location = strings.TrimSpace(document.GetScopeId())
		}
		if location == "" {
			location = strings.TrimSpace(document.GetScopeKind())
		}
		updated := ""
		if at := document.GetUpdatedAt(); at != nil && at.IsValid() {
			prefix := map[string]string{"de-DE": "Aktualisiert", "ar": "آخر تحديث"}[locale.Resolved]
			if prefix == "" {
				prefix = "Updated"
			}
			updated = prefix + " " + productui.ChatDocDateLabel(locale, at.AsTime(), now)
		}
		items = append(items, productui.AgentDocumentSuggestion{DocumentID: document.GetDocumentId(), Title: document.GetTitle(), Location: location, Updated: updated, PublishedVersion: published})
	}
	return items, nil
}

// personaDocumentVersionTimeout bounds the read of one document's versions.
const personaDocumentVersionTimeout = 3 * time.Second

// personaDocumentSearchFailure names why a search failed, so the picker can
// say what went wrong instead of one sentence for every cause: "timeout" when
// the hub did not answer in time, "denied" when the person may not list its
// documents, "unavailable" when the hub cannot be reached, and "" otherwise.
func personaDocumentSearchFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.PermissionDenied, codes.Unauthenticated:
		return "denied"
	case codes.Unavailable:
		return "unavailable"
	}
	return ""
}

// personaDocumentSearchFailureAttribute is the picker attribute that holds the
// sentence for one failure kind.
func personaDocumentSearchFailureAttribute(kind string) string {
	switch kind {
	case "timeout":
		return "agentdocFailedTimeout"
	case "denied":
		return "agentdocFailedDenied"
	case "unavailable":
		return "agentdocFailedUnavailable"
	}
	return "agentdocFailed"
}

func personaDocumentSuggestionLabels(items []productui.AgentDocumentSuggestion) map[string]string {
	counts := make(map[string]int, len(items))
	for _, item := range items {
		counts[strings.ToLower(strings.TrimSpace(item.Title))]++
	}
	labels := make(map[string]string, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Title)
		if counts[strings.ToLower(label)] > 1 && strings.TrimSpace(item.Location) != "" {
			label += " (" + strings.TrimSpace(item.Location) + ")"
		}
		labels[item.DocumentID] = label
	}
	return labels
}
