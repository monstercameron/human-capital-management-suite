package chat

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// Issue 3792. /decide: a conversational decision recorded as its own object.
//
// A decision is a short statement with the person who recorded it, the day it
// was recorded, an optional reason, and the message it came from. It is always
// informal: it is a record of what a conversation settled, never a deployed
// policy, and nothing here promotes it to one. A decision is never edited;
// recording a new one that replaces it leaves the earlier one in place, marked
// as replaced and pointing at its successor, so the history stays readable.

const (
	DecisionTextMax      = 500
	DecisionRationaleMax = 2000
	DecisionLabelMax     = 60
	// DecisionListLimit is how many decisions one list returns.
	DecisionListLimit = 200
	// DecisionDefaultLabel opens the line posted into the conversation when the
	// caller sends no label of its own.
	DecisionDefaultLabel = "Decision (informal)"
)

// Decision is one recorded decision.
type Decision struct {
	ID, TenantID, ConversationID string
	Text, Rationale              string
	OwnerHomeTenantID, OwnerID   string
	DecidedAt                    time.Time
	// SourcePostID is the message that carries the decision in the conversation:
	// the one the person marked, or the line posted when it was recorded.
	SourcePostID string
	// SourceSequence is that message's place in the conversation, so a reader can be taken to it.
	SourceSequence uint64
	// SupersedesID is the decision this one replaced; SupersededByID is the one
	// that replaced this one. Both are empty for a decision nobody replaced.
	SupersedesID, SupersededByID string
	// Informal is true for every decision recorded this way.
	Informal bool
}

// DecisionStore keeps decisions. Every method is scoped by the conversation's
// tenant; the service has already decided that the caller may act.
type DecisionStore interface {
	// InsertDecision stores a decision once: recording the same ID again returns
	// the stored decision. When SupersedesID is set it replaces that decision in
	// the same transaction, and refuses (ErrNotFound) one that is not in this
	// conversation or (ErrConflict) one that was already replaced.
	InsertDecision(ctx context.Context, d Decision) (Decision, error)
	GetDecision(ctx context.Context, tenant, conversation, id string) (Decision, error)
	// ListDecisions returns the conversation's decisions, newest first. A non-nil
	// since leaves out those recorded before it.
	ListDecisions(ctx context.Context, tenant, conversation string, since *time.Time, limit int) ([]Decision, error)
}

// DecisionView is a decision as one reader may act on it.
type DecisionView struct {
	Decision
	// CanReplace says the reader may record a decision that replaces this one:
	// they recorded it, or they manage the conversation. It is false once the
	// decision has been replaced.
	CanReplace bool
}

// DecisionList is what one reader sees of a conversation's decisions.
type DecisionList struct {
	Decisions []DecisionView
	// CanRecord says the reader may record a decision here now.
	CanRecord bool
}

// DecisionService records and lists decisions over the Chat service, so a
// decision is held to the same membership, channel status and content rules as
// a message.
type DecisionService struct {
	Chat  *Service
	Store DecisionStore
	// Sender posts the line that shows a decision in the conversation. The
	// served composition gives it the routed and audited conversation service;
	// nil posts through Chat.
	Sender Chatcmd002Sender
}

// DecisionRecordRequest asks to record a decision.
type DecisionRecordRequest struct {
	Principal                       Principal
	TenantID, ConversationID        string
	Text, Rationale                 string
	SourcePostID, SupersedesID      string
	ParentID, IdempotencyKey, Label string
}

// NormalizeDecisionText folds a decision's words into one line of 1 to 500
// characters without control characters.
func NormalizeDecisionText(text string) (string, bool) {
	if !utf8.ValidString(text) {
		return "", false
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || utf8.RuneCountInString(text) > DecisionTextMax {
		return "", false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return text, true
}

// NormalizeDecisionRationale trims a reason. It may be empty and may hold line
// breaks, up to 2000 characters.
func NormalizeDecisionRationale(text string) (string, bool) {
	if !utf8.ValidString(text) {
		return "", false
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > DecisionRationaleMax || strings.Contains(text, Chatcmd002BodyMarker) {
		return "", false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", false
		}
	}
	return text, true
}

// decisionLabels are the words that open the posted line, in the languages the
// page offers. The label tells readers the line is an informal record, so a
// caller cannot choose any other (a line that opens "Company policy" is not
// something this route may post).
var decisionLabels = map[string]bool{
	DecisionDefaultLabel:       true,
	"Entscheidung (informell)": true,
	"قرار (غير رسمي)":          true,
}

func decisionLabel(label string) string {
	label = strings.Join(strings.Fields(label), " ")
	if !decisionLabels[label] {
		return DecisionDefaultLabel
	}
	return label
}

// DecisionID is the identity of a recording: the same person sending the same
// key into the same conversation names the same decision, so a retry never
// records it twice.
func DecisionID(tenant, conversation string, p Principal, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(tenant+"\x00"+conversation+"\x00"+p.TenantID+"\x00"+p.SubjectID+"\x00decision\x00"+key)).String()
}

func (s *DecisionService) usable(p Principal, tenant, conversation string) bool {
	return s != nil && s.Chat != nil && s.Store != nil && p.SubjectID != "" && p.TenantID != "" && tenant != "" && conversation != ""
}

// authorize returns the conversation and the caller's current membership, or a
// refusal. A caller who is not a current member learns nothing.
func (s *DecisionService) authorize(ctx context.Context, p Principal, tenant, conversation string, write bool) (Conversation, Membership, error) {
	c, err := s.Chat.store.GetConversation(ctx, tenant, conversation)
	if err != nil {
		return Conversation{}, Membership{}, err
	}
	m, err := s.Chat.store.GetMembership(ctx, tenant, conversation, p.TenantID, p.SubjectID)
	if err != nil || !currentReferenceMembership(m, tenant, conversation, p.TenantID, p.SubjectID) {
		return Conversation{}, Membership{}, ErrPermissionDenied
	}
	action := chatpolicy.ActionRead
	if write {
		action = chatpolicy.ActionPost
	}
	if err := s.Chat.authorize(ctx, p, c, action); err != nil {
		return Conversation{}, Membership{}, err
	}
	return c, m, nil
}

func decisionCanReplace(d Decision, p Principal, m Membership) bool {
	if d.SupersededByID != "" {
		return false
	}
	return (d.OwnerHomeTenantID == p.TenantID && d.OwnerID == p.SubjectID) || m.Role == Manager
}

// Record records a decision and, unless it names the message that carries it,
// posts a line into the conversation so everyone there sees it. Authorization
// comes first; nothing is posted or stored for a caller who may not post here.
func (s *DecisionService) Record(ctx context.Context, r DecisionRecordRequest) (Decision, error) {
	if !s.usable(r.Principal, r.TenantID, r.ConversationID) || strings.TrimSpace(r.IdempotencyKey) == "" || len(r.IdempotencyKey) > 200 {
		return Decision{}, ErrInvalidArgument
	}
	c, m, err := s.authorize(ctx, r.Principal, r.TenantID, r.ConversationID, true)
	if err != nil {
		return Decision{}, err
	}
	text, ok := NormalizeDecisionText(r.Text)
	rationale, okR := NormalizeDecisionRationale(r.Rationale)
	if !ok || !okR {
		return Decision{}, ErrInvalidArgument
	}
	id := DecisionID(r.TenantID, r.ConversationID, r.Principal, r.IdempotencyKey)
	if existing, err := s.Store.GetDecision(ctx, r.TenantID, r.ConversationID, id); err == nil {
		// A retry of a recording that already happened.
		return existing, nil
	}
	if r.SupersedesID != "" {
		old, err := s.Store.GetDecision(ctx, r.TenantID, r.ConversationID, r.SupersedesID)
		if err != nil {
			return Decision{}, err
		}
		if old.SupersededByID != "" {
			return Decision{}, ErrConflict
		}
		if !decisionCanReplace(old, r.Principal, m) {
			return Decision{}, ErrPermissionDenied
		}
	}
	// The words pass the same filters as a message.
	if err := s.Chat.checkContent(ctx, ContentInput{Principal: r.Principal, Conversation: c, Body: text + "\n" + rationale}); err != nil {
		return Decision{}, err
	}
	source, sequence := strings.TrimSpace(r.SourcePostID), uint64(0)
	if source != "" {
		post, err := s.Chat.store.GetPost(ctx, r.TenantID, r.ConversationID, source)
		if err != nil || post.Deleted || !s.Chat.postVisibleTo(ctx, r.Principal, c, post) {
			return Decision{}, ErrNotFound
		}
		sequence = post.Sequence
	} else {
		request := SendPostRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID, ParentID: r.ParentID,
			IdempotencyKey: "decision-" + id, Body: decisionLabel(r.Label) + ": " + text}
		var posted Post
		if s.Sender != nil {
			posted, err = s.Sender.SendPost(ctx, request)
		} else {
			posted, err = s.Chat.SendPost(ctx, request)
		}
		if err != nil {
			return Decision{}, err
		}
		source, sequence = posted.ID, posted.Sequence
	}
	return s.Store.InsertDecision(ctx, Decision{ID: id, TenantID: r.TenantID, ConversationID: r.ConversationID, Text: text, Rationale: rationale,
		OwnerHomeTenantID: r.Principal.TenantID, OwnerID: r.Principal.SubjectID, DecidedAt: s.Chat.now(), SourcePostID: source, SourceSequence: sequence,
		SupersedesID: r.SupersedesID, Informal: true})
}

// List returns the conversation's decisions for one reader, newest first. A
// reader who joined with history from the day they joined sees only decisions
// recorded since; one with no history sees none.
func (s *DecisionService) List(ctx context.Context, p Principal, tenant, conversation string) (DecisionList, error) {
	if !s.usable(p, tenant, conversation) {
		return DecisionList{}, ErrInvalidArgument
	}
	_, m, err := s.authorize(ctx, p, tenant, conversation, false)
	if err != nil {
		return DecisionList{}, err
	}
	out := DecisionList{Decisions: []DecisionView{}}
	if m.HistoryVisibility == NoHistory {
		return out, nil
	}
	var since *time.Time
	if m.HistoryVisibility == FromJoin {
		if m.JoinedAt == nil {
			return out, nil
		}
		since = m.JoinedAt
	}
	list, err := s.Store.ListDecisions(ctx, tenant, conversation, since, DecisionListLimit)
	if err != nil {
		return DecisionList{}, err
	}
	for _, d := range list {
		out.Decisions = append(out.Decisions, DecisionView{Decision: d, CanReplace: decisionCanReplace(d, p, m)})
	}
	_, _, werr := s.authorize(ctx, p, tenant, conversation, true)
	out.CanRecord = werr == nil
	return out, nil
}
