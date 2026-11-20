//go:build js && wasm

package main

import (
	"context"
	"sync"
	"time"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3973, browser half. handoverCommand runs one step of the panel; when
// the person asks to prepare, handoverPrepare re-reads every ticked task from
// Projects as the author, asks Projects who belongs to each task's project,
// and builds the brief. All reads are the person's own calls, so Projects
// decides what they may see; nothing is written anywhere.

var (
	handoverMu  sync.Mutex
	handoverRun uint64
)

const (
	handoverReadTimeout  = 30 * time.Second
	handoverMemberPages  = 10
	handoverReadParallel = 6
)

func handoverCommand(cfg journeyclient.Config, cmd chatui.HandoverCommand) {
	conversationID := chatBrowser.selectedID()
	if conversationID == "" {
		return
	}
	prepare := false
	chatBrowser.mutate(func(m *chatui.Model) {
		m.Handover, prepare = handoverApply(m.Handover, conversationID, cmd, chatui.HandoverCandidates(*m), chatui.HandoverRecipients(*m))
	})
	refreshChatRoute()
	if prepare {
		go handoverPrepare(cfg, conversationID)
	}
}

func handoverFail(conversationID string, run uint64) {
	handoverMu.Lock()
	defer handoverMu.Unlock()
	if run != handoverRun {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == conversationID && m.Handover.Stage == "preparing" {
			m.Handover.Stage = "error"
		}
	})
	refreshChatRoute()
}

// handoverMembership asks whether the colleague is an active member of one
// project. A refused or incomplete read is "unknown".
func handoverMembership(ctx context.Context, active journeyclient.Config, client projectv1.ProjectServiceClient, projectID, recipient string) string {
	cursor := ""
	for page := 0; page < handoverMemberPages; page++ {
		response, err := client.ListProjectMembers(chatRPCContext(ctx, active), &projectv1.ListProjectMembersRequest{ProjectId: projectID, PageSize: 100, PageCursor: cursor})
		if err != nil {
			return "unknown"
		}
		if handoverRecipientAccess(response.GetMembers(), recipient) == "yes" {
			return "yes"
		}
		if cursor = response.GetNextPageCursor(); cursor == "" {
			return "no"
		}
	}
	return "unknown"
}

func handoverPrepare(cfg journeyclient.Config, conversationID string) {
	handoverMu.Lock()
	handoverRun++
	run := handoverRun
	handoverMu.Unlock()
	model := chatBrowser.snapshot()
	view := model.Handover
	client := chatProjects
	active := chatBrowser.config(cfg)
	if client == nil || view.ConversationID != conversationID {
		handoverFail(conversationID, run)
		return
	}
	var refs []chatui.ProjectTaskReference
	for _, candidate := range chatui.HandoverCandidates(model) {
		if view.Selected[candidate.Key] && candidate.Readable {
			refs = append(refs, chatui.ProjectTaskReference{ProjectID: candidate.ProjectID, TaskID: candidate.TaskID})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), handoverReadTimeout)
	defer cancel()
	items := make([]chatui.HandoverItem, len(refs))
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		membership = map[string]string{}
		semaphore  = make(chan struct{}, handoverReadParallel)
	)
	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			item := chatui.HandoverItem{ProjectID: ref.ProjectID, TaskID: ref.TaskID, Recipient: "unknown",
				Shown: model.ProjectTaskPreviews[chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)]}
			item.Fresh = resolveChatProjectPreview(ctx, active, client, ref)
			if item.Fresh.State == "ready" && item.Fresh.Readable {
				mu.Lock()
				answer, known := membership[ref.ProjectID]
				mu.Unlock()
				if !known {
					answer = handoverMembership(ctx, active, client, ref.ProjectID, view.Recipient)
					mu.Lock()
					membership[ref.ProjectID] = answer
					mu.Unlock()
				}
				item.Recipient = answer
			}
			items[i] = item
		}()
	}
	wg.Wait()
	result := chatui.BuildHandover(chatui.HandoverInput{Locale: model.Locale, Origin: model.EmbedOrigin, RecipientName: view.RecipientName, From: view.From, To: view.To, Now: time.Now(), Items: items})
	handoverMu.Lock()
	defer handoverMu.Unlock()
	if run != handoverRun {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		if m.SelectedID == conversationID && m.Handover.Stage == "preparing" {
			m.Handover.Stage, m.Handover.Text, m.Handover.Rows, m.Handover.Status = "draft", result.Text, result.Rows, ""
		}
	})
	refreshChatRoute()
	chatui.FocusHandoverText()
}
