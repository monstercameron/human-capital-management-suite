package application

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3998: ask for informal input during a governed review.
//
// The reviewer who holds the decision on a workflow case asks someone for
// advice, in a conversation the reviewer can already post in, about one
// proposal revision. The question is labelled as informal and carries the
// revision it addresses. The reviewer sees the exact message and who can read
// it before anything is sent (a preview is the same call without the send).
//
// This is advice, never an approval. Nothing here can reach the workflow: the
// use case reads the case and writes a chat message and a record of it, and its
// journey port has no write method. A reply, reaction or edit in chat never
// counts as a decision and never adds an approver; when a case needs another
// approver, the owning workflow's own routing does that, not Chat.
//
// The person's name and nothing else about the case is shared, and only when
// the reviewer chooses to, and never into a public channel. Pay, dates and
// reasons are never copied into chat. Authorization comes first: the case must
// be open to the viewer, the viewer must hold its approval now, the conversation
// must be readable and postable by the viewer, and the revision must be current.

var (
	// ErrInputRequestStale means the proposal changed since the reviewer read it.
	ErrInputRequestStale = errors.New("input request: the proposal changed since it was reviewed")
	// ErrInputRequestClosed means the case no longer asks for a decision.
	ErrInputRequestClosed = errors.New("input request: the case is closed")
	// ErrInputRequestConversation means the conversation cannot be used.
	ErrInputRequestConversation = errors.New("input request: the conversation cannot be used")
	// ErrInputRequestPublicName means the person's name may not go to a public channel.
	ErrInputRequestPublicName = errors.New("input request: the case name cannot be shared in a public channel")
)

const (
	inputRequestQuestionMax = 1000
	inputRequestLabelMax    = 160
)

// InputRequestJourneys reads one journey as the person in the context. It has
// no write method on purpose.
type InputRequestJourneys interface {
	Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error)
}

// InputRequestChat is the conversation service as this feature uses it.
type InputRequestChat interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	SendPost(context.Context, chat.SendPostRequest) (chat.Post, error)
}

// InputRequestStore keeps the record of each request.
type InputRequestStore interface {
	RecordInputRequest(context.Context, chatstore.InputRequest) (chatstore.InputRequest, bool, error)
	ListInputRequests(ctx context.Context, tenant, journeyID, asker string, limit int) ([]chatstore.InputRequest, error)
}

// InputRequestAudience says who can read a question before it is sent. Kind is
// direct, group, private_channel or public_channel. Size is how many people
// the conversation has (zero when unknown).
type InputRequestAudience struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Size int    `json:"size,omitempty"`
}

// InputRequestItem is one request as the reviewer sees it. Kind is always
// "informal": a request here is advice, not an approval. State is "current"
// (about the revision under review now), "outdated" (the proposal changed since,
// so the advice must be reviewed again), "closed" (the case is closed) or
// "unavailable" (the conversation can no longer be opened, so no advice from
// it is current).
type InputRequestItem struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	AskedAt          string `json:"asked_at"`
	Revision         string `json:"revision"`
	State            string `json:"state"`
	AudienceKind     string `json:"audience_kind"`
	AudienceSize     int    `json:"audience_size,omitempty"`
	SharedCaseName   bool   `json:"shared_case_name,omitempty"`
	ConversationID   string `json:"conversation_id,omitempty"`
	ConversationName string `json:"conversation_name,omitempty"`
	Href             string `json:"href,omitempty"`
}

// InputRequestView is what one viewer may see of a case's input requests.
type InputRequestView struct {
	CanAsk     bool               `json:"can_ask"`
	CaseOpen   bool               `json:"case_open"`
	Status     string             `json:"status"`
	Revision   string             `json:"revision,omitempty"`
	CaseName   string             `json:"case_name,omitempty"`
	Items      []InputRequestItem `json:"items"`
	Incomplete bool               `json:"incomplete"`
}

// InputRequestAsk is one reviewed question. Revision is the proposal revision
// the reviewer read. Label and RevisionLabel are the localized words the
// reviewer saw in the preview ("informal input, not an approval", "Proposal
// revision ..."); RevisionLabel must name the revision. Preview asks for the
// exact message and audience without sending.
type InputRequestAsk struct {
	JourneyID, Revision, Question, ConversationID string
	Label, RevisionLabel, IdempotencyKey          string
	IncludeCaseName, Preview                      bool
}

// InputRequestAsked is the answer to an ask. For a preview, Item is zero and
// nothing was written; Message is exactly what would be posted.
type InputRequestAsked struct {
	Preview  bool                 `json:"preview"`
	Message  string               `json:"message"`
	Audience InputRequestAudience `json:"audience"`
	Revision string               `json:"revision"`
	Item     *InputRequestItem    `json:"item,omitempty"`
	Created  bool                 `json:"-"`
}

// InputRequests is the use case behind the input request routes.
type InputRequests struct {
	Journeys InputRequestJourneys
	Chat     InputRequestChat
	Store    InputRequestStore
	Now      func() time.Time
}

func (c *InputRequests) ready() bool {
	return c != nil && c.Journeys != nil && c.Chat != nil && c.Store != nil
}

func (c *InputRequests) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *InputRequests) open(ctx context.Context, p *trust.Principal, journeyID string) (workspace.JourneyDetail, error) {
	if strings.TrimSpace(journeyID) == "" || len(journeyID) > 128 || journeyID != strings.TrimSpace(journeyID) || strings.Contains(journeyID, "/") {
		return workspace.JourneyDetail{}, ErrChatWorkInvalid
	}
	detail, err := c.Journeys.Inspect(trust.WithPrincipal(ctx, p), journeyID)
	switch {
	case err == nil:
		return detail, nil
	case errors.Is(err, workspace.ErrJourneyUnknown):
		return workspace.JourneyDetail{}, ErrChatWorkNotFound
	}
	return workspace.JourneyDetail{}, ErrChatWorkUnavailable
}

func inputRequestAudienceKind(k chat.ConversationKind) string {
	switch k {
	case chat.Direct:
		return "direct"
	case chat.Group:
		return "group"
	case chat.PrivateChannel:
		return "private_channel"
	}
	return "public_channel"
}

func inputRequestHref(conversation string) string {
	return "/workspace/app/chat#channel=" + url.QueryEscape(conversation)
}

func inputRequestState(stage workspace.JourneyStage, rowRevision, current string) string {
	switch {
	case closedJourneyStage(stage):
		return "closed"
	case rowRevision != current:
		return "outdated"
	}
	return "current"
}

// View answers what the viewer may see: whether they may ask (and the case and
// revision if so), who holds the next action, and the requests they made.
func (c *InputRequests) View(ctx context.Context, p *trust.Principal, journeyID string) (InputRequestView, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return InputRequestView{}, err
	}
	if !c.ready() {
		return InputRequestView{}, ErrChatWorkUnavailable
	}
	detail, err := c.open(ctx, p, journeyID)
	if err != nil {
		return InputRequestView{}, err
	}
	stage := detail.Summary.Stage
	view := InputRequestView{Items: []InputRequestItem{}, CaseOpen: !closedJourneyStage(stage)}
	switch {
	case !view.CaseOpen:
		view.Status = "closed"
	case approvalStage(stage) && detail.CanDecide:
		view.Status = "awaiting_you"
	case approvalStage(stage):
		view.Status = "awaiting_other"
	default:
		view.Status = "in_progress"
	}
	if view.CaseOpen && approvalStage(stage) && detail.CanDecide {
		view.CanAsk = true
		view.Revision = detail.Summary.ProposalRevisionID
		view.CaseName = detail.Summary.WorkerName
	}
	rows, err := c.Store.ListInputRequests(ctx, cp.TenantID, journeyID, cp.SubjectID, chatstore.InputRequestListLimit)
	if err != nil {
		return InputRequestView{}, ErrChatWorkUnavailable
	}
	for _, row := range rows {
		item := c.item(row, detail)
		// The conversation is named only to a person who can still open it; when
		// they cannot, no advice from it is current.
		room, _, err := c.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, row.ConversationID, "")
		switch {
		case err == nil && !room.Archived:
			item.ConversationID, item.ConversationName, item.Href = row.ConversationID, strings.TrimSpace(room.Name), inputRequestHref(row.ConversationID)
		case err == nil, errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
			if item.State != "closed" {
				item.State = "unavailable"
			}
		default:
			view.Incomplete = true
			continue
		}
		view.Items = append(view.Items, item)
	}
	return view, nil
}

func (c *InputRequests) item(row chatstore.InputRequest, detail workspace.JourneyDetail) InputRequestItem {
	return InputRequestItem{
		ID: row.ID, Kind: "informal", AskedAt: row.AskedAt.UTC().Format(time.RFC3339), Revision: row.ProposalRevision,
		State: inputRequestState(detail.Summary.Stage, row.ProposalRevision, detail.Summary.ProposalRevisionID), AudienceKind: row.AudienceKind,
		AudienceSize: row.AudienceSize, SharedCaseName: row.SharedCaseName,
	}
}

func validInputRequestLabel(s string, max int) bool {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}

func shortInputRevision(revision string) string {
	if utf8.RuneCountInString(revision) > 12 {
		return string([]rune(revision)[:12])
	}
	return revision
}

// inputRequestMessage is the exact text posted: the informal label, the
// reviewer's words, the person's name only when chosen, the revision stamp and
// the way back to the case (which opens only for those allowed to open it).
func inputRequestMessage(label, question, caseName, revisionLabel, journeyID string) string {
	parts := []string{strings.TrimSpace(label), question}
	if caseName != "" {
		parts = append(parts, caseName)
	}
	parts = append(parts, strings.TrimSpace(revisionLabel), "/workspace/app/journeys?journey="+url.QueryEscape(journeyID))
	return strings.Join(parts, "\n\n")
}

// Ask previews or sends the reviewer's question. Asking twice with the same
// key returns the first request and sends nothing more.
func (c *InputRequests) Ask(ctx context.Context, p *trust.Principal, r InputRequestAsk) (InputRequestAsked, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return InputRequestAsked{}, err
	}
	question := strings.TrimSpace(r.Question)
	if !validApprovalClarificationQuestion(question) || strings.TrimSpace(r.Revision) == "" || len(r.Revision) > 128 ||
		strings.TrimSpace(r.ConversationID) == "" || len(r.ConversationID) > 128 ||
		!validInputRequestLabel(r.Label, inputRequestLabelMax) || !validInputRequestLabel(r.RevisionLabel, inputRequestLabelMax) ||
		!strings.Contains(r.RevisionLabel, shortInputRevision(r.Revision)) ||
		(!r.Preview && !chatWorkKeyPattern.MatchString(r.IdempotencyKey)) {
		return InputRequestAsked{}, ErrChatWorkInvalid
	}
	if !c.ready() {
		return InputRequestAsked{}, ErrChatWorkUnavailable
	}
	detail, err := c.open(ctx, p, r.JourneyID)
	if err != nil {
		return InputRequestAsked{}, err
	}
	stage := detail.Summary.Stage
	if closedJourneyStage(stage) {
		return InputRequestAsked{}, ErrInputRequestClosed
	}
	if !approvalStage(stage) || !detail.CanDecide {
		return InputRequestAsked{}, ErrChatWorkDenied
	}
	digest := approvalClarificationDigest(question)
	if !r.Preview {
		// A retry of an attempt already made returns it, even if the proposal moved
		// on since: the question was asked about the revision the reviewer read.
		rows, err := c.Store.ListInputRequests(ctx, cp.TenantID, r.JourneyID, cp.SubjectID, chatstore.InputRequestListLimit)
		if err != nil {
			return InputRequestAsked{}, ErrChatWorkUnavailable
		}
		for _, row := range rows {
			if row.IdempotencyKey != r.IdempotencyKey {
				continue
			}
			if row.QuestionDigest != digest || row.ProposalRevision != r.Revision || row.ConversationID != r.ConversationID {
				return InputRequestAsked{}, ErrChatWorkConflict
			}
			item := c.item(row, detail)
			item.ConversationID, item.Href = row.ConversationID, inputRequestHref(row.ConversationID)
			return InputRequestAsked{Item: &item, Revision: row.ProposalRevision, Audience: InputRequestAudience{Kind: row.AudienceKind, Size: row.AudienceSize}}, nil
		}
	}
	if r.Revision != detail.Summary.ProposalRevisionID {
		return InputRequestAsked{}, ErrInputRequestStale
	}
	room, _, err := c.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, r.ConversationID, "")
	switch {
	case err == nil && !room.Archived:
	case err == nil, errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
		return InputRequestAsked{}, ErrInputRequestConversation
	default:
		return InputRequestAsked{}, ErrChatWorkUnavailable
	}
	audience := InputRequestAudience{Kind: inputRequestAudienceKind(room.Kind), Name: strings.TrimSpace(room.Name), Size: int(room.MemberCount)}
	caseName := ""
	if r.IncludeCaseName {
		if audience.Kind == "public_channel" {
			return InputRequestAsked{}, ErrInputRequestPublicName
		}
		caseName = strings.TrimSpace(detail.Summary.WorkerName)
	}
	message := inputRequestMessage(r.Label, question, caseName, r.RevisionLabel, r.JourneyID)
	if r.Preview {
		return InputRequestAsked{Preview: true, Message: message, Audience: audience, Revision: r.Revision}, nil
	}
	post, err := c.Chat.SendPost(ctx, chat.SendPostRequest{
		Principal: cp, TenantID: cp.TenantID, ConversationID: r.ConversationID, Body: message,
		IdempotencyKey: "input-" + approvalClarificationDigest(cp.SubjectID + "\x00" + r.IdempotencyKey)[:40],
	})
	switch {
	case err == nil:
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrNotFound):
		return InputRequestAsked{}, ErrInputRequestConversation
	default:
		return InputRequestAsked{}, ErrChatWorkUnavailable
	}
	row, created, err := c.Store.RecordInputRequest(ctx, chatstore.InputRequest{
		TenantID: cp.TenantID, ID: uuid.NewString(), JourneyID: r.JourneyID, ProposalRevision: r.Revision,
		ConversationID: r.ConversationID, PostID: post.ID, AskedBy: cp.SubjectID, AudienceKind: audience.Kind, AudienceSize: audience.Size,
		SharedCaseName: caseName != "", QuestionDigest: digest, IdempotencyKey: r.IdempotencyKey, AskedAt: c.now(),
	})
	if err != nil {
		return InputRequestAsked{}, ErrChatWorkUnavailable
	}
	item := c.item(row, detail)
	item.ConversationID, item.ConversationName, item.Href = row.ConversationID, audience.Name, inputRequestHref(row.ConversationID)
	return InputRequestAsked{Message: message, Audience: audience, Revision: r.Revision, Item: &item, Created: created}, nil
}

// NewInputRequests composes the use case over the journey engine, the
// conversation service (with the reference reader that applies the reader's own
// access) and the chat store.
func NewInputRequests(journeys workspace.JourneyEngine, service chat.ConversationService, refs interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
}, store InputRequestStore) *InputRequests {
	if journeys == nil || service == nil || refs == nil || store == nil {
		return nil
	}
	return &InputRequests{Journeys: journeys, Chat: approvalClarificationChat{ConversationService: service, refs: refs}, Store: store}
}
