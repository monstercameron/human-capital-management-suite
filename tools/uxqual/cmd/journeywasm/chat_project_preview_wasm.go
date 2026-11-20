//go:build js && wasm

package main

import (
	"context"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// resolveChatProjectPreviews reads many shared project addresses as the
// current viewer in one batched GetProjectPreviews call (issue 3751). A task
// link becomes a task card (status, priority, due date and assignee from the
// task, its project's name and workflow); a board link becomes a board card
// with task counts. The server classes each item: a refusal is the neutral
// restricted answer, a deleted record is missing, an archived one is archived,
// and any other failed read is retryable (see chatProjectBatchAnswers).
func resolveChatProjectPreviews(ctx context.Context, active journeyclient.Config, client projectv1.ProjectServiceClient, refs []chatui.ProjectTaskReference) map[string]chatui.ProjectTaskPreview {
	if len(refs) == 0 {
		return map[string]chatui.ProjectTaskPreview{}
	}
	response, err := client.GetProjectPreviews(chatRPCContext(ctx, active), chatProjectPreviewRequest(refs))
	directory := func() (map[string]string, map[string]string) { return projectDirectory(ctx, active) }
	return chatProjectBatchAnswers(refs, response, err, active.Subject, directory)
}

// resolveChatProjectPreview reads one shared project address through the same
// batched call.
func resolveChatProjectPreview(ctx context.Context, active journeyclient.Config, client projectv1.ProjectServiceClient, ref chatui.ProjectTaskReference) chatui.ProjectTaskPreview {
	return resolveChatProjectPreviews(ctx, active, client, []chatui.ProjectTaskReference{ref})[chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)]
}
