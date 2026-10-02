package main

import (
	"context"
	"strings"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func searchAgentRequestDocumentSuggestions(ctx context.Context, client documentv1.DocumentServiceClient, query, subject string, locale productui.LocaleContext, now time.Time) ([]productui.AgentRequestDocumentSuggestion, error) {
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
	documents := response.GetDocuments()
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		if document.GetDocumentId() != "" {
			ids = append(ids, document.GetDocumentId())
		}
	}
	previews, err := client.GetDocumentPreviews(ctx, &documentv1.GetDocumentPreviewsRequest{DocumentIds: ids})
	if err != nil {
		return nil, err
	}
	previewByID := make(map[string]*documentv1.DocumentPreview, len(previews.GetPreviews()))
	for _, preview := range previews.GetPreviews() {
		previewByID[preview.GetDocumentId()] = preview
	}
	folders := map[string]string{}
	if library, libraryErr := client.GetDocumentLibrary(ctx, &documentv1.GetDocumentLibraryRequest{}); libraryErr == nil {
		for _, folder := range library.GetFolders() {
			folders[folder.GetFolderId()] = folder.GetName()
		}
	}
	items := make([]productui.AgentRequestDocumentSuggestion, 0, len(documents))
	for _, document := range documents {
		preview := previewByID[document.GetDocumentId()]
		if preview == nil || !preview.GetReadable() || strings.TrimSpace(document.GetTitle()) == "" {
			continue
		}
		versions, versionErr := client.ListDocumentVersions(ctx, &documentv1.ListDocumentVersionsRequest{DocumentId: document.GetDocumentId()})
		if versionErr != nil {
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
		owner := strings.TrimSpace(preview.GetOwnerName())
		if preview.GetOwnerId() == subject {
			owner = locale.Text("agents.document_owner_you")
		}
		updatedAt := document.GetUpdatedAt()
		if preview.GetUpdatedAt() != nil && preview.GetUpdatedAt().IsValid() {
			updatedAt = preview.GetUpdatedAt()
		}
		updated := ""
		if updatedAt != nil && updatedAt.IsValid() {
			updated = productui.ChatDocDateLabel(locale, updatedAt.AsTime(), now)
		}
		snippet := ""
		if strings.EqualFold(document.GetMatch(), "text") {
			snippet = strings.TrimSpace(document.GetSnippet())
			if snippet == "" {
				snippet = strings.TrimSpace(preview.GetSnippet())
			}
		}
		items = append(items, productui.AgentRequestDocumentSuggestion{
			DocumentID: document.GetDocumentId(), Title: document.GetTitle(), Owner: owner, Folder: folders[document.GetFolderId()],
			Updated: updated, Snippet: snippet, PublishedVersion: published,
		})
	}
	return items, nil
}
