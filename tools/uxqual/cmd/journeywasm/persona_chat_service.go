package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var errPersonaChat = errors.New("persona chat is unavailable")

func personaChatPostReferences(post *chatv1.Post) []chatui.ChatReference {
	var out []chatui.ChatReference
	for _, ref := range post.GetReferences() {
		if ref.GetKind() == chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION {
			out = append(out, chatui.ChatReference{Kind: "AGENT_MENTION", TenantID: ref.GetTenantId(), ID: ref.GetId(), Display: ref.GetDisplay(), ConversationID: ref.GetConversationId()})
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
	Reference personaChatReference `json:"reference"`
	Purpose   string               `json:"purpose"`
	Owner     string               `json:"owner"`
	Version   string               `json:"version"`
	Skills    []struct {
		Name string `json:"name"`
		Tier string `json:"tier"`
	} `json:"skills"`
	DataClasses    []string `json:"data_classes"`
	CannotDo       []string `json:"cannot_do"`
	ReplyPlacement string   `json:"reply_placement"`
}

type personaChatPostActor struct {
	PostID         string `json:"post_id"`
	PersonaID      string `json:"persona_id"`
	PersonaVersion string `json:"persona_version"`
	AgentID        string `json:"agent_id"`
	InvokerHandle  string `json:"invoker_handle"`
	Display        string `json:"display"`
}

type personaChatDirectory struct {
	Personas   []personaChatProfile   `json:"personas"`
	PostActors []personaChatPostActor `json:"post_actors"`
}

func personaChatActorProjection(actors []personaChatPostActor) map[string]chatui.PersonaPostActor {
	out := make(map[string]chatui.PersonaPostActor, len(actors))
	for _, actor := range actors {
		if actor.PostID == "" || actor.PersonaID == "" || actor.AgentID == "" || strings.TrimSpace(actor.Display) == "" {
			continue
		}
		out[actor.PostID] = chatui.PersonaPostActor{Display: actor.Display, Actor: chatui.PersonaActor{PersonaID: actor.PersonaID, PersonaVersion: actor.PersonaVersion, AgentID: actor.AgentID, InvokerHandle: actor.InvokerHandle, Trusted: true}}
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
	defer response.Body.Close()
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
		candidate := chatui.ResolvedPersonaMention{Reference: chatui.ChatReference{Kind: ref.Kind, TenantID: ref.TenantID, ID: ref.ID, Display: ref.Display, ConversationID: ref.ConversationID}, Purpose: profile.Purpose, Owner: profile.Owner, Version: profile.Version, DataClasses: profile.DataClasses, CannotDo: profile.CannotDo, ReplyPlacement: placement}
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
		if ref.Kind != "AGENT_MENTION" || ref.TenantID != tenant || ref.ConversationID != conversation || ref.ID == "" {
			return nil, errPersonaChat
		}
		out = append(out, &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, TenantId: ref.TenantID, Id: ref.ID, Display: ref.Display, ConversationId: conversation})
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
		if invocation.InvokerID != cfg.Subject || invocation.ConversationID != conversation || invocation.InvocationID == "" || invocation.PostID == "" {
			continue
		}
		projection := chatui.PersonaProgressProjection{InvocationID: invocation.InvocationID, ViewerID: cfg.Subject, InvokerID: invocation.InvokerID}
		if invocation.PrivateConversationID != "" && invocation.PrivatePostID != "" {
			projection.PrivateReplyHref = chatui.ChannelReferenceURL(invocation.PrivateConversationID)
		}
		switch strings.ToLower(invocation.Status) {
		case "failed", "denied", "needs_repair":
			projection.Failure = &chatui.PersonaProgressFailure{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, Code: invocation.FailureCode, Message: invocation.FailureMessage, Retryable: invocation.Retryable}
		case "completed", "cancelled", "expired":
		default:
			projection.Progress = &chatui.PersonaProgressProps{InvocationID: invocation.InvocationID, InvokerID: invocation.InvokerID, AgentName: invocation.AgentName, Activity: invocation.Activity, CurrentStep: invocation.CurrentStep, TotalSteps: invocation.TotalSteps, Visible: true}
		}
		if invocation.TaskID != "" {
			state := strings.ToLower(invocation.TaskState)
			projection.Task = &chatui.PersonaTaskCardProps{ID: invocation.TaskID, Title: invocation.TaskTitle, State: state, Revision: strconv.FormatUint(invocation.TaskRevision, 10), OpenTaskHref: "/workspace/app/agents?task=" + url.QueryEscape(invocation.TaskID), AwaitingApproval: state == "awaiting_approval"}
		}
		postID := invocation.PostID
		if invocation.ThreadID != "" {
			postID = invocation.ThreadID
		}
		out = append(out, chatui.PersonaThreadInvocation{PostID: postID, Projection: projection})
	}
	return out
}
