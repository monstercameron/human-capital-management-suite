package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3995: ask for clarification from an approval review.
//
// The approver who holds the decision on a workflow case asks the person who
// made the request a focused question, about one proposal revision, in a
// private direct conversation between the two of them. Nothing is ever posted
// anywhere else, and the message holds only what the approver wrote and the
// way back to the case: no pay, dates or names are copied into chat. The
// request is recorded (who, of whom, which revision, where, a digest of the
// words) so it survives the message being edited or removed.
//
// A reply in chat is a conversation, not new evidence: nothing here lets a
// reply, a reaction or an edit amend, approve or execute the request. Putting
// new information into the case is the owning workflow's own action and is not
// part of this slice.
//
// Every decision is made before anything is written: the viewer must be able to
// open the case, must hold its approval now, and must have reviewed the
// revision that is current now.

var (
	// ErrApprovalClarificationStale means the proposal changed since the
	// approver reviewed it, so the question must be asked again on the new one.
	ErrApprovalClarificationStale = errors.New("approval clarification: the proposal changed since it was reviewed")
	// ErrApprovalClarificationClosed means the case no longer asks for a decision.
	ErrApprovalClarificationClosed = errors.New("approval clarification: the case is closed")
	// ErrApprovalClarificationRecipient means the requester cannot be named or messaged.
	ErrApprovalClarificationRecipient = errors.New("approval clarification: the requester cannot be messaged")
)

const (
	approvalClarificationQuestionMax = 1000
	approvalClarificationDMPages     = 5
	approvalClarificationDMScan      = 300
)

// ApprovalClarificationJourneys reads one journey as the person in the context.
type ApprovalClarificationJourneys interface {
	Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error)
}

// ApprovalClarificationChat is the conversation service as this feature uses it.
type ApprovalClarificationChat interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	ListConversations(context.Context, chat.ListConversationsRequest) (chat.ListConversationsResponse, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
	CreateConversation(context.Context, chat.CreateConversationRequest) (chat.Conversation, error)
	SendPost(context.Context, chat.SendPostRequest) (chat.Post, error)
}

// ApprovalClarificationStore keeps the record of each request.
type ApprovalClarificationStore interface {
	RecordApprovalClarification(context.Context, chatstore.ApprovalClarification) (chatstore.ApprovalClarification, bool, error)
	ListApprovalClarifications(ctx context.Context, tenant, journeyID, person string, limit int) ([]chatstore.ApprovalClarification, error)
}

// ApprovalClarificationAudience says who will be able to read a question
// before it is sent. Kind is always "direct": the approver and the requester.
type ApprovalClarificationAudience struct {
	Kind          string `json:"kind"`
	RecipientName string `json:"recipient_name,omitempty"`
	Private       bool   `json:"private"`
}

// ApprovalClarificationItem is one request as one of its two people sees it.
// State is "current" (asked about the revision under review now), "outdated"
// (the proposal changed since, so it needs a fresh review) or "closed". When
// the person can no longer open the conversation, Unavailable is true and no
// conversation is named.
type ApprovalClarificationItem struct {
	ID             string `json:"id"`
	Role           string `json:"role"` // asker | recipient
	PeerName       string `json:"peer_name,omitempty"`
	AskedAt        string `json:"asked_at"`
	Revision       string `json:"revision"`
	State          string `json:"state"`
	ConversationID string `json:"conversation_id,omitempty"`
	Href           string `json:"href,omitempty"`
	Unavailable    bool   `json:"unavailable,omitempty"`
}

// ApprovalClarificationView is what one viewer may see of a case's clarification.
// Status says who holds the next action: "awaiting_you", "awaiting_other",
// "in_progress" or "closed". Revision, Audience and WorkerName are filled only
// for the person who may ask.
type ApprovalClarificationView struct {
	CanAsk     bool                           `json:"can_ask"`
	AskBlocked string                         `json:"ask_blocked,omitempty"`
	CaseOpen   bool                           `json:"case_open"`
	Status     string                         `json:"status"`
	Revision   string                         `json:"revision,omitempty"`
	WorkerName string                         `json:"worker_name,omitempty"`
	Audience   *ApprovalClarificationAudience `json:"audience,omitempty"`
	Items      []ApprovalClarificationItem    `json:"items"`
	Incomplete bool                           `json:"incomplete"`
}

// ApprovalClarificationAsk is one reviewed question. Revision is the proposal
// revision the approver read; IdempotencyKey names the attempt.
type ApprovalClarificationAsk struct {
	JourneyID, Revision, Question, IdempotencyKey string
}

// ApprovalClarificationAsked is the answer to an ask. Created is false when
// the same attempt had already been made.
type ApprovalClarificationAsked struct {
	Item    ApprovalClarificationItem `json:"item"`
	Created bool                      `json:"-"`
}

// ApprovalClarifications is the use case behind the clarification routes.
type ApprovalClarifications struct {
	Journeys ApprovalClarificationJourneys
	Chat     ApprovalClarificationChat
	Store    ApprovalClarificationStore
	Intents  ChatWorkIntentReader
	// Names resolves display names for people; an unknown person has no entry.
	Names func(ctx context.Context, tenant string, subjects []string) (map[string]string, error)
	Now   func() time.Time
}

func (c *ApprovalClarifications) ready() bool {
	return c != nil && c.Journeys != nil && c.Chat != nil && c.Store != nil && c.Intents != nil
}

func (c *ApprovalClarifications) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func approvalStage(stage workspace.JourneyStage) bool {
	switch stage {
	case workspace.JourneyStageAwaitingApproval, workspace.JourneyStageManagerApproval, workspace.JourneyStageFinanceApproval, workspace.JourneyStageReapproval:
		return true
	}
	return false
}

func closedJourneyStage(stage workspace.JourneyStage) bool {
	switch stage {
	case workspace.JourneyStageCompleted, workspace.JourneyStageRejected, workspace.JourneyStageFailed,
		workspace.JourneyStageExecuted, workspace.JourneyStageObservingEffects, workspace.JourneyStageRecorded:
		return true
	}
	return false
}

// open reads the case as the viewer. A case the viewer cannot open and a case
// that does not exist read the same.
func (c *ApprovalClarifications) open(ctx context.Context, p *trust.Principal, journeyID string) (workspace.JourneyDetail, error) {
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

// requester names the person who made the request. It is read as the viewer,
// so the owner's own read rule decides whether the viewer may know.
func (c *ApprovalClarifications) requester(ctx context.Context, p *trust.Principal, journeyID string) (string, error) {
	inst, err := c.Intents.ReadIntent(ctx, p, journeyID)
	switch {
	case err == nil && inst != nil:
	case errors.Is(err, ErrChatWorkNotFound), errors.Is(err, ErrChatWorkDenied):
		return "", ErrApprovalClarificationRecipient
	default:
		return "", ErrChatWorkUnavailable
	}
	initiator := inst.GetInitiator()
	id := strings.TrimSpace(initiator.GetPrincipalId())
	if id == "" || initiator.GetKind() != intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN || id == p.Subject() {
		return "", ErrApprovalClarificationRecipient
	}
	return id, nil
}

func (c *ApprovalClarifications) names(ctx context.Context, tenant string, ids ...string) map[string]string {
	if c.Names == nil {
		return nil
	}
	out, err := c.Names(ctx, tenant, ids)
	if err != nil {
		return nil
	}
	return out
}

func approvalClarificationHref(conversation string) string {
	return "/workspace/app/chat#channel=" + url.QueryEscape(conversation)
}

// View answers what the viewer may see of the case's clarification: whether
// they may ask (and the case, revision and audience if so), who holds the next
// action, and the requests they are one of the two people of.
func (c *ApprovalClarifications) View(ctx context.Context, p *trust.Principal, journeyID string) (ApprovalClarificationView, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return ApprovalClarificationView{}, err
	}
	if !c.ready() {
		return ApprovalClarificationView{}, ErrChatWorkUnavailable
	}
	detail, err := c.open(ctx, p, journeyID)
	if err != nil {
		return ApprovalClarificationView{}, err
	}
	stage := detail.Summary.Stage
	view := ApprovalClarificationView{Items: []ApprovalClarificationItem{}, CaseOpen: !closedJourneyStage(stage)}
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
	rows, err := c.Store.ListApprovalClarifications(ctx, cp.TenantID, journeyID, cp.SubjectID, chatstore.ApprovalClarificationListLimit)
	if err != nil {
		return ApprovalClarificationView{}, ErrChatWorkUnavailable
	}
	people := []string{}
	for _, row := range rows {
		people = append(people, row.AskedBy, row.RecipientID)
	}
	recipient := ""
	if approvalStage(stage) && detail.CanDecide {
		recipient, err = c.requester(ctx, p, journeyID)
		switch {
		case err == nil:
			people = append(people, recipient)
		case errors.Is(err, ErrApprovalClarificationRecipient):
			view.AskBlocked = "no_recipient"
		default:
			return ApprovalClarificationView{}, err
		}
	}
	names := c.names(ctx, cp.TenantID, people...)
	if recipient != "" {
		view.CanAsk = true
		view.Revision = detail.Summary.ProposalRevisionID
		view.WorkerName = detail.Summary.WorkerName
		view.Audience = &ApprovalClarificationAudience{Kind: "direct", RecipientName: names[recipient], Private: true}
	}
	for _, row := range rows {
		item := ApprovalClarificationItem{
			ID: row.ID, Role: "recipient", PeerName: names[row.AskedBy], AskedAt: row.AskedAt.UTC().Format(time.RFC3339), Revision: row.ProposalRevision, State: "current",
		}
		if row.AskedBy == cp.SubjectID {
			item.Role, item.PeerName = "asker", names[row.RecipientID]
		}
		switch {
		case !view.CaseOpen:
			item.State = "closed"
		case row.ProposalRevision != detail.Summary.ProposalRevisionID:
			item.State = "outdated"
		}
		// The conversation is named only to a person who can still open it.
		_, _, err := c.Chat.ReadAuthorizedReference(ctx, cp, cp.TenantID, row.ConversationID, "")
		switch {
		case err == nil:
			item.ConversationID, item.Href = row.ConversationID, approvalClarificationHref(row.ConversationID)
		case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
			item.Unavailable = true
		default:
			view.Incomplete = true
			continue
		}
		view.Items = append(view.Items, item)
	}
	return view, nil
}

func validApprovalClarificationQuestion(question string) bool {
	if question == "" || utf8.RuneCountInString(question) > approvalClarificationQuestionMax || !utf8.ValidString(question) {
		return false
	}
	for _, r := range question {
		if r < 0x20 && r != '\n' && r != '\t' {
			return false
		}
		if r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}

func approvalClarificationDigest(question string) string {
	sum := sha256.Sum256([]byte(question))
	return hex.EncodeToString(sum[:])
}

// Ask sends the approver's question to the requester in their private direct
// conversation and records it. Asking twice with the same key returns the
// first request and sends nothing more.
func (c *ApprovalClarifications) Ask(ctx context.Context, p *trust.Principal, r ApprovalClarificationAsk) (ApprovalClarificationAsked, error) {
	cp, err := chatWorkPrincipal(p)
	if err != nil {
		return ApprovalClarificationAsked{}, err
	}
	question := strings.TrimSpace(r.Question)
	if !validApprovalClarificationQuestion(question) || strings.TrimSpace(r.Revision) == "" || len(r.Revision) > 128 || !chatWorkKeyPattern.MatchString(r.IdempotencyKey) {
		return ApprovalClarificationAsked{}, ErrChatWorkInvalid
	}
	if !c.ready() {
		return ApprovalClarificationAsked{}, ErrChatWorkUnavailable
	}
	detail, err := c.open(ctx, p, r.JourneyID)
	if err != nil {
		return ApprovalClarificationAsked{}, err
	}
	stage := detail.Summary.Stage
	if closedJourneyStage(stage) {
		return ApprovalClarificationAsked{}, ErrApprovalClarificationClosed
	}
	if !approvalStage(stage) || !detail.CanDecide {
		return ApprovalClarificationAsked{}, ErrChatWorkDenied
	}
	digest := approvalClarificationDigest(question)
	// A retry of an attempt already made returns it, even if the proposal moved
	// on since: the question was asked about the revision the approver read.
	if rows, err := c.Store.ListApprovalClarifications(ctx, cp.TenantID, r.JourneyID, cp.SubjectID, chatstore.ApprovalClarificationListLimit); err != nil {
		return ApprovalClarificationAsked{}, ErrChatWorkUnavailable
	} else {
		for _, row := range rows {
			if row.AskedBy != cp.SubjectID || row.IdempotencyKey != r.IdempotencyKey {
				continue
			}
			if row.QuestionDigest != digest || row.ProposalRevision != r.Revision {
				return ApprovalClarificationAsked{}, ErrChatWorkConflict
			}
			return ApprovalClarificationAsked{Item: c.askedItem(row, detail)}, nil
		}
	}
	if r.Revision != detail.Summary.ProposalRevisionID {
		return ApprovalClarificationAsked{}, ErrApprovalClarificationStale
	}
	recipient, err := c.requester(ctx, p, r.JourneyID)
	if err != nil {
		return ApprovalClarificationAsked{}, err
	}
	conversation, err := c.directConversation(ctx, cp, recipient)
	if err != nil {
		return ApprovalClarificationAsked{}, err
	}
	post, err := c.Chat.SendPost(ctx, chat.SendPostRequest{
		Principal: cp, TenantID: cp.TenantID, ConversationID: conversation,
		Body:           question + "\n\n/workspace/app/journeys?journey=" + url.QueryEscape(r.JourneyID),
		IdempotencyKey: "clarify-" + approvalClarificationDigest(cp.SubjectID + "\x00" + r.IdempotencyKey)[:40],
	})
	switch {
	case err == nil:
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrNotFound):
		return ApprovalClarificationAsked{}, ErrApprovalClarificationRecipient
	default:
		return ApprovalClarificationAsked{}, ErrChatWorkUnavailable
	}
	row, created, err := c.Store.RecordApprovalClarification(ctx, chatstore.ApprovalClarification{
		TenantID: cp.TenantID, ID: uuid.NewString(), JourneyID: r.JourneyID, ProposalRevision: r.Revision,
		ConversationID: conversation, PostID: post.ID, AskedBy: cp.SubjectID, RecipientID: recipient,
		QuestionDigest: digest, IdempotencyKey: r.IdempotencyKey, AskedAt: c.now(),
	})
	if err != nil {
		return ApprovalClarificationAsked{}, ErrChatWorkUnavailable
	}
	return ApprovalClarificationAsked{Item: c.askedItem(row, detail), Created: created}, nil
}

func (c *ApprovalClarifications) askedItem(row chatstore.ApprovalClarification, detail workspace.JourneyDetail) ApprovalClarificationItem {
	state := "current"
	switch {
	case closedJourneyStage(detail.Summary.Stage):
		state = "closed"
	case row.ProposalRevision != detail.Summary.ProposalRevisionID:
		state = "outdated"
	}
	return ApprovalClarificationItem{
		ID: row.ID, Role: "asker", AskedAt: row.AskedAt.UTC().Format(time.RFC3339), Revision: row.ProposalRevision, State: state,
		ConversationID: row.ConversationID, Href: approvalClarificationHref(row.ConversationID),
	}
}

// directConversation finds the private direct conversation between the asker
// and the recipient, whoever started it, or starts one. A conversation holding
// anyone else is never used.
func (c *ApprovalClarifications) directConversation(ctx context.Context, asker chat.Principal, recipient string) (string, error) {
	cursor, scanned := "", 0
	for page := 0; page < approvalClarificationDMPages; page++ {
		listed, err := c.Chat.ListConversations(ctx, chat.ListConversationsRequest{Principal: asker, TenantID: asker.TenantID, Page: chat.Page{Cursor: cursor, PageSize: 100}})
		if err != nil {
			return "", ErrChatWorkUnavailable
		}
		for _, room := range listed.Conversations {
			if room.Kind != chat.Direct || scanned >= approvalClarificationDMScan {
				continue
			}
			scanned++
			members, err := c.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: asker, TenantID: asker.TenantID, ConversationID: room.ID, Page: chat.Page{PageSize: 3}})
			if err != nil {
				return "", ErrChatWorkUnavailable
			}
			if directPairMatches(members.Memberships, asker.SubjectID, recipient) {
				return room.ID, nil
			}
		}
		if listed.NextCursor == "" || listed.NextCursor == cursor {
			break
		}
		cursor = listed.NextCursor
	}
	pair := []string{asker.SubjectID, recipient}
	if pair[0] > pair[1] {
		pair[0], pair[1] = pair[1], pair[0]
	}
	created, err := c.Chat.CreateConversation(ctx, chat.CreateConversationRequest{
		Principal: asker, TenantID: asker.TenantID, Kind: chat.Direct, OwnerID: asker.SubjectID,
		Members:        []chat.MemberRef{{TenantID: asker.TenantID, SubjectID: recipient}},
		IdempotencyKey: "approval-clarification-dm-" + approvalClarificationDigest(asker.TenantID + "\x00" + pair[0] + "\x00" + pair[1])[:40],
	})
	switch {
	case err == nil && created.ID != "":
		return created.ID, nil
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrInvalidArgument), errors.Is(err, chat.ErrNotFound):
		return "", ErrApprovalClarificationRecipient
	}
	return "", ErrChatWorkUnavailable
}

// directPairMatches is true only for exactly the two named people, both active.
func directPairMatches(members []chat.Membership, a, b string) bool {
	active := map[string]bool{}
	for _, m := range members {
		if m.LeftAt != nil || m.JoinedAt == nil {
			continue
		}
		active[m.SubjectID] = true
	}
	return len(active) == 2 && active[a] && active[b]
}

// approvalClarificationChat joins the conversation service with the reference
// reader that applies the reader's own access.
type approvalClarificationChat struct {
	chat.ConversationService
	refs interface {
		ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	}
}

func (c approvalClarificationChat) ReadAuthorizedReference(ctx context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	return c.refs.ReadAuthorizedReference(ctx, p, tenant, conversation, post)
}

// NewApprovalClarifications composes the use case over the journey engine, the
// conversation service, the chat store and the intent service.
func NewApprovalClarifications(journeys workspace.JourneyEngine, service chat.ConversationService, refs interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
}, store ApprovalClarificationStore, intents ChatWorkIntentReader, names func(context.Context, string, []string) (map[string]string, error)) *ApprovalClarifications {
	if journeys == nil || service == nil || refs == nil || store == nil || intents == nil {
		return nil
	}
	return &ApprovalClarifications{
		Journeys: journeys, Chat: approvalClarificationChat{ConversationService: service, refs: refs},
		Store: store, Intents: intents, Names: names,
	}
}
