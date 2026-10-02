package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var errPersonaChat = errors.New("persona chat is unavailable")

// errPersonaChatDenied is the server's "not for you in this conversation"
// answer. It is not a failure of the agent list: the person has no agents to
// mention there, so the mention menu shows its people alone (CHATBUG-028).
var errPersonaChatDenied = errors.New("persona chat is not offered in this conversation")

func personaChatResponseOK(response *http.Response, err error) bool {
	return response != nil && err == nil && response.StatusCode == http.StatusOK
}

// personaWatchDenied is the refusal that means "no agent activity for you in
// this conversation", as opposed to a signed-out session or a dead server.
func personaWatchDenied(response *http.Response) bool {
	return response != nil && response.StatusCode == http.StatusForbidden
}

func personaWatchTerminal(response *http.Response, err error) bool {
	if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
		return true
	}
	if err == nil {
		return false
	}
	var opErr *net.OpError
	message := strings.ToLower(err.Error())
	return errors.Is(err, syscall.ECONNREFUSED) || (errors.As(err, &opErr) && errors.Is(opErr.Err, syscall.ECONNREFUSED)) || strings.Contains(message, "connection refused") || strings.Contains(message, "err_connection_refused")
}

func personaWatchBackoff(failures int) time.Duration {
	if failures < 0 {
		failures = 0
	}
	delay := 3 * time.Second
	for i := 0; i < failures && delay < 60*time.Second; i++ {
		delay *= 2
	}
	if delay > 60*time.Second {
		return 60 * time.Second
	}
	return delay
}

func personaChatPostReferences(post *chatv1.Post) []chatui.ChatReference {
	var out []chatui.ChatReference
	for _, ref := range post.GetReferences() {
		kind := ""
		switch ref.GetKind() {
		case chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION:
			kind = "AGENT_MENTION"
		case chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION:
			kind = "PERSON_MENTION"
		}
		if kind != "" {
			out = append(out, chatui.ChatReference{Kind: kind, TenantID: ref.GetTenantId(), ID: ref.GetId(), Display: ref.GetDisplay(), ConversationID: ref.GetConversationId()})
		}
	}
	return out
}

type personaChatReference struct {
	Kind           string `json:"kind"`
	TenantID       string `json:"tenant_id"`
	ID             string `json:"id"`
	Display        string `json:"display"`
	ConversationID string `json:"conversation_id"`
}
type personaChatProfile struct {
	Icon          agenticon.Value      `json:"icon"`
	IconRevision  int64                `json:"icon_revision"`
	Reference     personaChatReference `json:"reference"`
	Handle        string               `json:"handle"`
	Initials      string               `json:"initials"`
	AvatarURL     string               `json:"avatar_url"`
	Purpose       string               `json:"purpose"`
	DocumentScope string               `json:"document_scope"`
	Owner         string               `json:"owner"`
	Version       string               `json:"version"`
	Skills        []struct {
		Name string `json:"name"`
		Tier string `json:"tier"`
	} `json:"skills"`
	DataClasses    []string `json:"data_classes"`
	CannotDo       []string `json:"cannot_do"`
	ReplyPlacement string   `json:"reply_placement"`
}

type personaChatPostActor struct {
	Icon           agenticon.Value `json:"icon"`
	IconRevision   int64           `json:"icon_revision"`
	PostID         string          `json:"post_id"`
	PersonaID      string          `json:"persona_id"`
	PersonaVersion string          `json:"persona_version"`
	AgentID        string          `json:"agent_id"`
	InvokerHandle  string          `json:"invoker_handle"`
	Display        string          `json:"display"`
}

type personaChatDirectory struct {
	Personas   []personaChatProfile   `json:"personas"`
	PostActors []personaChatPostActor `json:"post_actors"`
}

func applyPersonaDirectoryResult(model *chatui.Model, payload personaChatDirectory, cfg journeyclient.Config, conversation string, err error, retry func()) {
	if model == nil {
		return
	}
	model.Callbacks.RetryPersonaMentions = retry
	model.PersonaLookupConversationID = conversation
	if errors.Is(err, errPersonaChatDenied) {
		err, payload = nil, personaChatDirectory{}
	}
	if err != nil {
		model.ResolvedPersonaMentions, model.PersonaPostActors = nil, nil
		model.PersonaLookup = chatui.PersonaLookupFailed
		return
	}
	model.ResolvedPersonaMentions = personaChatProfiles(payload.Personas, cfg, conversation)
	for _, persona := range model.ResolvedPersonaMentions {
		if persona.Reference.ConversationID == conversation {
			applyAgentDirectConversation(model, conversation, persona.Reference.ID, persona.Reference.Display, agentDirectIdentity{Icon: persona.Icon, Revision: persona.IconRevision, Purpose: persona.Purpose})
			break
		}
	}
	model.PersonaPostActors = personaChatActorProjection(payload.PostActors)
	model.PersonaLookup = chatui.PersonaLookupReady
	// The directory carries every stored icon for this room's agents: one without
	// is drawn with its own fallback from here on, not held empty. While the
	// server's agent list is still on its way, that list decides instead.
	if !model.AgentRailPending {
		model.AgentIconsReady = true
	}
}

func personaChatActorProjection(actors []personaChatPostActor) map[string]chatui.PersonaPostActor {
	out := make(map[string]chatui.PersonaPostActor, len(actors))
	for _, actor := range actors {
		if actor.PostID == "" || actor.PersonaID == "" || actor.AgentID == "" || strings.TrimSpace(actor.Display) == "" {
			continue
		}
		out[actor.PostID] = chatui.PersonaPostActor{Display: actor.Display, Actor: chatui.PersonaActor{PersonaID: actor.PersonaID, PersonaVersion: actor.PersonaVersion, AgentID: actor.AgentID, InvokerHandle: actor.InvokerHandle, Trusted: true, Icon: actor.Icon, IconRevision: actor.IconRevision}}
	}
	return out
}

type personaChatInvocation struct {
	InvocationID          string `json:"invocation_id"`
	PostID                string `json:"post_id"`
	ThreadID              string `json:"thread_id"`
	ConversationID        string `json:"conversation_id"`
	InvokerID             string `json:"invoker_id"`
	AgentName             string `json:"agent_name"`
	Status                string `json:"status"`
	Activity              string `json:"activity"`
	CurrentStep           int    `json:"current_step"`
	TotalSteps            int    `json:"total_steps"`
	TaskID                string `json:"task_id"`
	TaskTitle             string `json:"task_title"`
	TaskState             string `json:"task_state"`
	TaskRevision          uint64 `json:"task_revision"`
	FailureCode           string `json:"failure_code"`
	FailureMessage        string `json:"failure_message"`
	Retryable             bool   `json:"retryable"`
	PrivateConversationID string `json:"private_conversation_id"`
	PrivatePostID         string `json:"private_post_id"`
	// PublicPostID is the answer posted to the channel (AGENTUX-070); feedback on
	// that message rates this invocation.
	PublicPostID string `json:"public_post_id"`
}

func personaChatURL(cfg journeyclient.Config, path, conversation string) (string, error) {
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" {
		return "", errPersonaChat
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return "", errPersonaChat
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = path, "", ""
	// A caller may pass its own query. Left in Path it would be escaped and
	// the request would miss its route.
	if route, query, found := strings.Cut(path, "?"); found {
		endpoint.Path, endpoint.RawQuery = route, query
	}
	if conversation != "" {
		endpoint.RawQuery = url.Values{"conversation_id": {conversation}}.Encode()
	}
	return endpoint.String(), nil
}

func personaChatRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, method, path, conversation string, out any) error {
	endpoint, err := personaChatURL(cfg, path, conversation)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	if response == nil {
		return errPersonaChat
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return errPersonaChatDenied
	}
	if response.StatusCode != http.StatusOK {
		return errPersonaChat
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}

func personaChatProfiles(profiles []personaChatProfile, cfg journeyclient.Config, conversation string) []chatui.ResolvedPersonaMention {
	out := make([]chatui.ResolvedPersonaMention, 0, len(profiles))
	for _, profile := range profiles {
		ref := profile.Reference
		if ref.Kind != "AGENT_MENTION" || ref.TenantID != cfg.Tenant || ref.ConversationID != conversation || ref.ID == "" || strings.TrimSpace(ref.Display) == "" {
			continue
		}
		placement := chatui.PersonaReplyPlacement(profile.ReplyPlacement)
		if placement == "private" {
			placement = chatui.PersonaReplyPrivateAudience
		}
		candidate := chatui.ResolvedPersonaMention{Icon: profile.Icon, IconRevision: profile.IconRevision, Reference: chatui.ChatReference{Kind: ref.Kind, TenantID: ref.TenantID, ID: ref.ID, Display: ref.Display, ConversationID: ref.ConversationID}, Handle: profile.Handle, Initials: profile.Initials, AvatarURL: profile.AvatarURL, Purpose: profile.Purpose, DocumentScope: profile.DocumentScope, Owner: profile.Owner, Version: profile.Version, DataClasses: profile.DataClasses, CannotDo: profile.CannotDo, ReplyPlacement: placement}
		for _, skill := range profile.Skills {
			candidate.Skills = append(candidate.Skills, chatui.PersonaMentionSkill{Name: skill.Name, Tier: skill.Tier})
		}
		out = append(out, candidate)
	}
	return out
}

func personaChatReferences(refs []chatui.ChatReference, tenant, conversation string) ([]*chatv1.Reference, error) {
	out := make([]*chatv1.Reference, 0, len(refs))
	for _, ref := range refs {
		if (ref.Kind != "AGENT_MENTION" && ref.Kind != "PERSON_MENTION") || ref.TenantID != tenant || ref.ConversationID != conversation || ref.ID == "" {
			return nil, errPersonaChat
		}
		kind := chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION
		if ref.Kind == "PERSON_MENTION" {
			kind = chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION
		}
		out = append(out, &chatv1.Reference{Kind: kind, TenantId: ref.TenantID, Id: ref.ID, Display: ref.Display, ConversationId: conversation})
	}
	return out, nil
}

func personaChatSendIdentity(body string, refs []chatui.ChatReference) string {
	encoded, _ := json.Marshal(refs)
	return body + "\x00" + string(encoded)
}

func personaChatInvocations(invocations []personaChatInvocation, cfg journeyclient.Config, conversation string) []chatui.PersonaThreadInvocation {
	out := make([]chatui.PersonaThreadInvocation, 0, len(invocations))
	for _, invocation := range invocations {
		inSource := invocation.ConversationID == conversation
		inPrivateDestination := invocation.PrivateConversationID == conversation && invocation.PrivatePostID != ""
		if invocation.InvokerID != cfg.Subject || (!inSource && !inPrivateDestination) || invocation.InvocationID == "" || invocation.PostID == "" {
			continue
		}
		projection := chatui.PersonaProgressProjection{InvocationID: invocation.InvocationID, ViewerID: cfg.Subject, InvokerID: invocation.InvokerID, AgentName: invocation.AgentName}
		if invocation.PrivateConversationID != "" && invocation.PrivatePostID != "" {
			projection.DurablePostID = invocation.PrivatePostID
			if !inPrivateDestination {
				projection.PrivateReplyHref = chatui.ChannelReferenceURL(invocation.PrivateConversationID)
			}
		} else if invocation.PublicPostID != "" {
			projection.DurablePostID = invocation.PublicPostID
		}
		switch strings.ToLower(invocation.Status) {
		case "failed", "denied", "needs_repair":
			projection.Failure = &chatui.PersonaProgressFailure{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, Code: invocation.FailureCode, Message: invocation.FailureMessage, Retryable: invocation.Retryable}
		case "cancelled":
			code := invocation.FailureCode
			if code == "" {
				code = "CANCELLED"
			}
			projection.Failure = &chatui.PersonaProgressFailure{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, Code: code}
		case "expired":
			code := invocation.FailureCode
			if code == "" {
				code = "EXPIRED"
			}
			projection.Failure = &chatui.PersonaProgressFailure{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, Code: code}
		case "completed":
			// Private delivery and the invocation projection arrive on separate
			// recipient-scoped streams. Keep the admitted row in its working
			// state until the private envelope supplies the answer itself.
			if projection.PrivateReplyHref != "" {
				projection.Progress = &chatui.PersonaProgressProps{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, AgentName: invocation.AgentName, Activity: invocation.Activity, Visible: true}
			}
		default:
			projection.Progress = &chatui.PersonaProgressProps{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, AgentName: invocation.AgentName, Activity: invocation.Activity, CurrentStep: invocation.CurrentStep, TotalSteps: invocation.TotalSteps, Visible: true}
		}
		if invocation.TaskID != "" {
			state := strings.ToLower(invocation.TaskState)
			projection.Task = &chatui.PersonaTaskCardProps{ID: invocation.TaskID, Title: invocation.TaskTitle, State: state, Revision: strconv.FormatUint(invocation.TaskRevision, 10), OpenTaskHref: "/workspace/app/agents?task=" + url.QueryEscape(invocation.TaskID), AwaitingApproval: state == "awaiting_approval"}
		}
		out = append(out, chatui.PersonaThreadInvocation{PostID: invocation.PostID, ThreadID: invocation.ThreadID, Projection: projection})
	}
	return out
}
