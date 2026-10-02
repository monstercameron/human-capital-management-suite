package main

import (
	"context"
	"strings"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
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
		versions, versionErr := client.ListDocumentVersions(ctx, &documentv1.ListDocumentVersionsRequest{DocumentId: document.GetDocumentId()})
		if versionErr != nil {
			// One listed document whose versions cannot be read (it was
			// removed or is no longer readable between the two calls) is left
			// out. It must not turn every other match into "search is
			// unavailable". A cancelled or expired search still fails as a whole.
			if ctx.Err() != nil {
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
