package application

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectrefs"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3791: the work a conversation has spawned. This is a viewer-authorized
// projection, not a list of everything keyed to the conversation. The
// conversation only remembers which proposals came from it; what each one is
// doing, and whether this person may know it exists, is asked of its owner
// under the reader's own authority every time the panel is read. A proposal the
// reader cannot open is left out (no row, no count, no placeholder). A source
// that cannot answer is reported as incomplete, never as an empty list or as
// done.

// ChatWorkIntentSource and ChatWorkTaskSource name the two owners the panel reads.
const (
	ChatWorkIntentSource = "approvals"
	ChatWorkTaskSource   = "tasks"
)

const chatLinkedWorkMax = 100

// ChatWorkLinkReader lists the proposals a conversation remembers.
type ChatWorkLinkReader interface {
	ListWorkLinks(ctx context.Context, tenant, conversation string, limit int) ([]chatstore.WorkLink, error)
}

// ChatWorkIntentReader reads one proposal as the person asking. ErrChatWorkNotFound
// (or ErrChatWorkDenied) means the person may not open it; ErrChatWorkUnavailable
// means the owner could not answer.
type ChatWorkIntentReader interface {
	ReadIntent(context.Context, *trust.Principal, string) (*intentsv1.IntentInstance, error)
}

// ChatWorkTasks is the tasks that conversation messages became.
type ChatWorkTasks interface {
	Linked(context.Context, *trust.Principal, string) ([]ChatWorkLink, error)
}

// ChatLinkedWorkItem is one thing the conversation spawned, as one reader may see it.
type ChatLinkedWorkItem struct {
	Kind       string `json:"kind"` // intent | task
	ID         string `json:"id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Source     string `json:"source"`
	ObservedAt string `json:"observed_at"`
	Revision   uint64 `json:"revision,omitempty"`
	Href       string `json:"href,omitempty"`
	Detail     string `json:"detail,omitempty"`
	OwnerID    string `json:"owner_id,omitempty"`
	DueDate    string `json:"due_date,omitempty"`
	ProjectID  string `json:"project_id,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
}

// ChatLinkedWorkResult is the panel. Incomplete names the sources that could
// not answer, so a short list is never mistaken for a complete one.
type ChatLinkedWorkResult struct {
	Items      []ChatLinkedWorkItem `json:"items"`
	Incomplete []string             `json:"incomplete"`
	AsOf       string               `json:"as_of"`
}

// ChatLinkedWork answers the conversation work panel.
type ChatLinkedWork struct {
	Chat    projectrefs.ChatLinks
	Links   ChatWorkLinkReader
	Intents ChatWorkIntentReader
	Tasks   ChatWorkTasks
	Now     func() time.Time
}

// List returns the work of one conversation for a person who can read it.
func (w *ChatLinkedWork) List(ctx context.Context, p *trust.Principal, conversationID string) (ChatLinkedWorkResult, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return ChatLinkedWorkResult{}, err
	}
	if strings.TrimSpace(conversationID) == "" {
		return ChatLinkedWorkResult{}, ErrChatWorkInvalid
	}
	if w == nil || w.Chat == nil || w.Links == nil || w.Intents == nil {
		return ChatLinkedWorkResult{}, ErrChatWorkUnavailable
	}
	// The room first: nothing about its work is read for a person who cannot open it.
	if _, _, err := w.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, conversationID, ""); err != nil {
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
			return ChatLinkedWorkResult{}, ErrChatWorkNotFound
		}
		return ChatLinkedWorkResult{}, ErrChatWorkUnavailable
	}
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	observed := now().UTC().Format(time.RFC3339)
	result := ChatLinkedWorkResult{Items: []ChatLinkedWorkItem{}, Incomplete: []string{}, AsOf: observed}

	incompleteIntents := false
	links, err := w.Links.ListWorkLinks(ctx, cp.TenantID, conversationID, chatLinkedWorkMax)
	if err != nil {
		incompleteIntents = true
	}
	for _, link := range links {
		if link.Kind != chatstore.WorkLinkIntentProposal {
			continue
		}
		inst, err := w.Intents.ReadIntent(ctx, p, link.SourceID)
		switch {
		case err == nil && inst != nil:
		case errors.Is(err, ErrChatWorkNotFound), errors.Is(err, ErrChatWorkDenied):
			continue
		default:
			incompleteIntents = true
			continue
		}
		intentType := inst.GetDefinition().GetIntentTypeId()
		if intentType == "" {
			intentType = link.IntentType
		}
		result.Items = append(result.Items, ChatLinkedWorkItem{
			Kind: "intent", ID: link.SourceID, Title: chatWorkHumanize(intentType), State: chatWorkIntentState(inst),
			Source: ChatWorkIntentSource, ObservedAt: observed, Revision: inst.GetInstanceVersion(),
			Href: "/workspace/app/journeys?journey=" + url.QueryEscape(link.SourceID),
		})
	}
	if incompleteIntents {
		result.Incomplete = append(result.Incomplete, ChatWorkIntentSource)
	}

	if w.Tasks != nil {
		tasks, err := w.Tasks.Linked(ctx, p, conversationID)
		if err != nil {
			result.Incomplete = append(result.Incomplete, ChatWorkTaskSource)
		}
		for _, task := range tasks {
			result.Items = append(result.Items, ChatLinkedWorkItem{
				Kind: "task", ID: task.TaskID, Title: task.Title, State: chatWorkTaskState(task.Category),
				Source: ChatWorkTaskSource, ObservedAt: observed, Detail: task.Status, OwnerID: task.OwnerID,
				DueDate: task.DueDate, ProjectID: task.ProjectID, TaskID: task.TaskID,
			})
		}
	}
	// What needs a person first, then what is moving, then what is finished.
	// The sort is stable, so within a group the newest proposal stays first.
	sort.SliceStable(result.Items, func(i, j int) bool {
		return chatWorkStateRank(result.Items[i].State) < chatWorkStateRank(result.Items[j].State)
	})
	return result, nil
}

func chatWorkStateRank(state string) int {
	switch state {
	case "awaiting_approval":
		return 0
	case "failed", "blocked":
		return 1
	case "proposed", "approved":
		return 2
	case "scheduled", "executing", "executed", "open":
		return 3
	case "unknown":
		return 4
	case "done":
		return 5
	}
	return 6
}

func chatWorkTaskState(category string) string {
	switch category {
	case "DONE":
		return "done"
	case "CANCELLED":
		return "closed"
	case "BLOCKED":
		return "blocked"
	case "NOT_STARTED", "ACTIVE":
		return "open"
	}
	return "unknown"
}

// chatWorkIntentState tells where a proposal stands without claiming more than
// its owner recorded. Done needs the business result completed and the effects
// reconciled; a committed write whose effects are still being observed is only
// "executed", and anything the owner left unsaid is "unknown".
func chatWorkIntentState(inst *intentsv1.IntentInstance) string {
	life := inst.GetLifecycle()
	if life == nil {
		return "unknown"
	}
	switch life.GetExecution() {
	case intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED, intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED:
		return "failed"
	case intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED:
		return "scheduled"
	case intentsv1.ExecutionState_EXECUTION_STATE_REVALIDATING, intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING:
		return "executing"
	case intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED:
		if life.GetBusiness() == intentsv1.BusinessState_BUSINESS_STATE_NOT_ACHIEVED {
			return "failed"
		}
		switch life.GetConsistency() {
		case intentsv1.ConsistencyState_CONSISTENCY_STATE_UNKNOWN, intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED,
			intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING, intentsv1.ConsistencyState_CONSISTENCY_STATE_UNSPECIFIED:
			return "unknown"
		}
		if life.GetBusiness() == intentsv1.BusinessState_BUSINESS_STATE_COMPLETED &&
			(life.GetConsistency() == intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT || life.GetConsistency() == intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE) {
			return "done"
		}
		return "executed"
	}
	switch life.GetRequest() {
	case intentsv1.RequestState_REQUEST_STATE_DRAFT, intentsv1.RequestState_REQUEST_STATE_PREFLIGHTED,
		intentsv1.RequestState_REQUEST_STATE_SIMULATED, intentsv1.RequestState_REQUEST_STATE_REOPENED:
		return "proposed"
	case intentsv1.RequestState_REQUEST_STATE_SUBMITTED:
		return "awaiting_approval"
	case intentsv1.RequestState_REQUEST_STATE_APPROVED:
		return "approved"
	case intentsv1.RequestState_REQUEST_STATE_REJECTED, intentsv1.RequestState_REQUEST_STATE_WITHDRAWN,
		intentsv1.RequestState_REQUEST_STATE_CANCELLED, intentsv1.RequestState_REQUEST_STATE_SUPERSEDED,
		intentsv1.RequestState_REQUEST_STATE_CLOSED:
		return "closed"
	}
	return "unknown"
}

// chatWorkHumanize turns an intent type id ("promotion.request") into words.
func chatWorkHumanize(id string) string {
	words := strings.Fields(strings.NewReplacer(".", " ", "_", " ", "-", " ", "/", " ").Replace(id))
	if len(words) == 0 {
		return "Proposed action"
	}
	text := strings.Join(words, " ")
	return strings.ToUpper(text[:1]) + text[1:]
}

// chatWorkIntentAdapter reads a proposal through the intent service as the
// caller, so the intent kernel's own read rule decides what this person sees.
type chatWorkIntentAdapter struct{ service *intentapp.IntentService }

func (a chatWorkIntentAdapter) ReadIntent(ctx context.Context, p *trust.Principal, id string) (*intentsv1.IntentInstance, error) {
	if a.service == nil || p == nil {
		return nil, ErrChatWorkUnavailable
	}
	ctx = trust.WithPrincipal(ctx, p)
	res, err := a.service.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: id})
	if err != nil {
		var owned *envelope.Error
		if errors.As(err, &owned) && (owned.Code() == envelope.CodeNotFound || owned.Code() == envelope.CodePermissionDenied) {
			return nil, ErrChatWorkNotFound
		}
		return nil, ErrChatWorkUnavailable
	}
	return res.GetIntent(), nil
}

// NewChatLinkedWork composes the panel over the chat store, the intent service
// and (when projects are served) the message tasks.
func NewChatLinkedWork(chatLinks projectrefs.ChatLinks, links ChatWorkLinkReader, intents *intentapp.IntentService, tasks *ChatMessageWork) *ChatLinkedWork {
	if chatLinks == nil || links == nil || intents == nil {
		return nil
	}
	w := &ChatLinkedWork{Chat: chatLinks, Links: links, Intents: chatWorkIntentAdapter{service: intents}}
	if tasks != nil {
		w.Tasks = tasks
	}
	return w
}
