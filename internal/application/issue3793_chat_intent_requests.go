package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3793: a person asks for a governed change from the composer.
//
// "/leave" is a typed request command (chat.IntentRequestCommand). The person
// fills in typed fields, sees what will be sent, and submits; the request is
// proposed as a BusinessIntent through the same ChatExtensions.ProposeIntent an
// installed agent uses, so the intent kernel's own authorization decides it.
//
// The owner of such a request is an installed agent of the conversation that
// declares the command in its manifest (Manifest.Commands) with the scope
// granted: a human-typed command cannot invent an installation identity, so a
// conversation without one, or with one that is paused or revoked, answers
// "not available" instead of offering a form. The person's current authority
// for the command scope is checked again at preview and at submit.
//
// A submission is one explicit act. The page names it with a key; retrying the
// same reviewed request reuses the key and recovers the same intent, and a
// deliberately different request uses a new one. Nothing here approves or
// carries out the change: the receipt says the request was sent for review.

var (
	ErrChatIntentRequestInvalid     = errors.New("chat request: invalid request")
	ErrChatIntentRequestNotFound    = errors.New("chat request: not found")
	ErrChatIntentRequestDenied      = errors.New("chat request: not allowed")
	ErrChatIntentRequestUnavailable = errors.New("chat request: unavailable")
)

// Reasons a request command is not available in a conversation.
const (
	ChatIntentRequestNotInstalled = "not_installed"
	ChatIntentRequestPaused       = "paused"
	ChatIntentRequestNotGranted   = "not_granted"
	ChatIntentRequestNotAllowed   = "not_allowed"
)

// ChatIntentRequestNotAvailableError says why a command cannot be used here.
type ChatIntentRequestNotAvailableError struct{ Reason string }

func (e *ChatIntentRequestNotAvailableError) Error() string { return "chat request: not available" }

// ChatIntentRequestInvalidError names each refused field by its code
// (chat.FormErr*, chat.IntentRequestErrBadRange).
type ChatIntentRequestInvalidError struct{ Fields map[string]string }

func (e *ChatIntentRequestInvalidError) Error() string { return ErrChatIntentRequestInvalid.Error() }
func (e *ChatIntentRequestInvalidError) Unwrap() error { return ErrChatIntentRequestInvalid }

// ChatIntentRequestPort is what the use case needs of the chat extensions.
type ChatIntentRequestPort interface {
	ListInstallations(context.Context, chat.Principal, string) ([]chatapps.Installation, error)
	ProposeIntent(context.Context, chat.Principal, string, chatapps.Proposal) (chatapps.ProposalReceipt, error)
	AuthorizeRequestCommand(context.Context, chat.Principal, string, string, string) error
	ActiveMembers(context.Context, chat.Principal, string) (map[string]bool, error)
}

// ChatIntentRequestAvailability is one command as this person may use it here.
type ChatIntentRequestAvailability struct {
	Command   string `json:"command"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// ChatIntentRequestPreview is what a submission would send, read back after the
// server has checked it. Nothing is written.
type ChatIntentRequestPreview struct {
	Command string            `json:"command"`
	Values  map[string]string `json:"values"`
	// Via is the installed agent that takes the request in.
	Via string `json:"via"`
}

// ChatIntentRequestReceipt is the authoritative answer to a submission.
type ChatIntentRequestReceipt struct {
	Command  string `json:"command"`
	IntentID string `json:"intent_id"`
	Status   string `json:"status,omitempty"`
	// Href opens the request where its status is kept.
	Href string `json:"href"`
}

// ChatIntentRequests is the use case behind the request routes.
type ChatIntentRequests struct {
	Port ChatIntentRequestPort
	Now  func() time.Time
}

func (s *ChatIntentRequests) ready() bool { return s != nil && s.Port != nil }

func (s *ChatIntentRequests) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// NewChatIntentRequests composes the use case over the chat extensions; it
// returns nil, which leaves the routes off, when there are none.
func NewChatIntentRequests(extensions *ChatExtensions) *ChatIntentRequests {
	if extensions == nil || extensions.Apps == nil || extensions.Conversations == nil {
		return nil
	}
	return &ChatIntentRequests{Port: extensions}
}

// AuthorizeRequestCommand is the person's current authority for one command
// scope, never the installation's.
func (s *ChatExtensions) AuthorizeRequestCommand(ctx context.Context, p chat.Principal, conversation, appID, scope string) error {
	if s == nil || s.AppCommandAuthorization == nil {
		return chat.ErrPermissionDenied
	}
	return s.AppCommandAuthorization.AuthorizeChatAppCommand(ctx, p, conversation, appID, scope)
}

// ActiveMembers lists the people currently in the conversation, read as the
// person asking.
func (s *ChatExtensions) ActiveMembers(ctx context.Context, p chat.Principal, conversation string) (map[string]bool, error) {
	if s == nil || s.Conversations == nil {
		return nil, chat.ErrUnavailable
	}
	members := map[string]bool{}
	cursor := ""
	for page := 0; page < 50; page++ {
		res, err := s.Conversations.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation, Page: chat.Page{Cursor: cursor, PageSize: 200}})
		if err != nil {
			return nil, err
		}
		for _, m := range res.Memberships {
			if m.JoinedAt != nil && m.LeftAt == nil && m.HomeTenantID == p.TenantID {
				members[m.SubjectID] = true
			}
		}
		if res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	return members, nil
}

// owner finds the installation that takes a command in, or says why there is
// none. A refusal to read the conversation reads as not found.
func (s *ChatIntentRequests) owner(ctx context.Context, p chat.Principal, conversation string, command chat.IntentRequestCommand) (chatapps.Installation, string, error) {
	installs, err := s.Port.ListInstallations(ctx, p, conversation)
	switch {
	case err == nil:
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrNotFound):
		return chatapps.Installation{}, "", ErrChatIntentRequestNotFound
	default:
		return chatapps.Installation{}, "", ErrChatIntentRequestUnavailable
	}
	sort.Slice(installs, func(i, j int) bool { return installs[i].ID < installs[j].ID })
	declared, paused := false, false
	var scope string
	for _, install := range installs {
		if install.Tenant != p.TenantID || install.Conversation != conversation || install.Manifest.Agent == nil {
			continue
		}
		scope = ""
		for _, declaredCommand := range install.Manifest.Commands {
			if declaredCommand.Name == command.Name {
				scope = declaredCommand.Scope
			}
		}
		if scope == "" {
			continue
		}
		declared = true
		if !install.Current(s.now()) {
			paused = true
			continue
		}
		if !hasString(install.GrantedScopes, scope) || !hasString(install.Manifest.Scopes, scope) {
			continue
		}
		if s.Port.AuthorizeRequestCommand(ctx, p, conversation, install.AppID, scope) != nil {
			return chatapps.Installation{}, ChatIntentRequestNotAllowed, nil
		}
		return install, "", nil
	}
	switch {
	case !declared:
		return chatapps.Installation{}, ChatIntentRequestNotInstalled, nil
	case paused:
		return chatapps.Installation{}, ChatIntentRequestPaused, nil
	}
	return chatapps.Installation{}, ChatIntentRequestNotGranted, nil
}

func hasString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// Availability says, for each request command, whether this person may use it
// in this conversation, and if not, why.
func (s *ChatIntentRequests) Availability(ctx context.Context, human *trust.Principal, conversation string) ([]ChatIntentRequestAvailability, error) {
	p, err := chatWorkPrincipal(human)
	if err != nil {
		return nil, ErrChatIntentRequestDenied
	}
	if !s.ready() {
		return nil, ErrChatIntentRequestUnavailable
	}
	if strings.TrimSpace(conversation) == "" {
		return nil, ErrChatIntentRequestInvalid
	}
	var out []ChatIntentRequestAvailability
	for _, command := range chat.IntentRequestCommands() {
		_, reason, err := s.owner(ctx, p, conversation, command)
		if err != nil {
			return nil, err
		}
		out = append(out, ChatIntentRequestAvailability{Command: command.Name, Available: reason == "", Reason: reason})
	}
	return out, nil
}

// check is everything that must hold before a request is shown or sent: the
// command exists, the conversation is the person's to use, an owner is
// installed and current, the person may use the command, and every value is
// valid. It writes nothing.
func (s *ChatIntentRequests) check(ctx context.Context, human *trust.Principal, conversation, name string, values map[string]string) (chat.Principal, chat.IntentRequestCommand, chatapps.Installation, map[string]string, error) {
	p, err := chatWorkPrincipal(human)
	if err != nil {
		return p, chat.IntentRequestCommand{}, chatapps.Installation{}, nil, ErrChatIntentRequestDenied
	}
	if !s.ready() {
		return p, chat.IntentRequestCommand{}, chatapps.Installation{}, nil, ErrChatIntentRequestUnavailable
	}
	command, ok := chat.IntentRequestLookup(name)
	if !ok {
		return p, command, chatapps.Installation{}, nil, ErrChatIntentRequestNotFound
	}
	if strings.TrimSpace(conversation) == "" {
		return p, command, chatapps.Installation{}, nil, ErrChatIntentRequestInvalid
	}
	install, reason, err := s.owner(ctx, p, conversation, command)
	if err != nil {
		return p, command, install, nil, err
	}
	if reason != "" {
		return p, command, install, nil, &ChatIntentRequestNotAvailableError{Reason: reason}
	}
	var members map[string]bool
	isMember := func(subject string) (bool, error) {
		if members == nil {
			found, err := s.Port.ActiveMembers(ctx, p, conversation)
			if err != nil {
				return false, ErrChatIntentRequestUnavailable
			}
			members = found
		}
		return members[subject], nil
	}
	clean, err := command.Check(values, isMember)
	if err != nil {
		var invalid *chat.FormValidationError
		if errors.As(err, &invalid) {
			return p, command, install, nil, &ChatIntentRequestInvalidError{Fields: invalid.Fields}
		}
		return p, command, install, nil, err
	}
	return p, command, install, clean, nil
}

// Preview checks a request and reads back what a submission would send.
func (s *ChatIntentRequests) Preview(ctx context.Context, human *trust.Principal, conversation, name string, values map[string]string) (ChatIntentRequestPreview, error) {
	_, command, install, clean, err := s.check(ctx, human, conversation, name, values)
	if err != nil {
		return ChatIntentRequestPreview{}, err
	}
	return ChatIntentRequestPreview{Command: command.Name, Values: clean, Via: install.Manifest.Agent.DisplayName}, nil
}

// Submit sends one reviewed request. Everything is checked again first; the
// submission key names this one act, and the key the intent kernel sees is
// bound to the person, the conversation and the command as well, so a key can
// never recover someone else's request.
func (s *ChatIntentRequests) Submit(ctx context.Context, human *trust.Principal, conversation, name string, values map[string]string, submission string) (ChatIntentRequestReceipt, error) {
	if !chatWorkKeyPattern.MatchString(submission) {
		if _, err := chatWorkPrincipal(human); err != nil {
			return ChatIntentRequestReceipt{}, ErrChatIntentRequestDenied
		}
		return ChatIntentRequestReceipt{}, ErrChatIntentRequestInvalid
	}
	p, command, install, clean, err := s.check(ctx, human, conversation, name, values)
	if err != nil {
		return ChatIntentRequestReceipt{}, err
	}
	arguments := map[string]string{}
	names := make([]string, 0, len(clean))
	for key, value := range clean {
		arguments[key] = value
		names = append(names, key)
	}
	// The reviewed values are part of the key: replaying a submission key with
	// different values is a different request, never a recovery of the first
	// one (whose receipt would then misstate what was sent).
	sort.Strings(names)
	parts := []string{p.TenantID, conversation, p.SubjectID, command.Name, submission}
	for _, name := range names {
		parts = append(parts, name, clean[name])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	receipt, err := s.Port.ProposeIntent(ctx, p, conversation, chatapps.Proposal{
		AgentInstallation: install.ID,
		IntentType:        command.IntentType,
		IdempotencyKey:    "chatreq-" + hex.EncodeToString(sum[:20]),
		Arguments:         arguments,
		Subjects:          []chatapps.ProposalSubject{{Kind: command.SubjectKind, ID: clean[command.SubjectField], AuthorityDomain: command.SubjectDomain}},
		SchemaID:          command.SchemaID,
		SchemaVersion:     command.SchemaVersion,
		ProtoFullName:     command.ProtoFullName,
		RequestPayload:    command.Payload(clean),
	})
	switch {
	case err == nil:
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chatapps.ErrDenied):
		return ChatIntentRequestReceipt{}, ErrChatIntentRequestDenied
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chatapps.ErrNotFound):
		return ChatIntentRequestReceipt{}, ErrChatIntentRequestNotFound
	default:
		return ChatIntentRequestReceipt{}, ErrChatIntentRequestUnavailable
	}
	if receipt.IntentID == "" {
		return ChatIntentRequestReceipt{}, ErrChatIntentRequestUnavailable
	}
	return ChatIntentRequestReceipt{Command: command.Name, IntentID: receipt.IntentID, Status: receipt.Status, Href: "/workspace/app/journeys?journey=" + url.QueryEscape(receipt.IntentID)}, nil
}
